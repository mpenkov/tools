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
		// Fri, 26 May 2023 07:57:11 +0000
		// Nb. the date is in GMT, so we need to convert to the local timezone
		//
		lines := strings.Split(bodyString, "\r\n")
		parsedDate, err := time.Parse(time.RFC1123Z, msg.Header.Get("Date"))
		if err != nil {
			log.Fatal(err)
		}
		ldate := parsedDate.Local()
		dow := []string{"日", "月", "火", "水", "木", "金", "土"}
		bodyString = fmt.Sprintf(
			"%s(%s)、%s",
			ldate.Format("1月2日"),
			dow[int(ldate.Weekday())],
			strings.Join(lines[1:4], ""),
		)
	}

	return strings.Trim(bodyString, "\n")
}

//
// fetch the envelopes for the most recent messages (no message body).
//
func fetchMostRecentMessageEnvelopes(
	c *client.Client,
	from uint32,
	to uint32,
) (result []*imap.Message, err error) {
	if !(from <= to) {
		return result, fmt.Errorf("invalid sequence interval from: %d to: %d", from, to)
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	items := []imap.FetchItem{imap.FetchEnvelope}

	messages := make(chan *imap.Message, to-from+1)
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

//
// fetch the specified messages in their entirety
//
func fetchMessages(c *client.Client, seqNums []uint32) (result []*imap.Message, err error) {
	if len(seqNums) == 0 {
		return result, nil
	}

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
// Calculate the sequence number range
//
// max: The maximum sequence number (the most recent message in the inbox)
// seen: The most recent message we've already seen
// count: The max number of messages to include in the range
//
// Returns (0, 0) to indicate an empty range.
//
func seqRange(max, seen, count uint32) (from, to uint32) {
	from = 1
	to = max
	//
	// We're dealing with unsigned variables here, so only subtract if we're
	// sure there's no wraparound
	//
	if max > count {
		from = max - count
	}
	if from <= seen {
		from = seen + 1
	}

	if seen >= to {
		return 0, 0
	}

	return from, to
}

//
// Returns the bodies of messages that match the specified subject filters
//
func readMatchingMessages(config Config) ([]*imap.Message, uint32, error) {
	log.Println("Connecting to server...")

	hostAndPort := fmt.Sprintf("%s:%d", config.IMAP.Host, config.IMAP.Port)
	c, err := client.DialTLS(hostAndPort, nil)
	if err != nil {
		return []*imap.Message{}, uint32(0), err
	}
	log.Println("Connected")

	defer func() {
		log.Println("Logging out")
		c.Logout()
	}()

	if err := c.Login(config.IMAP.User, config.IMAP.Password); err != nil {
		return []*imap.Message{}, uint32(0), err
	}
	log.Println("Logged in")

	mbox, err := c.Select(config.IMAP.Folder, false)
	if err != nil {
		return []*imap.Message{}, uint32(0), err
	}

	lastSeen := lastSeenSeqNum(config.TempDir)

	//
	// First retrieve the most recent messages and match their subjects against
	// our filters.  Then fetch the actual message bodies for matching
	// meskasages.
	//
	// The main idea is to skip downloading non-matching messages.
	//
	// The range of messages to fetch is [from, to] (inclusive).
	//
	from, to := seqRange(mbox.Messages, lastSeen, config.IMAP.MaxCount)
	if from == 0 && to == 0 {
		//
		// No new messages
		//
		return []*imap.Message{}, mbox.Messages, nil
	}

	messageEnvelopes, err := fetchMostRecentMessageEnvelopes(c, from, to)
	if err != nil {
		return []*imap.Message{}, uint32(0), err
	}
	log.Printf("Fetched %d message envelopes", len(messageEnvelopes))

	seqNums := []uint32{}
	subjectFilters := []regexp.Regexp{}
	for _, pattern := range config.IMAP.SubjectFilters {
		r := regexp.MustCompile(pattern)
		subjectFilters = append(subjectFilters, *r)
	}

	log.Println("Message subjects:")
	for _, msg := range messageEnvelopes {
		subject, err := mime.DecodeHeader(msg.Envelope.Subject)
		if err != nil {
			return []*imap.Message{}, uint32(0), err
		}

		log.Printf("[%d] %s\n", msg.SeqNum, subject)

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
	messages, err := fetchMessages(c, seqNums)
	if err != nil {
		return []*imap.Message{}, uint32(0), err
	}
	return messages, mbox.Messages, nil
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
	testIMAP := flag.Bool("ti", false, "Test IMAP retrieval and exit immediately")
	flag.Parse()

	config := loadConfig(*configPath)

	//
	// main-driven testing and development...
	//
	if *testTg != "" {
		sendMessages(config, []string{*testTg})
		return
	}

	messages, latestSeqNum, err := readMatchingMessages(config)
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

	log.Println("Outbox:")
	for i, text := range outbox {
		log.Printf("[%d] %s", messages[i].SeqNum, text)
	}

	if *testIMAP {
		return
	}

	if len(outbox) > 0 {
		sendMessages(config, outbox)
	} else {
		log.Println("No new messages")
	}

	err = writeLastSeenSeqNum(config.TempDir, latestSeqNum)
	if err != nil {
		log.Fatal(err)
	}
}
