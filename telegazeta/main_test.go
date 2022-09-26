package main

import (
	"fmt"
	_ "log"
	"os"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func loadMessage(path string) (message tg.Message, err error) {
	f, err := os.Open(path)
	if err != nil {
		return message, err
	}
	defer f.Close()

	var buffer bin.Buffer = bin.Buffer{Buf: make([]byte, 1024768)}
	if _, err := f.Read(buffer.Buf); err != nil {
		return message, err
	}

	if err = message.Decode(&buffer); err != nil {
		return message, err
	}

	return message, nil
}

func TestDedup(t *testing.T) {
	item1 := Item{Text: "foo"}
	item2 := Item{Text: "bar"}
	item3 := Item{Text: "foo"}
	item4 := Item{Text: ""}
	item5 := Item{Text: ""}
	items := []Item{item1, item2, item3, item4, item5}
	uniq := dedup(items)
	if len(uniq) != 4 {
		t.Errorf("deduplication failed want: 4 got: %d", len(uniq))
	}
}

func TestRealDedup(t *testing.T) {
	var testCases = []struct {
		messageID []string
		want      int
	}{
		{[]string{"8668", "64905"}, 1},
		{[]string{"8433", "16371"}, 1},
	}
	for idx, tc := range testCases {
		var items []Item
		for _, id := range tc.messageID {
			filename := fmt.Sprintf("testdata/%s.bin", id)
			m, err := loadMessage(filename)
			if err != nil {
				t.Fatalf("unable to load %q: %s", filename, err)
			}
			item := mkitem(&m)
			items = append(items, item)
		}
		uniq := dedup(items)
		if len(uniq) != tc.want {
			t.Errorf("dedup(%d) failed want: %d got: %d", idx, tc.want, len(uniq))
		}
	}
}