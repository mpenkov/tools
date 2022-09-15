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
// - [ ] Embed images into the HTML so that it is fully self-contained
// - [ ] Configuration file (contain phone number, secrets, channel IDs, etc) - avoid signing up to public channels
// - [ ] Show channel thumbnails
// - [x] Markup for hyperlinks, etc. using entities from the Message
// - [ ] Parametrize threshold for old news
// - [ ] Exclude cross-posts between covered channels (i.e. deduplicate messages)
// - [x] Keyboard shortcuts (j/k, etc)
// - [ ] configurable cache location
// - [ ] try harder to split the first paragraph, maybe try a sentence split?
// - [ ] Correctly identify and attribute forwarded messages
// - [ ] Include photos/videos from forwarded messages
//
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"html/template"
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
	URL             template.URL
	ThumbnailWidth  int
	ThumbnailHeight int
	Duration        string
}

type Item struct {
	Domain       string
	MessageID    int
	GroupedID    int64
	ChannelTitle string
	Text         string
	Date         time.Time
	Webpage      *tg.WebPage
	HasWebpage   bool
	Media        []Media
}

//
// A bunch of stuff we frequently pass around together
//
type WorkArea struct {
	Context context.Context
	Log     *zap.Logger
	Client  *tg.Client
}

const templ = `
<!DOCTYPE html>
<html>
	<head>
		<style>
			body { font-family: Helvetica; }
			.item-list { display: grid; grid-gap: 15px; }
			.item {
				display: grid;
				grid-template-columns: 100px 200px 1000px;
				border-top: 1px solid gray;
			}
			.item:nth-child(odd) { background-color: hsl(0, 0%, 90%); }
			.channel { font-size: large; font-weight: bold }
			.datestamp { font-size: xx-large; font-weight: bold; color: gray }
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

			.datestamp { margin: 10px; }
			.datestamp > a { text-decoration: none; }
			span.time { font-size: xx-large; font-weight: bold;  }
			span.date { font-size:  medium; }
			.channel { margin-top: 10px; display: flex; flex-direction: column; gap: 10px; }
			.channel-title { font-size: small; color: gray; }
			.message p { margin-top: 10px; }

			a { color: darkred; }
			a:hover { color: red; }
		</style>
	</head>
	<body>
		<div class='item-list'>
			{{range $index, $item := .Items}}
			<span class='item' id="item-{{$index}}">
				<span class='datestamp'>
					<a href='{{$item | tgUrl}}'>
						<span class="time">{{$item.Date | formatTime}}</span>
						<span class="date">{{$item.Date | formatDate}}</span>
					</a>
				</span>
				<span class='channel'>
					<span class="domain">@{{$item.Domain}}</span>
					<span class="channel-title">({{$item.ChannelTitle}})</span>
				</span>
				<span class='message'>
					{{$item.Text | markup}}
				{{if $item.HasWebpage}}
					<blockquote class='webpage'>
						<span class='link'><a href='{{$item.Webpage.URL}}'>{{$item.Webpage.Title}}</a></span>
						<span class='description'>{{$item.Webpage.Description | markup}}</span>
					</blockquote>
				{{end}}
					<span class="thumbnails">
				{{range $item.Media}}
					{{if .IsVideo}}
						{{if .Thumbnail}}
						<span class='image'>
							<span class='container'>
								<a href='{{.URL}}'>
									<img class="video-thumbnail" src='{{.Thumbnail}}' width="{{.ThumbnailWidth}}" Height="{{.ThumbnailHeight}}"></img>
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
							<a href='{{.URL}}'><img class="image-thumbnail" src='{{.Thumbnail}}'></img></a>
						</span>
					{{end}}
				{{end}}
					</span>
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
		if (checkVisible(items[i])) {
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
	return date.Format("2 Jan", )
}

func formatTime(date time.Time) string {
	return date.Format("15:04")
}

func tgUrl(item Item) template.URL {
	return template.URL(fmt.Sprintf("tg://resolve?domain=%s&post=%d", item.Domain, item.MessageID))
}

var mapping = template.FuncMap{"formatDate": formatDate, "tgUrl": tgUrl, "markup": markup, "formatTime": formatTime}
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

func decodeMessages(mmc tg.MessagesMessagesClass, wa WorkArea) (messages []tg.Message, err error) {
	var innerMessages []tg.MessageClass

	switch inner := mmc.(type) {
	case *tg.MessagesChannelMessages:
		innerMessages = inner.Messages
		break
	case *tg.MessagesMessagesSlice:
		innerMessages = inner.Messages
		break
	}

	for _, m := range innerMessages {
		wa.Log.Debug(m.String())
		switch message := m.(type) {
		case *tg.Message:
			messages = append(messages, *message)
			break
		}
	}

	return messages, err
}

func getChannelInfo(input tg.InputChannel, wa WorkArea) (string, string, error) {
	var result *tg.MessagesChatFull

	result, err := wa.Client.ChannelsGetFullChannel(wa.Context, &input)
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

func extractThumbnailSize(candidates []tg.PhotoSizeClass) (tg.PhotoSize, error) {
	//
	// https://core.telegram.org/api/files#downloading-files
	//
	var sizes []tg.PhotoSize
	for _, photoSizeClass := range candidates {
		switch photoSize := photoSizeClass.(type) {
		case *tg.PhotoSize:
			sizes = append(sizes, *photoSize)
			break
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

func downloadThumbnail(id int64, location tg.InputFileLocationClass, wa WorkArea) (string, error) {
	path := fmt.Sprintf("/tmp/%d.jpeg", id)
	_, err := os.Stat(path)
	if err == nil {
		//
		// File exists, we don't need to download
		//
		return path, nil
	} else {
		wa.Log.Info(fmt.Sprintf("downloading thumbnail for id %d", id))
		dloader := downloader.NewDownloader()
		builder := dloader.Download(wa.Client, location)
		_, err := builder.ToPath(wa.Context, path)
		if err == nil {
			return path, nil
		} else {
			//
			// TODO: handle error, e.g. https://core.telegram.org/api/file_reference
			//
			return "", err
		}
	}
}

func processMessage(m tg.Message, wa WorkArea) (Item, error) {
	var webpage *tg.WebPage
	var media Media

	if m.Media != nil {
		wa.Log.Debug("attachment: " + m.Media.TypeName())

		switch messageMedia := m.Media.(type) {
		case *tg.MessageMediaPhoto:
			switch photo := messageMedia.Photo.(type) {
			case *tg.Photo:
				thumbnailSize, err := extractThumbnailSize(photo.Sizes)
				if err != nil {
					wa.Log.Error(fmt.Sprintf("unable to extract thumbnail: %s", err))
				} else {
					location := tg.InputPhotoFileLocation{
						ID:            photo.ID,
						AccessHash:    photo.AccessHash,
						FileReference: photo.FileReference,
						ThumbSize:     thumbnailSize.Type,
					}
					media.ThumbnailWidth = thumbnailSize.W
					media.ThumbnailHeight = thumbnailSize.H

					path, err := downloadThumbnail(photo.ID, &location, wa)
					if err == nil {
						media.Thumbnail = path
					} else {
						//
						// TODO: handle error, e.g. https://core.telegram.org/api/file_reference
						//
						wa.Log.Error(fmt.Sprintf("download error: %s", err))
					}
				}
				break
			}
			break
		case *tg.MessageMediaWebPage:
			// Quoting another telegram channel?
			switch wp := messageMedia.Webpage.(type) {
			case *tg.WebPage:
				webpage = wp
				break
			}
		case *tg.MessageMediaDocument:
			switch doc := messageMedia.Document.(type) {
			case *tg.Document:
				if strings.HasPrefix(doc.MimeType, "video/") {
					media.IsVideo = true
					for _, attr := range doc.Attributes {
						switch a := attr.(type) {
						case *tg.DocumentAttributeVideo:
							minutes := a.Duration / 60
							seconds := a.Duration % 60
							media.Duration = fmt.Sprintf("%02d:%02d", minutes, seconds)
							break
						}
					}
				}

				thumbnailSize, err := extractThumbnailSize(doc.Thumbs)
				if err != nil {
					wa.Log.Error(fmt.Sprintf("unable to extract thumbnail: %s", err))
				} else {
					media.ThumbnailWidth = thumbnailSize.W
					media.ThumbnailHeight = thumbnailSize.H

					location := tg.InputDocumentFileLocation{
						ID:            doc.ID,
						AccessHash:    doc.AccessHash,
						FileReference: doc.FileReference,
						ThumbSize:     thumbnailSize.Type,
					}
					path, err := downloadThumbnail(doc.ID, &location, wa)
					if err == nil {
						media.Thumbnail = path
					} else {
						//
						// TODO: handle error, e.g. https://core.telegram.org/api/file_reference
						//
						wa.Log.Error(fmt.Sprintf("download error: %s", err))
					}
				}
				break
			}
			break
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
	item.Media = append(item.Media, media)

	return item, nil
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
			switch e := entities[entityIndex].(type) {
			case *tg.MessageEntityBold:
				builder.WriteString(fmt.Sprintf("<strong entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</strong>", End: e.Offset + e.Length})
				break
			case *tg.MessageEntityItalic:
				builder.WriteString(fmt.Sprintf("<em entity='%d'>", entityIndex))
				stack = append(stack, Element{Tag: "</em>", End: e.Offset + e.Length})
				break
			case *tg.MessageEntityTextURL:
				builder.WriteString(fmt.Sprintf("<a entity='%d' href='%s'>", entityIndex, e.URL))
				stack = append(stack, Element{Tag: "</a>", End: e.Offset + e.Length})
				break
			case *tg.MessageEntityURL:
				//
				// Argh, now we have to work out what the URL is from the stuff
				// we've just encoded...
				//
				var urlChunks []uint16
				for i := 0; i < e.Length; i++ {
					if e.Offset + i >= len(chunks) {
						break
					}
					urlChunks = append(urlChunks, chunks[e.Offset + i]...)
				}
				var url []rune = utf16.Decode(urlChunks)
				builder.WriteString(fmt.Sprintf("<a entity='%d' href='%s'>", entityIndex, string(url)))
				stack = append(stack, Element{Tag: "</a>", End: e.Offset + e.Length})
				break
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

func processPeer(ip tg.InputPeerClass, wa WorkArea) ([]Item, error) {
	var items []Item
	thresholdDate := time.Now().Unix() - 24*3600

	channel, err := decodeChannel(ip)
	if err != nil {
		return []Item{}, fmt.Errorf("unable to decodeChannel: %w", err)
	}

	channelTitle, channelDomain, err := getChannelInfo(channel, wa)
	if err != nil {
		return []Item{}, fmt.Errorf("unable to getChannelFull: %w", err)
	}

	request := tg.MessagesGetHistoryRequest{Peer: ip}
	history, err := wa.Client.MessagesGetHistory(wa.Context, &request)
	if err != nil {
		return []Item{}, fmt.Errorf("MessagesGetHistory failed: %w", err)
	}

	messages, err := decodeMessages(history, wa)
	if err != nil {
		// log.Debug(history.String())
		return []Item{}, fmt.Errorf("decodeMessages of type %q for channel %q failed: %w", history.TypeName(), channelTitle, err)
	}

	for _, m := range messages {
		if int64(m.Date) < thresholdDate {
			// old news from over 24 hours ago
			continue
		}

		item, _ := processMessage(m, wa)
		item.Domain = channelDomain
		item.ChannelTitle = channelTitle
		for i := range item.Media {
			item.Media[i].URL = tgUrl(item)
		}

		items = append(items, item)

		wa.Log.Info(
			fmt.Sprintf(
				"handled MessageID %d (%s) from Domain %s (%d char)",
				item.MessageID,
				item.Date,
				item.Domain,
				len(item.Text),
			),
		)
	}

	return items, nil
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
	flag.Parse()

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

			wa := WorkArea{Client: client.API(), Log: log, Context: ctx}
			folders, err := wa.Client.MessagesGetDialogFilters(ctx)
			if err != nil {
				return err
			}

			var items []Item

			for _, dfc := range folders {
				var folder tg.DialogFilter
				ok := false
				switch f := dfc.(type) {
				case *tg.DialogFilter:
					folder = *f
					ok = true
				}
				if !ok {
					continue
				}

				if folder.Title == "News" {
					for _, ip := range folder.IncludePeers {
						peerItems, err := processPeer(ip, wa)
						if err == nil {
							items = append(items, peerItems[:]...)
						} else {
							log.Error(fmt.Sprintf("processPeer failed: %s", err))
						}
					}
				}
			}

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
