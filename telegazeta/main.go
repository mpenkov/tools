// News reader for Telegram channels
//
// TODO:
//
// - [ ] Pagination (we can only fetch 20 messages from a single channel at a time)
// - [x] Output HTML instead of text
// - [x] Use proper Golang templates when outputting HTML
// - [x] Extract headlines (first line in the message) and mark them up with CSS
// - [x] Output media files when available (photos, caching, etc)
// - [x] Group multiple photos/videos together
// - [x] extract video thumbnails
// - [x] Properly show video in the HTML: aspect ratio, display duration, etc.
// - [x] Embed images into the HTML so that it is fully self-contained
// - [.] Configuration file (contain phone number, secrets, channel names, etc) - avoid signing up to public channels
// - [ ] Show channel thumbnails
// - [x] Markup for hyperlinks, etc. using entities from the Message
// - [x] Parametrize threshold for old news
// - [ ] Exclude cross-posts between covered channels (i.e. deduplicate messages)
// - [x] Keyboard shortcuts (j/k, etc)
// - [x] configurable cache location
// - [ ] try harder to split the first paragraph, maybe try a sentence split?
// - [ ] Correctly identify and attribute forwarded messages
// - [x] Include photos/videos from forwarded messages
//
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/go-faster/errors"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

type Media struct {
	IsVideo         bool
	Thumbnail       string
	ThumbnailBase64 string
	URL             template.URL
	ThumbnailWidth  int
	ThumbnailHeight int
	Duration        string
	PendingDownload tg.InputFileLocationClass
}

type Channel struct {
	Domain string
	Title  string
}

type Item struct {
	MessageID  int
	GroupedID  int64
	Channel    Channel
	Text       string
	Date       time.Time
	Webpage    *tg.WebPage
	HasWebpage bool
	Media      []Media
	FwdFrom    Channel
	Forwarded  bool
}

const templ = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8">
		<style>
			body { font-family: Helvetica; }
			.item-list { display: grid; grid-gap: 0px; }
			.item {
				display: grid;
				grid-template-columns: 100px 200px 1000px;
				border-top: 1px solid gray;
				padding: 10px;
			}
			.item:nth-child(odd) { background-color: hsl(0, 0%, 90%); }
			.channel { font-size: large; font-weight: bold }
			/* .message p:nth-child(1) { font-weight: bold; font-size: large } */
			.placeholder {
				display: flex;
				align-items: center;
				justify-content: center;
				width: 300px;
				height: 200px;
				background-color: silver;
			}
			.thumbnails {
				display: grid;
				grid-template-columns: 325px 325px 325px;
				grid-gap: 10px;
			}
			.image-thumbnail { border-radius: 5%; }
			.video-thumbnail {
				border-left: 5px dashed black;
				border-right: 5px dashed black;
			}

			/* https://stackoverflow.com/questions/44275502/overlay-text-on-image-html#44275595 */
			.container { position: relative; }
			.container img { width: 100%; }
			.container p {
				position: absolute;
				bottom: 0;
				right: 0;
				color: white;
				background-color: black;
				font-size: xx-large;
				padding: 5px;
				margin: 5px;
			}

			.datestamp { margin: 10px; font-weight: bold; display: flex; flex-direction: column; gap: 10px; }
			.datestamp a { text-decoration: none; }
			span.time { font-size: large; font-weight: bold;  }
			span.date { font-size:  small; }
			.channel { margin-top: 10px; display: flex; flex-direction: column; gap: 10px; }
			.channel-title { font-size: small; color: gray; }
			.message p { margin-top: 10px; }

			a { color: darkred; }
			a:hover { color: red; }

			blockquote {
				margin: 1em 2em;
				color: hsl(0, 0%, 25%);
				border-left: 4px solid darkred;
				padding-left: 1em;
			}
		</style>
	</head>
	<body>
		<div class='item-list'>
			{{range $index, $item := .Items}}
			<span class='item' id="item-{{$index}}">
				<span class='datestamp'>
					<span class="time"><a href='{{$item | tgUrl}}'>{{$item.Date | formatTime}}</a></span>
					<span class="date"><a href='{{$item | tgUrl}}'>{{$item.Date | formatDate}}</a></span>
				</span>
				<span class='channel'>
					<span class="domain">@{{$item.Channel.Domain}}</span>
					<span class="channel-title">({{$item.Channel.Title}})</span>
				</span>
				<span class='message'>
				{{if $item.Forwarded}}
					(Forwarded from @{{$item.FwdFrom.Domain}})
				{{end}}
					{{$item.Text | markup}}
				{{if $item.HasWebpage}}
					<blockquote class='webpage'>
						<span class='link'><a href='{{$item.Webpage.URL}}' target='_blank'>{{$item.Webpage.Title}}</a></span>
						<span class='description'>{{$item.Webpage.Description | markup}}</span>
				{{end}}
					<span class="thumbnails">
				{{range $item.Media}}
					{{if .IsVideo}}
						{{if .Thumbnail}}
						<span class='image'>
							<span class='container'>
								<a href='{{.URL}}'>
									<img class="video-thumbnail" src='data:image/jpeg;base64, {{.ThumbnailBase64 }}' width="{{.ThumbnailWidth}}" Height="{{.ThumbnailHeight}}"></img>
								</a>
								<p>{{.Duration}}</p>
							</span>
						{{else}}
							<span class='placeholder'>
								<a href='{{.URL}}'>Video: {{.Duration}}</a>
							</span>
						{{end}}
						</span>
					{{else}}
						<span class='image'>
							<a href='{{.URL}}'><img class="image-thumbnail" src='data:image/jpeg;base64, {{.ThumbnailBase64 }}'></img></a>
						</span>
					{{end}}
				{{end}}
					</span>
				{{if $item.HasWebpage}}
					</blockquote>
				{{end}}
				</span>
			</span>
			{{end}}
		</div>
		<script>
//
// https://stackoverflow.com/questions/5353934/check-if-element-is-visible-on-screen
//
function checkVisible(elm, threshold, mode) {
	threshold = threshold || 0
	mode = mode || 'visible'

	var rect = elm.getBoundingClientRect()
	var viewHeight = Math.max(document.documentElement.clientHeight, window.innerHeight)
	var above = rect.bottom - threshold < 0
	var below = rect.top - viewHeight + threshold >= 0

	return mode === 'above' ? above : (mode === 'below' ? below : !above && !below)
}

document.addEventListener('keydown', function(event) {
	console.debug("keyCode", event.keyCode)
	var up = false
	if (event.keyCode === 74) {
		up = false
	} else if (event.keyCode === 75) {
		up = true
	} else {
		return true
	}
	var items = document.querySelectorAll(".item")

	//
	// Find the first visible item, and then scroll from it to the adjacent ones
	//
	for (let i = 0; i < items.length; ++i) {
		if (checkVisible(items[i], 50)) {
			if (up && i > 0) {
				items[i-1].scrollIntoView({behavior: 'smooth', 'block': 'end'})
				return false
			} else if (!up && i != items.length - 1) {
				items[i+1].scrollIntoView({behavior: 'smooth', 'block': 'start'})
				return false
			}
		}
	}
	return false
})
		</script>
	</body>
</html>
`

func formatDate(date time.Time) string {
	return date.Format("2 Jan")
}

func formatTime(date time.Time) string {
	return date.Format("15:04")
}

func tgUrl(item Item) template.URL {
	return template.URL(fmt.Sprintf("tg://resolve?domain=%s&post=%d", item.Channel.Domain, item.MessageID))
}

func imageAsBase64(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("unable to open %q: %s", path, err)
	}
	defer f.Close()
	const bufSize int = 1024768
	var buf []byte = make([]byte, bufSize)
	numRead, err := f.Read(buf)
	if numRead >= bufSize {
		log.Fatalf("buffer underflow reading %q", path)
	}
	var encoded string = base64.StdEncoding.EncodeToString(buf[:numRead])
	return encoded
}

var mapping = template.FuncMap{
	"formatDate": formatDate,
	"tgUrl":      tgUrl,
	"markup":     markup,
	"formatTime": formatTime,
}
var lenta = template.Must(template.New("lenta").Funcs(mapping).Parse(templ))

func markup(message string) template.HTML {
	paragraphs := strings.Split(message, "\n")
	var builder strings.Builder
	for _, p := range paragraphs {
		if len(p) > 0 {
			builder.WriteString(fmt.Sprintf("<p>%s</p>\n", p))
		}
	}
	return template.HTML(builder.String())
}

// noSignUp can be embedded to prevent signing up.
type noSignUp struct{}

func (c noSignUp) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("not implemented")
}

func (c noSignUp) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	return &auth.SignUpRequired{TermsOfService: tos}
}

// termAuth implements authentication via terminal.
type termAuth struct {
	noSignUp

	phone string
}

func (a termAuth) Phone(_ context.Context) (string, error) {
	return a.phone, nil
}

func (a termAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Enter 2FA password: ")
	bytePwd, err := terminal.ReadPassword(0)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytePwd)), nil
}

func (a termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Enter code: ")
	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(code), nil
}

func decodeChannel(inputPeerClass tg.InputPeerClass) (channel tg.InputChannel, err error) {
	switch inputPeerChannel := inputPeerClass.(type) {
	case *tg.InputPeerChannel:
		channel.ChannelID = inputPeerChannel.ChannelID
		channel.AccessHash = inputPeerChannel.AccessHash
	default:
		err = fmt.Errorf("decodeChannel failed")
	}
	return channel, nil
}

type Worker struct {
	Context         context.Context
	Log             *zap.Logger
	Client          *tg.Client
	TmpPath         string
	DurationSeconds int64
}

func (w Worker) decodeMessages(mmc tg.MessagesMessagesClass) (messages []tg.Message, err error) {
	var innerMessages []tg.MessageClass

	switch inner := mmc.(type) {
	case *tg.MessagesChannelMessages:
		innerMessages = inner.Messages
	case *tg.MessagesMessagesSlice:
		innerMessages = inner.Messages
	}

	for _, m := range innerMessages {
		w.Log.Debug(m.String())
		switch message := m.(type) {
		case *tg.Message:
			messages = append(messages, *message)
		}
	}

	return messages, err
}

func (w Worker) getChannelInfo(input tg.InputChannel) (string, string, error) {
	var result *tg.MessagesChatFull

	result, err := w.Client.ChannelsGetFullChannel(w.Context, &input)
	if err != nil {
		return "", "", err
	}

	switch thing := result.Chats[0].(type) {
	case *tg.Chat:
		return thing.Title, "", nil
	case *tg.Channel:
		return thing.Title, thing.Username, nil
	}

	return "", "", fmt.Errorf("not implemented yet")
}

func (w Worker) downloadThumbnail(location tg.InputFileLocationClass) (string, error) {
	var id int64
	switch location := location.(type) {
	case *tg.InputPhotoFileLocation:
		id = location.ID
	case *tg.InputDocumentFileLocation:
		id = location.ID
	default:
		return "", fmt.Errorf("unable to determine object ID from %s", location.String())
	}

	path := fmt.Sprintf("/%s/%d.jpeg", w.TmpPath, id)
	_, err := os.Stat(path)
	if err == nil {
		//
		// File exists, we don't need to download
		//
		return path, nil
	} else {
		w.Log.Info(fmt.Sprintf("downloading thumbnail for id %d", id))
		dloader := downloader.NewDownloader()
		builder := dloader.Download(w.Client, location)
		_, err := builder.ToPath(w.Context, path)
		if err == nil {
			return path, nil
		} else {
			return "", err
		}
	}
}

func (w Worker) predownloadDocumentThumbnail(doc *tg.Document) (Media, error) {
	var media Media
	if strings.HasPrefix(doc.MimeType, "video/") {
		media.IsVideo = true
		for _, attr := range doc.Attributes {
			switch a := attr.(type) {
			case *tg.DocumentAttributeVideo:
				minutes := a.Duration / 60
				seconds := a.Duration % 60
				media.Duration = fmt.Sprintf("%02d:%02d", minutes, seconds)
			}
		}
	}

	thumbnailSize, err := extractThumbnailSize(doc.Thumbs)
	w.Log.Info(fmt.Sprintf("doc.ID: %d thumbnailSize: %s", doc.ID, thumbnailSize.String()))
	if err == nil {
		media.ThumbnailWidth = thumbnailSize.W
		media.ThumbnailHeight = thumbnailSize.H
		media.PendingDownload = &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
			ThumbSize:     thumbnailSize.Type,
		}
	}

	return media, err
}

func (w Worker) predownloadPhotoThumbnail(photo *tg.Photo) (Media, error) {
	var media Media
	thumbnailSize, err := extractThumbnailSize(photo.Sizes)
	if err == nil {
		media.PendingDownload = &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumbnailSize.Type,
		}
		media.ThumbnailWidth = thumbnailSize.W
		media.ThumbnailHeight = thumbnailSize.H
	}
	return media, err
}

func (w Worker) processMessage(m tg.Message) (Item, error) {
	var webpage *tg.WebPage
	var media Media
	var haveMedia bool = false

	if m.Media != nil {
		w.Log.Debug("attachment: " + m.Media.TypeName())

		switch messageMedia := m.Media.(type) {
		case *tg.MessageMediaPhoto:
			switch photo := messageMedia.Photo.(type) {
			case *tg.Photo:
				var err error
				media, err = w.predownloadPhotoThumbnail(photo)
				if err != nil {
					w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
				} else {
					haveMedia = true
				}
			}
		case *tg.MessageMediaWebPage:
			switch wp := messageMedia.Webpage.(type) {
			case *tg.WebPage:
				//
				// Quoting another telegram channel?
				// Two places to look here: .Document and .Photo.
				// Either one will do.
				//
				webpage = wp
				switch doc := wp.Document.(type) {
				case *tg.Document:
					var err error
					media, err = w.predownloadDocumentThumbnail(doc)
					if err != nil {
						w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
					} else {
						haveMedia = true
					}
				}

				if !haveMedia {
					switch photo := wp.Photo.(type) {
					case *tg.Photo:
						var err error
						media, err = w.predownloadPhotoThumbnail(photo)
						if err != nil {
							w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
						} else {
							haveMedia = true
						}
					}
				}
			}
		case *tg.MessageMediaDocument:
			switch doc := messageMedia.Document.(type) {
			case *tg.Document:
				var err error
				media, err = w.predownloadDocumentThumbnail(doc)
				if err != nil {
					w.Log.Error(fmt.Sprintf("unable to downloadDocumentThumbnail: %s", err))
				} else {
					haveMedia = true
				}
			}
		}
	}

	// https://stackoverflow.com/questions/24987131/how-to-parse-unix-timestamp-to-time-time
	tm := time.Unix(int64(m.Date), 0)
	item := Item{
		MessageID:  m.ID,
		GroupedID:  m.GroupedID,
		Text:       highlightEntities(m.Message, m.Entities),
		Date:       tm,
		Webpage:    webpage,
		HasWebpage: webpage != nil,
	}
	if haveMedia {
		item.Media = append(item.Media, media)
	}

	return item, nil
}

func (w Worker) processPeer(ip tg.InputPeerClass) ([]Item, error) {
	var items []Item
	thresholdDate := time.Now().Unix() - w.DurationSeconds

	channel, err := decodeChannel(ip)
	if err != nil {
		return []Item{}, fmt.Errorf("unable to decodeChannel: %w", err)
	}

	channelTitle, channelDomain, err := w.getChannelInfo(channel)
	if err != nil {
		return []Item{}, fmt.Errorf("unable to getChannelFull: %w", err)
	}

	request := tg.MessagesGetHistoryRequest{Peer: ip}
	history, err := w.Client.MessagesGetHistory(w.Context, &request)
	if err != nil {
		return []Item{}, fmt.Errorf("MessagesGetHistory failed: %w", err)
	}

	messages, err := w.decodeMessages(history)
	if err != nil {
		// log.Debug(history.String())
		return []Item{}, fmt.Errorf("decodeMessages of type %q for channel %q failed: %w", history.TypeName(), channelTitle, err)
	}

	for _, m := range messages {
		if int64(m.Date) < thresholdDate {
			continue
		}

		item, _ := w.processMessage(m)
		item.Channel = Channel{Domain: channelDomain, Title: channelTitle}

		//
		// TODO: this part is not quite 100% working yet...
		//
		if fwdFrom, ok := m.GetFwdFrom(); ok {
			if fromName, ok := fwdFrom.GetFromName(); ok {
				item.FwdFrom = Channel{Domain: fromName}
			} else if fromName, ok := fwdFrom.GetPostAuthor(); ok {
				item.FwdFrom = Channel{Domain: fromName}
			} else {
				item.FwdFrom = Channel{Domain: "???"}
			}
			item.Forwarded = true
		}
		for i := range item.Media {
			item.Media[i].URL = tgUrl(item)
		}

		items = append(items, item)

		w.Log.Info(
			fmt.Sprintf(
				"handled MessageID %d (%s) from Domain %s (%d char)",
				item.MessageID,
				item.Date,
				item.Channel.Domain,
				len(item.Text),
			),
		)
	}

	return items, nil
}

func highlightEntities(message string, entities []tg.MessageEntityClass) string {
	//
	// FIXME: debug this function, still a bit buggy for some messages
	//
	// the offsets provided by GetOffset() and friends are in UTF-16 space, so
	// we have to take our UTF-8 message, decode it to runes (implicitly as part
	// of the range function), then encode to UTF-16 chunks.
	//
	// BTW UTF-16 is a variable-width format, so a character can occupy 1 or 2
	// bytes.
	//
	var chunks [][]uint16
	for _, char := range message {
		encoded := utf16.Encode([]rune{char})
		chunks = append(chunks, encoded)
	}

	var builder strings.Builder

	type Element struct {
		Tag string
		End int
	}

	var chunkIndex int = 0
	var entityIndex int = 0
	var stack []Element
	for _, chunk := range chunks {
		//
		// First, attempt to terminate any elements that end at the current
		// chunk.  There may be more than one.
		//
		for len(stack) > 0 {
			top := len(stack) - 1
			if stack[top].End == chunkIndex {
				builder.WriteString(stack[top].Tag)
				stack = stack[:top]
			} else {
				break
			}
		}

		haveMoreEntities := entityIndex < len(entities)
		if haveMoreEntities && entities[entityIndex].GetOffset() == chunkIndex {
			end := entities[entityIndex].GetOffset() + entities[entityIndex].GetLength()
			switch e := entities[entityIndex].(type) {
			case *tg.MessageEntityBold:
				builder.WriteString(fmt.Sprintf("<strong entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</strong>", End: end})
			case *tg.MessageEntityItalic:
				builder.WriteString(fmt.Sprintf("<em entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</em>", End: end})
			case *tg.MessageEntityStrike:
				builder.WriteString(fmt.Sprintf("<s entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</s>", End: end})
			case *tg.MessageEntityTextURL:
				builder.WriteString(fmt.Sprintf("<a entity='%d' href='%s' target='_blank'>", entityIndex, e.URL))
				stack = append(stack, Element{Tag: "</a>", End: end})
			case *tg.MessageEntityURL:
				//
				// Argh, now we have to work out what the URL is from the stuff
				// we've just encoded...
				//
				var urlChunks []uint16
				for i := 0; i < e.Length; i++ {
					if e.Offset+i >= len(chunks) {
						break
					}
					urlChunks = append(urlChunks, chunks[e.Offset+i]...)
				}
				var url []rune = utf16.Decode(urlChunks)
				builder.WriteString(fmt.Sprintf("<a entity='%d' href='%s' target='_blank'>", entityIndex, string(url)))
				stack = append(stack, Element{Tag: "</a>", End: end})
			}

			entityIndex += 1
		}

		var decodedChunk []rune = utf16.Decode(chunk)
		builder.WriteString(string(decodedChunk))

		chunkIndex += len(chunk)
	}

	//
	// Ensure we've closed all the tags that we've opened, just to be safe.
	//
	for len(stack) > 0 {
		top := len(stack) - 1
		builder.WriteString(stack[top].Tag)
		stack = stack[:top]
	}

	return builder.String()
}

func extractThumbnailSize(candidates []tg.PhotoSizeClass) (tg.PhotoSize, error) {
	//
	// https://core.telegram.org/api/files#downloading-files
	//
	var sizes []tg.PhotoSize
	for _, photoSizeClass := range candidates {
		switch photoSize := photoSizeClass.(type) {
		case *tg.PhotoSize:
			sizes = append(sizes, *photoSize)
		}
	}
	//
	// We want the smallest possible image, for now
	//
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].Size < sizes[j].Size })
	if len(sizes) > 0 {
		return sizes[0], nil
	}
	return tg.PhotoSize{}, fmt.Errorf("unable to find a suitable thumbnail size")
}

func groupItems(items []Item) (groups []Item) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].GroupedID < items[j].GroupedID
	})
	for i, item := range items {
		if i == 0 || item.GroupedID == 0 {
			groups = append(groups, item)
			continue
		}
		lastGroup := &groups[len(groups)-1]
		if item.GroupedID == lastGroup.GroupedID {
			lastGroup.Media = append(lastGroup.Media, item.Media[:]...)
			if len(lastGroup.Text) == 0 {
				lastGroup.Text = item.Text
			}
		} else {
			groups = append(groups, item)
		}
	}
	return groups
}

func main() {
	phone := flag.String("phone", "", "phone number to authenticate")
	channelsPath := flag.String("channels", "", "list of public channels to read, one per line")
	durationHours := flag.Int("hours", 28, "max age of messages to include, in hours")
	tmpPath := flag.String("tempdir", "/tmp", "where to cache image files")
	flag.Parse()

	var channels []string
	if *channelsPath != "" {
		f, err := os.Open(*channelsPath)
		if err != nil {
			log.Fatalf("unable to open %q: %s", *channelsPath, err)
		}
		defer f.Close()

		var reader *bufio.Reader = bufio.NewReader(f)
		for {
			ch, err := reader.ReadString('\n')
			if err == io.EOF {
				break
			} else if err != nil {
				log.Fatalf("error reading %q: %s", *channelsPath, err)
			} else {
				channels = append(channels, strings.TrimRight(ch, "\n"))
			}
		}
	}

	examples.Run(func(ctx context.Context, log *zap.Logger) error {
		// Setting up authentication flow helper based on terminal auth.
		flow := auth.NewFlow(
			termAuth{phone: *phone},
			auth.SendCodeOptions{},
		)

		client, err := telegram.ClientFromEnvironment(telegram.Options{
			Logger: log,
		})
		if err != nil {
			return err
		}
		return client.Run(ctx, func(ctx context.Context) error {
			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return err
			}

			log.Info("Login success")

			w := Worker{
				Client:          client.API(),
				Context:         ctx,
				DurationSeconds: int64(*durationHours * 3600),
				TmpPath:         *tmpPath,
				Log:             log,
			}
			var items []Item

			for _, username := range channels {
				peer, err := w.Client.ContactsResolveUsername(w.Context, username)
				if err != nil {
					log.Error(fmt.Sprintf("unable to resolve peer for username %q: %s", username, err))
					continue
				}
				switch chat := peer.Chats[0].(type) {
				case *tg.Channel:
					var ip tg.InputPeerChannel = tg.InputPeerChannel{
						ChannelID:  chat.ID,
						AccessHash: chat.AccessHash,
					}
					peerItems, err := w.processPeer(&ip)
					if err == nil {
						items = append(items, peerItems[:]...)
					} else {
						log.Error(fmt.Sprintf("processPeer failed: %s", err))
					}
				}
			}

			//
			// Download thumbnails.  At this stage the items are ungrouped,
			// so at most one Media per item.
			//
			log.Info("starting downloads")
			var success_counter, error_counter int
			for idx := range items {
				if len(items[idx].Media) > 0 && items[idx].Media[0].PendingDownload != nil {
					m := &items[idx].Media[0]
					path, err := w.downloadThumbnail(m.PendingDownload)
					if err == nil {
						m.PendingDownload = nil
						m.Thumbnail = path
						m.ThumbnailBase64 = imageAsBase64(path)
						success_counter++
					} else {
						error_counter++
					}
				}
			}
			log.Info(fmt.Sprintf("downloads complete, %d success %d failures", success_counter, error_counter))

			//
			// Some items are supposed to be grouped together, e.g. multiple photos in an album.
			//
			groupedItems := groupItems(items)

			sort.Slice(groupedItems, func(i, j int) bool {
				return groupedItems[i].Date.Unix() < groupedItems[j].Date.Unix()
			})
			var data struct {
				Items    []Item
				MaxIndex int
			}
			data.Items = groupedItems
			data.MaxIndex = len(groupedItems) - 1
			lenta.Execute(os.Stdout, data)

			return nil
		})
	})
}
