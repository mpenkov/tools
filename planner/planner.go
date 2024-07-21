//
// Output a template for a simple weekly planner to standard output
//
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"context"
	"net/http"
	"os/signal"
	"sort"

	webdav "github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

//go:embed secret.json
var secretBytes []byte

type Secret struct {
	Username string
	Password string
	Calendars []string
}

var secret Secret

func init() {
	if err := json.Unmarshal(secretBytes, &secret); err != nil {
		log.Fatal(err)
	}
}

type Event struct {
	Start time.Time
	Title string
}

func query(
	ctx context.Context,
	client *caldav.Client,
	calendarPath string,
	start time.Time,
	end time.Time,
) (events []Event, err error) {
	calendarQuery := caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name: "VCALENDAR",
			Comps: []caldav.CalendarCompRequest{{
				Name: "VEVENT",
				Props: []string{
					"SUMMARY",
					"UID",
					"DTSTART",
					"DTEND",
					"DURATION",
				},
			}},
		},
		CompFilter: caldav.CompFilter{
			Name: "VCALENDAR",
			Comps: []caldav.CompFilter{{
				Name:  "VEVENT",
				Start: start,
				End:   end,
			}},
		},
	}

	objects, err := client.QueryCalendar(ctx, calendarPath, &calendarQuery)
	if err != nil {
		return events, err
	}

	for _, obj := range objects {
		// fmt.Printf("%d %s\n", i, obj.Path)
		// printComponent(obj.Data.Component, 0)

		summary := obj.Data.Children[0].Props.Get("SUMMARY")
		dtstart := obj.Data.Children[0].Props.Get("DTSTART")
		tzid := dtstart.Params.Get("TZID")

		location, err := time.LoadLocation(tzid)
		if err != nil {
			log.Fatal(err)
			continue
		}

		startTime, err := time.ParseInLocation("20060102T150405", dtstart.Value, location)
		if err != nil {
			log.Printf("ParseInLocation summary = %q err = %q", summary, err)
			continue
		}

		event := Event{startTime, summary.Value}
		events = append(events, event)
	}

	return events, nil
}

func toMonday(t time.Time) time.Time {
	//
	// Fast-forward to the nearest Monday
	//
	if t.Weekday() == 0 {
		return t.AddDate(0, 0, 1)
	} else if t.Weekday() == 1 {
		return t
	} else {
		var days int = 8 - int(t.Weekday())
		return t.AddDate(0, 0, days)
	}
}

func main() {
	var today time.Time
	if len(os.Args) > 1 {
		var err error
		today, err = time.Parse("2006-01-02", os.Args[1])
		if err != nil {
			log.Fatalf("unable to parse %q: %s", os.Args[1], err)
		}
	} else {
		today = time.Now()
	}

	httpClient := &http.Client{}
	authorizedClient := webdav.HTTPClientWithBasicAuth(
		httpClient,
		secret.Username,
		secret.Password,
	)
	caldavClient, err := caldav.NewClient(
		authorizedClient,
		"https://caldav.fastmail.com/dav/calendars/user/"+secret.Username,
	)
	if err != nil {
		log.Fatalf("NewClient: %s", err)
	}

	gctx := context.Background()
	gctx, _ = signal.NotifyContext(gctx, os.Interrupt)

	start := today.AddDate(0, -1, 0)
	end := today.AddDate(0, 7, 0)

	var events []Event
	for _, path := range secret.Calendars {
		evts, err := query(gctx, caldavClient, path, start, end)
		if err != nil {
			log.Fatal(err)
		}
		events = append(events, evts...)
	}

	eventMap := map[string][]Event{}
	for _, event := range events {
		year, month, day := event.Start.Date()
		datestamp := fmt.Sprintf("%d-%02d-%02d", year, month, day)
		eventMap[datestamp] = append(eventMap[datestamp], event)
	}

	today = toMonday(today)

	year, week := today.ISOWeek()
	fmt.Printf("# %04d Week %d\n\n", year, week)
	for i := 0; i < 7; i++ {
		t := today.AddDate(0, 0, i)
		fmt.Printf("## %s\n\n", t.Format("2006-01-02 Mon"))

		evts := eventMap[t.Format("2006-01-02")]
		sort.Slice(
			evts,
			func(i, j int) bool { return evts[i].Start.Sub(evts[j].Start).Seconds() < 0 },
		)
		for _, event := range evts {
			fmt.Printf("%s %s\n", event.Start.Format("15:04"), event.Title)
		}

		if len(evts) > 0 {
			fmt.Println()
		}
	}
}
