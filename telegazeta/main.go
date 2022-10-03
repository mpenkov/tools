// News reader for Telegram channels
//
// TODO:
//
// - [x] Pagination (we can only fetch 20 messages from a single channel at a time)
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
// - [x] Exclude cross-posts between covered channels (i.e. deduplicate messages)
// - [x] Keyboard shortcuts (j/k, etc)
// - [x] configurable cache location
// - [ ] try harder to split the first paragraph, maybe try a sentence split?
// - [x] Correctly identify and attribute forwarded messages
// - [x] Include photos/videos from forwarded messages
// - [x] Detect and handle FLOOD_WAIT responses
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

type Credentials struct {
	PhoneNumber string
	APIID       string
	APIHash     string
}

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

const (
	FLOOD_WAIT   int = 420
	NUM_ATTEMPTS int = 5
)

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

type Worker struct {
	Context         context.Context
	Log             *zap.Logger
	Client          *tg.Client
	TmpPath         string
	DumpPath        string
	DurationSeconds int64
	ChannelCache    map[int64]Channel
	RequestCounter  int
}

type WorkerRequest func() error

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

			//
			// Dump messages for collecting test data
			//
			if w.DumpPath != "" {
				var buf bin.Buffer
				err = message.Encode(&buf)
				if err != nil {
					w.Log.Error(fmt.Sprintf("unable to encode message %d: %s", message.ID, err))
					continue
				}

				path := fmt.Sprintf("%s/%d.bin", w.DumpPath, message.ID)
				fout, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
				if err != nil {
					w.Log.Error(fmt.Sprintf("unable to open %q for writing: %s", path, err))
					continue
				}
				defer fout.Close()

				fout.Write(buf.Buf)
				w.Log.Info(fmt.Sprintf("wrote %d bytes to %q", len(buf.Buf), path))
			}
		}
	}

	return messages, err
}

//
// Handle the request while being mindful of FLOOD_WAIT errors.
// If we encounter one of these, then attempt to retry intelligently after sleeping.
//
func (w Worker) handleRequest(request WorkerRequest, attempts int) error {
	var err error

	if attempts == 0 {
		attempts = NUM_ATTEMPTS
	}

	//
	// It's a bit difficult to get a handle on the real error because we can't
	// reproduce it reliably, so we just use a regex to fish out the details.
	//
	expr := regexp.MustCompile(`rpc error code 420: FLOOD_WAIT \((\d+)\)`)

	w.RequestCounter++

	for attempt := 0; attempt < attempts; attempt++ {
		err = request()
		if err == nil {
			return nil
		}

		w.Log.Info(fmt.Sprintf("handleRequest err: %q (%T)", err, err))
		submatches := expr.FindStringSubmatch(err.Error())

		if len(submatches) == 2 {
			//
			// The time we should sleep depends on how many requests we've made
			// since the last FLOOD_WAIT, as long as the error text, e.g.
			// FLOOD_WAIT_5
			//
			// https://docs.madelineproto.xyz/docs/FLOOD_WAIT.html
			//
			var sleepSeconds int = w.RequestCounter
			if s, err := strconv.Atoi(submatches[1]); err == nil {
				sleepSeconds += s
			}
			sleepDuration := time.Duration(sleepSeconds) * time.Second
			w.Log.Info(fmt.Sprintf("sleeping for %s before retrying", sleepDuration))
			time.Sleep(sleepDuration)
			w.RequestCounter = 0
			continue
		}

		//
		// Not a FLOOD_WAIT error, so give up immediately.
		//
		w.Log.Info(fmt.Sprintf("unrecoverable error: %q (%T)", err, err))
		return err
	}

	return fmt.Errorf("gave up after %d attempts, last error: %w", NUM_ATTEMPTS, err)
}

func (w Worker) getChannelInfo(input tg.InputChannelClass) (string, string, error) {
	var result *tg.MessagesChatFull

	request := func() error {
		var err error
		result, err = w.Client.ChannelsGetFullChannel(w.Context, input)
		return err
	}

	err := w.handleRequest(request, NUM_ATTEMPTS)
	if err != nil {
		return "", "", fmt.Errorf("ChannelsGetFullChannel failed: %w", err)
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
		w.RequestCounter++
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

	item := mkitem(&m)
	if webpage != nil {
		item.Webpage = webpage
		item.HasWebpage = true
	}
	if haveMedia {
		item.Media = append(item.Media, media)
	}

	return item, nil
}

func (w Worker) processPeer(ip tg.InputPeerClass) ([]Item, error) {
	var items []Item
	thresholdDate := time.Now().Unix() - w.DurationSeconds

	var channelTitle string
	var channelDomain string
	var err error

	inputPeerChannel, ok := ip.(*tg.InputPeerChannel)
	if !ok {
		return []Item{}, fmt.Errorf("unable to processPeer: %s", ip)
	}
	inputChannel := tg.InputChannel{
		AccessHash: inputPeerChannel.AccessHash,
		ChannelID:  inputPeerChannel.ChannelID,
	}
	channelTitle, channelDomain, err = w.getChannelInfo(&inputChannel)
	w.Log.Info(fmt.Sprintf("title: %q domain: %q err: %s", channelTitle, channelDomain, err))
	if err != nil {
		return []Item{}, fmt.Errorf("unable to getChannelInfo: %w", err)
	}

	w.ChannelCache[inputChannel.ChannelID] = Channel{channelDomain, channelTitle}

	//
	// Page through the message history until we reach messages that are too old.
	// Telegram typically serves 20 messages per request.
	//
	offset := 0
	messages := []tg.Message{}
	for {
		var history tg.MessagesMessagesClass
		request := func() error {
			var err error
			getHistoryRequest := tg.MessagesGetHistoryRequest{Peer: ip, AddOffset: offset}
			history, err = w.Client.MessagesGetHistory(w.Context, &getHistoryRequest)
			return err
		}
		err := w.handleRequest(request, NUM_ATTEMPTS)
		if err != nil {
			return []Item{}, fmt.Errorf("MessagesGetHistory failed: %w", err)
		}

		response, err := w.decodeMessages(history)
		if err != nil {
			// log.Debug(history.String())
			return []Item{}, fmt.Errorf("decodeMessages of type %q for channel %q failed: %w", history.TypeName(), channelTitle, err)
		}

		stop := false
		for _, m := range response {
			if int64(m.Date) < thresholdDate {
				stop = true
				break
			} else {
				messages = append(messages, m)
			}
		}

		if stop {
			break
		} else {
			offset += len(response)
		}
	}

	for _, m := range messages {
		item, _ := w.processMessage(m)
		item.Channel = Channel{Domain: channelDomain, Title: channelTitle}

		if fwdFrom, ok := m.GetFwdFrom(); ok {
			var chid int64

			switch fromPeer := fwdFrom.FromID.(type) {
			case *tg.PeerChannel:
				chid = fromPeer.ChannelID
			}

			if channelInfo, ok := w.ChannelCache[chid]; ok {
				item.FwdFrom = channelInfo
				item.Forwarded = true
			} else {
				inputChannel := tg.InputChannelFromMessage{
					ChannelID: chid,
					MsgID:     m.ID,
					Peer:      ip,
				}

				title, username, err := w.getChannelInfo(&inputChannel)
				if err == nil {
					item.FwdFrom = Channel{Domain: username, Title: title}
					item.Forwarded = true
					w.ChannelCache[chid] = item.FwdFrom
				}
			}
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

func mkitem(m *tg.Message) Item {
	// https://stackoverflow.com/questions/24987131/how-to-parse-unix-timestamp-to-time-time
	tm := time.Unix(int64(m.Date), 0)
	item := Item{
		MessageID: m.ID,
		GroupedID: m.GroupedID,
		Text:      highlightEntities(m.Message, m.Entities),
		Date:      tm,
	}
	return item
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

func dedup(items []Item) (uniq []Item) {
	//
	// Deduplicating based on the full message text isn't ideal, because if the
	// source of the forward edits their message, then the texts are now
	// different.
	//
	// A better way would be to deduplicate on the datestamps, but it's tricky,
	// because we don't have the datestamp of the original message available
	// here.  Using the message prefix (40 chars or so) may be enough.
	//
	var seen = make(map[string]bool)
	for _, item := range items {
		//
		// Argh, no min function in golang...
		//
		numChars := 40
		if len(item.Text) < numChars {
			numChars = len(item.Text)
		}
		key := fmt.Sprintf("%s", item.Text[:numChars])
		if _, ok := seen[key]; ok {
			continue
		}
		uniq = append(uniq, item)

		//
		// Some items contain photos only, we don't want to discard them here
		//
		if len(item.Text) > 0 {
			seen[key] = true
		}
	}
	return uniq
}

func main() {
	phone := flag.String("phone", "", "phone number to authenticate")
	channelsPath := flag.String("channels", "", "list of public channels to read, one per line")
	durationHours := flag.Int("hours", 24, "max age of messages to include, in hours")
	tmpPath := flag.String("tempdir", "/tmp", "where to cache image files")
	dumpPath := flag.String("dumpdir", "", "where to dump messages")
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
				ChannelCache:    make(map[int64]Channel),
				Client:          client.API(),
				Context:         ctx,
				DumpPath:        *dumpPath,
				DurationSeconds: int64(*durationHours * 3600),
				TmpPath:         *tmpPath,
				Log:             log,
			}
			var items []Item

			for _, username := range channels {
				log.Info(fmt.Sprintf("processing username: %q", username))

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
			// Sort before deduplication to favor original (non-forwarded)
			// messages
			//
			sort.Slice(items, func(i, j int) bool {
				return items[i].Date.Unix() < items[j].Date.Unix()
			})
			before := len(items)
			items = dedup(items)
			log.Info(fmt.Sprintf("removed %d items as duplicates", before-len(items)))

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
