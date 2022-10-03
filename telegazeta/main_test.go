package main

import (
	"errors"
	"fmt"
	_ "log"
	"os"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"

	"go.uber.org/zap"
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

func TestHandleRequest(t *testing.T) {
	logger := zap.NewExample()
	defer logger.Sync()
	worker := Worker{Log: logger}

	//
	// Happy case, everything works
	//
	flag := false
	request := func() error {
		flag = true
		return nil
	}
	err := worker.handleRequest(request, NUM_ATTEMPTS)
	if err != nil {
		t.Errorf("expected error to be nil, got: %s", err)
	}
	if flag != true {
		t.Error("expected flag to be true")
	}

	//
	// Sad case, non-recoverable error
	//
	counter := 0
	flag = false
	request = func() error {
		counter++
		return fmt.Errorf("attempt %d failed", counter)
	}
	err = worker.handleRequest(request, NUM_ATTEMPTS)
	want := "attempt 1 failed"
	if err.Error() != want {
		t.Errorf("want %q got %q", want, err.Error())
	}
	if flag != false {
		t.Errorf("expected flag to be false")
	}

	//
	// Recoverable error
	//
	counter = 0
	flag = false
	request = func() error {
		counter++
		if counter == 2 {
			flag = true
			return nil
		}
		return errors.New("rpcDoRequest: rpc error code 420: FLOOD_WAIT (16)")
	}
	err = worker.handleRequest(request, NUM_ATTEMPTS)
	if err != nil {
		t.Errorf("want nil got %q", err.Error())
	}
	if flag != true {
		t.Errorf("expected flag to be true")
	}
	if counter != 2 {
		t.Errorf("expected counter to be 2, got %d", counter)
	}

	//
	// Recoverable error, but too many retries
	//
	counter = 0
	request = func() error {
		counter++
		return errors.New("rpcDoRequest: rpc error code 420: FLOOD_WAIT (1)")
	}
	err = worker.handleRequest(request, NUM_ATTEMPTS)
	if err == nil {
		t.Errorf("unexpectedly nil")
	}
}
