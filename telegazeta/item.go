package telegazeta

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"os"
	"sort"
	"time"

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

func (m *Media) embedImageData(path string) {
	m.PendingDownload = nil
	m.Thumbnail = path
	m.ThumbnailBase64 = imageAsBase64(path)
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

func newItem(m *tg.Message) Item {
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

type ItemList []Item

func (l ItemList) Swap(i, j int) {l[i], l[j] = l[j], l[i]}
func (l ItemList) Len() int {return len(l)}
func (l ItemList) Less(i, j int) bool {
	return l[i].Date.Unix() < l[j].Date.Unix()
}

func (il ItemList) dedup() ItemList {
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
	var uniq ItemList
	for _, item := range il {
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

func (il ItemList) group() ItemList {
	var groups ItemList
	sort.Slice(il, func(i, j int) bool {
		return il[i].GroupedID < il[j].GroupedID
	})
	for i, item := range il {
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
