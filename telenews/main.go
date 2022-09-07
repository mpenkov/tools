// News reader for Telegram channels
//
// TODO:
//
// - [ ] Pagination
// - [x] Output HTML instead of text
// - [ ] Use proper Golang templates when outputting HTML
// - [x] Extract headlines (first line in the message) and mark them up with CSS
// - [.] Output media files when available (photos, caching, etc)
// - [ ] Embed images into the HTML so that it is fully self-contained
// - [ ] Configuration file (contain phone number, secrets, channel IDs, etc) - avoid signing up to public channels
// - [ ] Show channel thumbnails
// - [ ] Markup for hyperlinks, etc. - why isn't this already in the message text?
// - [ ] Parametrize threshold for old news
// - [ ] Exclude cross-posts between covered channels
// - [ ] Keyboard shortcuts (j/k, etc)
//
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

type Item struct {
	Domain       string
	MessageID    int
	ChannelTitle string
	Text         string
	Date         time.Time
	Webpage      *tg.WebPage
	Images       []string
}

//
// A bunch of stuff we frequently pass around together
//
type WorkArea struct {
	Context context.Context
	Log     *zap.Logger
	Client  *tg.Client
}

func output(items []Item) {
	fmt.Println("<!DOCTYPE html>")
	fmt.Println("<html>")
	fmt.Println("<head><style>")
	fmt.Println("body { font-family: Helvetica; }")
	fmt.Println(".item-list { display: grid; grid-gap: 15px; }")
	fmt.Println(".item { display: grid; grid-template-columns: 100px 200px 800px 300px; border-top: 1px solid gray}")
	fmt.Println("p:nth-child(1) { font-weight: bold; font-size: large }")
	fmt.Println(".channel { font-size: large; font-weight: bold }")
	fmt.Println(".datestamp { font-size: xx-large; font-weight: bold; color: gray }")
	fmt.Println("</style></head>")
	fmt.Println("<body><div class='item-list'>")
	for _, item := range items {
		if len(item.Text) == 0 && item.Webpage == nil {
			//
			// Empty message?  Why is it empty?  Hide these from the output for now.
			//
			continue
		}
		dateStr := item.Date.Format("15:04")
		msgUrl := fmt.Sprintf("tg://resolve?domain=%s&post=%d", item.Domain, item.MessageID)

		fmt.Println("<span class='item'>")
		fmt.Println(fmt.Sprintf("<span class='datestamp'>%s</span>", dateStr))
		fmt.Println(fmt.Sprintf("<span class='channel'><a href='%s'>@%s</a></span>", msgUrl, item.Domain))
		fmt.Println(fmt.Sprintf("<span class='message'>%s", markup(item.Text)))
		if item.Webpage != nil {
			fmt.Println("<blockquote class='webpage'>")
			fmt.Println(fmt.Sprintf("<span class='link'><a href='%s'>%s</a></span>", item.Webpage.URL, item.Webpage.Title))
			fmt.Println(fmt.Sprintf("<span class='description'>%s</span>", markup(item.Webpage.Description)))
			fmt.Println("</blockquote>")
		}
		fmt.Println("</span>") // message

		if len(item.Images) > 0 {
			fmt.Println("<span class='images'>")
			for _, img := range item.Images {
				fmt.Println(fmt.Sprintf("<img src=%q></img>", img))
			}
			fmt.Println("</span>")
		}
		fmt.Println("</span>") // item
	}
	fmt.Println("</div></body></html>")
}

func markup(message string) string {
	paragraphs := strings.Split(message, "\n")
	// FIXME: very inefficient string concatenation
	// TODO: try harder to split the first paragraph, maybe try a sentence split?
	nonono := ""
	for _, p := range paragraphs {
		if len(p) > 0 {
			nonono = nonono + "<p>" + p + "</p>\n"
		}
	}
	return nonono
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

func extractThumbnailLocation(photo tg.Photo, wa WorkArea) tg.InputPhotoFileLocation {
	wa.Log.Debug(photo.String())
	//
	// https://core.telegram.org/api/files#downloading-files
	//
	var sizes []tg.PhotoSize
	for _, photoSizeClass := range photo.Sizes {
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
		location := tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     sizes[0].Type,
		}
		wa.Log.Debug(fmt.Sprintf("location: %s", location.String()))
		return location
	}

	return tg.InputPhotoFileLocation{}
}

func processMessage(m tg.Message, wa WorkArea) (Item, error) {
	var webpage *tg.WebPage
	var thumbnailLocation tg.InputPhotoFileLocation

	if m.Media != nil {
		wa.Log.Debug("attachment: " + m.Media.TypeName())

		switch messageMedia := m.Media.(type) {
		case *tg.MessageMediaPhoto:
			switch photo := messageMedia.Photo.(type) {
			case *tg.Photo:
				thumbnailLocation = extractThumbnailLocation(*photo, wa)
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
		}
	}

	// TODO: configurable cache location
	//
	var images []string
	if thumbnailLocation.ID != 0 {
		path := fmt.Sprintf("/tmp/%d.jpeg", thumbnailLocation.ID)
		_, err := os.Stat(path)
		if err != nil {
			dloader := downloader.NewDownloader()
			builder := dloader.Download(wa.Client, &thumbnailLocation)
			builder.ToPath(wa.Context, path)
		}
		images = append(images, path)
	}

	// https://stackoverflow.com/questions/24987131/how-to-parse-unix-timestamp-to-time-time
	tm := time.Unix(int64(m.Date), 0)
	item := Item{
		MessageID: m.ID,
		Text:      m.Message,
		Date:      tm,
		Webpage:   webpage,
		Images:    images,
	}

	return item, nil
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
		items = append(items, item)

		wa.Log.Info(
			fmt.Sprintf(
				"handled MessageID %d (%s) from Domain %s (%d char %d images)",
				item.MessageID,
				item.Date,
				item.Domain,
				len(item.Text),
				len(item.Images),
			),
		)
	}

	return items, nil
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

			sort.Slice(items, func(i, j int) bool {
				return items[i].Date.Unix() < items[j].Date.Unix()
			})
			output(items)

			return nil
		})
	})
}
