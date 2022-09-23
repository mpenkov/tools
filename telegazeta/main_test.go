package main

import (
	"os"
	"log"
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
	//
	// NB. Message text is slightly different
	//
	m1, _ := loadMessage("testdata/8668.bin")
	m2, _ := loadMessage("testdata/64905.bin")
	log.Printf("%q", m1.Message)
	log.Printf("%q", m2.Message)
	items := []Item{Item{Text: m1.Message}, Item{Text: m2.Message}}
	uniq := dedup(items)
	if len(uniq) != 1 {
		t.Errorf("deduplication failed want: 1 got: %d", len(uniq))
	}
}
