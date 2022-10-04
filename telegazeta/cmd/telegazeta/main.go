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
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/go-faster/errors"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"github.com/mpenkov/tools/telegazeta"
)

type Credentials struct {
	PhoneNumber string
	APIID       string
	APIHash     string
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

			w := telegazeta.Worker{
				Client:          client.API(),
				Context:         ctx,
				DumpPath:        *dumpPath,
				DurationSeconds: int64(*durationHours * 3600),
				TmpPath:         *tmpPath,
				Log:             log,
			}

			items := w.Collect(channels)

			var data struct {
				Items    telegazeta.ItemList
				MaxIndex int
			}
			data.Items = items
			data.MaxIndex = len(items) - 1
			telegazeta.Template.Execute(os.Stdout, data)

			return nil
		})
	})
}
