package telegazeta

import (
	"fmt"
	_ "log"
	"os"
	"sort"
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
	items := ItemList{item1, item2, item3, item4, item5}
	uniq := items.dedup()
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
		var items ItemList
		for _, id := range tc.messageID {
			filename := fmt.Sprintf("testdata/%s.bin", id)
			m, err := loadMessage(filename)
			if err != nil {
				t.Fatalf("unable to load %q: %s", filename, err)
			}
			item := newItem(&m)
			items = append(items, item)
		}
		uniq := items.dedup()
		if len(uniq) != tc.want {
			t.Errorf("dedup(%d) failed want: %d got: %d", idx, tc.want, len(uniq))
		}
	}
}

func TestSort(t *testing.T) {
	var ids = []string{"16371", "64905", "8433", "8668"}
	var items ItemList

	for _, id := range ids {
		m, _ := loadMessage(id)
		items = append(items, newItem(&m))
	}

	sort.Sort(items)
	if !sort.IsSorted(items) {
		t.Errorf("expected items to be sorted")
	}

	for i, item := range items {
		if i == 0 {
			continue	
		}
		if item.Date.Unix() < items[i-1].Date.Unix() {
			t.Fatalf("expected items to be sorted")
		}
	}

	itemsByDate := func(i, j int) bool {
		return items[i].Date.Unix() < items[j].Date.Unix()
	}
	sort.Slice(items, itemsByDate)
	if !sort.IsSorted(items) {
		t.Errorf("expected items to be sorted")
	}
}
