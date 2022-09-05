// News reader for Telegram channels
//
// TODO:
//
// - [ ] Pagination
// - [ ] Output HTML instead of text
// - [ ] Output media files when available
// - [ ] Configuration file (contain phone number, secrets, channel IDs, etc) - avoid signing up to public channels
// - [ ] Show channel thumbnails
// - [ ] Markup for hyperlinks, etc. - why isn't this already in the message text?
// - [ ] Parametrize threshold for old news
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
	"github.com/gotd/td/tg"

	"github.com/gotd/td/bin"
)

type Item struct {
	Domain string
	MessageID int
	ChannelTitle string
	Text string
	Date string
}

func output(items []Item) {
	for _, item := range items {
		fmt.Println(fmt.Sprintf("[%s] %s (tg://resolve?domain=%s&post=%d)", item.Date, item.ChannelTitle, item.Domain, item.MessageID))
		fmt.Println()
		fmt.Println(item.Text)
		fmt.Println("\n---")
		fmt.Println()
	}
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

func decodeFolder(dfc tg.DialogFilterClass) (tg.DialogFilter, error) {
	var buf bin.Buffer
	var folder tg.DialogFilter

	err := dfc.Encode(&buf)
	if err != nil {
		return folder, err
	}

	err = folder.Decode(&buf)
	if err != nil {
		return folder, err
	}

	return folder, nil
}

func decodeChannel(inputPeerClass tg.InputPeerClass) (channel tg.InputChannel, err error) {
	var buffer bin.Buffer
	var inputPeerChannel tg.InputPeerChannel

	err = inputPeerClass.Encode(&buffer)
	if err != nil {
		return channel, err
	}

	err = inputPeerChannel.Decode(&buffer)
	if err != nil {
		return channel, err
	}

	channel.ChannelID = inputPeerChannel.ChannelID
	channel.AccessHash = inputPeerChannel.AccessHash

	return channel, nil
}

func decodeMessages(mmc tg.MessagesMessagesClass) (messages []tg.Message, err error) {
	var buffer bin.Buffer

	err = mmc.Encode(&buffer)
	if err != nil {
		return messages, fmt.Errorf("MessagesMessagesClass failed to encode: %w", err)
	}

	var innerMessages []tg.MessageClass

	switch mmc.(type) {
	case *tg.MessagesChannelMessages:
		var inner tg.MessagesChannelMessages
		err = inner.Decode(&buffer)
		if err != nil {
			return messages, fmt.Errorf("MessagesChannelMessages failed to decode: %w", err)
		}
		innerMessages = inner.Messages	
	case *tg.MessagesMessagesSlice:
		var inner tg.MessagesMessagesSlice
		err = inner.Decode(&buffer)
		if err != nil {
			return messages, fmt.Errorf("MessagesMessagesSlice failed to decode: %w", err)
		}
		innerMessages = inner.Messages	
	}

	for _, m := range(innerMessages) {
		//
		// If stuff fails to decode here, then just ignore the message
		//
		var legitMessage tg.Message
		ignoreErr := m.Encode(&buffer)
		if ignoreErr != nil {
			continue
		}
		ignoreErr = legitMessage.Decode(&buffer)
		if ignoreErr != nil {
			continue
		}
		messages = append(messages, legitMessage)
	}

	return messages, err
}

func getChannelInfo(input tg.InputChannel, client *tg.Client, ctx context.Context) (string, string, error) {
	var result *tg.MessagesChatFull
	var buf bin.Buffer

	result, err := client.ChannelsGetFullChannel(ctx, &input)
	if err != nil {
		return "", "", err
	}

	err = result.Chats[0].Encode(&buf)
	if err != nil {
		return "", "", err
	}

	switch result.Chats[0].(type) {
	case *tg.Chat:
		var chat tg.Channel
		err = chat.Decode(&buf)
		if err != nil {
			return "", "", err
		}
		return chat.Title, "", nil
	case *tg.Channel:
		var channel tg.Channel
		err = channel.Decode(&buf)
		if err != nil {
			return "", "", err
		}
		return channel.Title, channel.Username, nil
	}

	return "", "", fmt.Errorf("not implemented yet")
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
			var items []Item

			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return err
			}

			log.Info("Login success")

			folders, err := client.API().MessagesGetDialogFilters(ctx)
			if err != nil {
				return err
			}

			for _, dfc := range folders {
				folder, err := decodeFolder(dfc)
				if err != nil {
					log.Error(fmt.Sprintf("unable to decodeFolder: %s", err))
					continue
				}

				if folder.Title == "News" {	
					for _, ip := range folder.IncludePeers {
						channel, err := decodeChannel(ip)
						if err != nil {
							log.Error(fmt.Sprintf("unable to decodeChannel: %s", err))
							continue
						}

						channelTitle, channelDomain, err := getChannelInfo(channel, client.API(), ctx)
						if err != nil {
							log.Error(fmt.Sprintf("unable to getChannelFull: %s", err))
							continue
						}

						request := tg.MessagesGetHistoryRequest{Peer: ip}
						history, err := client.API().MessagesGetHistory(ctx, &request)
						if err != nil {
							log.Error(fmt.Sprintf("MessagesGetHistory failed: %s", err))
							continue
						} 

						messages, err := decodeMessages(history)
						if err != nil {
							// log.Debug(history.String())
							log.Error(fmt.Sprintf("decodeMessages of type %q for channel %q failed: %s", history.TypeName(), channelTitle, err))
							continue
						}

						thresholdDate := time.Now().Unix() - 24 * 3600

						for _, m := range messages {
							if int64(m.Date) < thresholdDate {
								// old news from over 24 hours ago
								continue
							}
							// https://stackoverflow.com/questions/24987131/how-to-parse-unix-timestamp-to-time-time
							tm := time.Unix(int64(m.Date), 0)
							dateStr := tm.Format("2006-01-02 15:04:05")
							item := Item{
								Domain: channelDomain,
								MessageID: m.ID,
								ChannelTitle: channelTitle, 
								Text: m.Message,
								Date: dateStr,
							}
							items = append(items, item)
						}
					}
				}
			}

			sort.Slice(items, func (i, j int) bool {
				return items[i].Date < items[j].Date
			})
			output(items)

			return nil
		})
	})
}
