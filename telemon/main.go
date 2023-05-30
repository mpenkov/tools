package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	mime "github.com/ProtonMail/go-mime"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/text/encoding/japanese"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/examples"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

type IMAPConfig struct {
	Host           string
	Port           int
	User           string
	Password       string
	Folder         string
	MaxCount       uint32
	SubjectFilters []string
}

type TelegramConfig struct {
	PhoneNumber string
	APIID       int
	APIHash     string
	ChannelName string
}

type Config struct {
	IMAP     IMAPConfig
	Telegram TelegramConfig
	TempDir  string
}

func loadConfig(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		log.Fatal(err)
	}

	return config
}

//
// extract the email body as a string
//
func emailBody(message io.Reader) string {
	msg, err := mail.ReadMessage(message)
	if err != nil {
		log.Fatal(err)
	}

	if false {
		keys := []string{}
		for key := range msg.Header {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Printf("%s: %q\n", key, msg.Header[key])
		}
	}

	var body []byte
	switch msg.Header.Get("Content-Transfer-Encoding") {
	case "quoted-printable":
		data, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
		if err != nil {
			log.Fatal(err)
		}
		body = data
	case "base64":
		data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
		if err != nil {
			log.Fatal(err)
		}
		body = data
	default:
		data, err := io.ReadAll(msg.Body)
		if err != nil {
			log.Fatal(err)
		}
		body = data
	}

	//
	// TODO: handle other encodings?
	//
	if strings.Index(msg.Header.Get("Content-Type"), "ISO-2022-JP") != -1 {
		decoded, err := japanese.ISO2022JP.NewDecoder().Bytes(body)
		if err != nil {
			log.Fatal(err)
		}
		body = decoded
	}

	bodyString := string(body)

	if strings.Index(bodyString, "『ツイタもん』") != -1 {
		//
		// Additional business logic for the tsuitamon notifications.
		// Strip the footer, insert a datestamp.
		//
		lines := strings.Split(bodyString, "\r\n")

		// Fri, 26 May 2023 07:57:11 +0000
		parsedDate, err := time.Parse(time.RFC1123, msg.Header.Get("Date"))
		if err != nil {
			log.Fatal(err)
		}
		dow := []string{"日", "月", "火", "水", "木", "金", "土"}
		bodyString = fmt.Sprintf(
			"%s(%s)、%s",
			parsedDate.Format("1月2日"),
			dow[int(parsedDate.Weekday())],
			strings.Join(lines[1:4], ""),
		)
	}

	return strings.Trim(bodyString, "\n")
}

//
// fetch the envelopes for the most recent messages (no message body).
//
func fetchMostRecentMessageEnvelopes(c *client.Client, from uint32, to uint32) ([]*imap.Message, error) {
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	items := []imap.FetchItem{imap.FetchEnvelope}

	messages := make(chan *imap.Message, to-from)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	result := []*imap.Message{}
	for msg := range messages {
		result = append(result, msg)
	}

	if err := <-done; err != nil {
		return []*imap.Message{}, err
	}

	return result, nil
}

//
// fetch the specified messages in their entirety
//
func fetchMessages(c *client.Client, seqNums []uint32) (result []*imap.Message, err error) {
	seqset := new(imap.SeqSet)
	seqset.AddNum(seqNums...)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}

	messages := make(chan *imap.Message, len(seqNums))
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	for msg := range messages {
		result = append(result, msg)
	}

	if err := <-done; err != nil {
		return result, err
	}

	return result, nil
}

func lastSeenSeqNum(tempDir string) uint32 {
	data, err := os.ReadFile(tempDir + "/seqnum")
	if err != nil {
		return 0
	}

	var seqnum uint32
	_, err = fmt.Sscanf(string(data), "%d\n", &seqnum)
	if err != nil {
		return 0
	}

	return seqnum
}

func writeLastSeenSeqNum(tempDir string, seqNum uint32) error {
	err := os.MkdirAll(tempDir, 0o700)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%d\n", seqNum)
	err = os.WriteFile(tempDir+"/seqnum", []byte(line), 0o600)
	return err
}

//
// Returns the bodies of messages that match the specified subject filters
//
func readMatchingMessages(config Config) ([]*imap.Message, error) {
	log.Println("Connecting to server...")

	hostAndPort := fmt.Sprintf("%s:%d", config.IMAP.Host, config.IMAP.Port)
	c, err := client.DialTLS(hostAndPort, nil)
	if err != nil {
		return []*imap.Message{}, err
	}
	log.Println("Connected")

	defer func() {
		log.Println("Logging out")
		c.Logout()
	}()

	if err := c.Login(config.IMAP.User, config.IMAP.Password); err != nil {
		return []*imap.Message{}, err
	}
	log.Println("Logged in")

	mbox, err := c.Select(config.IMAP.Folder, false)
	if err != nil {
		return []*imap.Message{}, err
	}

	if mbox.Messages == lastSeenSeqNum(config.TempDir) {
		log.Printf("No new messages (delete %s/seqnum to override)\n", config.TempDir)
		return []*imap.Message{}, nil
	}
	err = writeLastSeenSeqNum(config.TempDir, mbox.Messages)
	if err != nil {
		return []*imap.Message{}, err
	}

	//
	// First retrieve the most recent messages and match their subjects against
	// our filters.  Then fetch the actual message bodies for matching
	// messages.
	//
	// The main idea is to skip downloading non-matching messages.
	//
	from := uint32(1)
	to := mbox.Messages
	if mbox.Messages > config.IMAP.MaxCount {
		// We're using unsigned integers here, only subtract if the result is > 0
		from = mbox.Messages - config.IMAP.MaxCount
	}
	messageEnvelopes, err := fetchMostRecentMessageEnvelopes(c, from, to)
	if err != nil {
		return []*imap.Message{}, err
	}
	log.Printf("Fetched %d message envelopes", len(messageEnvelopes))

	seqNums := []uint32{}
	subjectFilters := []regexp.Regexp{}
	for _, pattern := range config.IMAP.SubjectFilters {
		r := regexp.MustCompile(pattern)
		subjectFilters = append(subjectFilters, *r)
	}

	for _, msg := range messageEnvelopes {
		subject, err := mime.DecodeHeader(msg.Envelope.Subject)
		if err != nil {
			return []*imap.Message{}, err
		}

		match := false
		for _, sf := range subjectFilters {
			if sf.Match([]byte(subject)) {
				match = true
				break
			}
		}

		if match {
			seqNums = append(seqNums, msg.SeqNum)
		}
	}

	log.Printf("Fetching %d matching messages", len(seqNums))
	return fetchMessages(c, seqNums)
}

//
// noSignUp can be embedded to prevent signing up.
//
type noSignUp struct{}

func (c noSignUp) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("not implemented")
}

func (c noSignUp) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	return &auth.SignUpRequired{TermsOfService: tos}
}

//
// termAuth implements authentication via terminal.
//
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

func sendMessagesToChannel(
	api *tg.Client,
	ctx context.Context,
	channel *tg.Channel,
	messages []string,
) error {
	log.Printf("sending %d messages", len(messages))
	rand.Seed(time.Now().Unix())
	for _, m := range messages {
		inputPeer := tg.InputPeerChannel{
			ChannelID:  channel.ID,
			AccessHash: channel.AccessHash,
		}
		request := tg.MessagesSendMessageRequest{
			Peer:     &inputPeer,
			Message:  m,
			RandomID: rand.Int63(),
		}
		updates, err := api.MessagesSendMessage(ctx, &request)
		if err != nil {
			return err
		}

		log.Println(updates.String())
	}
	return nil
}

//
// panics on error
//
func sendMessages(config Config, messages []string) {
	sessionPath := config.TempDir + "/telegram.session"

	examples.Run(func(ctx context.Context, log *zap.Logger) error {
		// Setting up authentication flow helper based on terminal auth.
		flow := auth.NewFlow(
			termAuth{phone: config.Telegram.PhoneNumber},
			auth.SendCodeOptions{},
		)

		options := telegram.Options{
			Logger:         log,
			SessionStorage: &session.FileStorage{Path: sessionPath},
			Middlewares: []telegram.Middleware{
				floodwait.NewSimpleWaiter().WithMaxRetries(10),
			},
		}
		client := telegram.NewClient(config.Telegram.APIID, config.Telegram.APIHash, options)
		return client.Run(ctx, func(ctx context.Context) error {
			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return err
			}

			log.Info("Telegram login success")

			api := client.API()

			searchRequest := tg.ContactsSearchRequest{
				Q:     config.Telegram.ChannelName,
				Limit: 1,
			}
			contactsFound, err := api.ContactsSearch(ctx, &searchRequest)
			if err != nil {
				return err
			}

			for _, ch := range contactsFound.Chats {
				switch ch := ch.(type) {
				case *tg.Channel:
					if ch.Creator {
						return sendMessagesToChannel(api, ctx, ch, messages)
					}
				}
			}

			log.Info(contactsFound.String())
			return errors.New(
				fmt.Sprintf(
					"unable to find channel %q owned by the current user",
					config.Telegram.ChannelName,
				),
			)
		})
	})
}

func main() {

	configPath := flag.String("cfg", "gitignore/config.json", "The location of the config file")
	testTg := flag.String("tt", "", "Test Telegram message sending and exit immediately")
	flag.Parse()

	config := loadConfig(*configPath)

	//
	// main-driven testing and development...
	//
	if *testTg != "" {
		sendMessages(config, []string{*testTg})
		return
	}

	messages, err := readMatchingMessages(config)
	if err != nil {
		log.Fatal(err)
	}

	outbox := []string{}
	for _, msg := range messages {
		body := ""
		for _, val := range msg.Body {
			body += emailBody(val)
		}
		outbox = append(outbox, body)
	}

	for _, text := range outbox {
		log.Println(text)
	}

	sendMessages(config, outbox)
}
