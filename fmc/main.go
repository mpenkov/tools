//
// Demonstrates connecting to fastmail via caldev
//
// Influential env variables:
//
// - USERNAME: e.g. me@fastmail.com
// - PASSWORD: an app-specific password with access to the calendar
// - CALENDAR_PATH: /dav/calendars/user/me@fastmail.com/DEADBEEF
//
// you can get the link to the calendar from settings/calendars/export.
//
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"time"

	ical "github.com/emersion/go-ical"
	webdav "github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

type Event struct {
	Start time.Time
	Title string
}

func discover(ctx context.Context, client *caldav.Client, path string) ([]caldav.Calendar, error) {
	// curl -X PROPFIND -u USERNAME:PASSWORD https://caldav.fastmail.com/dav/principals/user/USERNAME
	homeSet, err := client.FindCalendarHomeSet(ctx, path)
	if err != nil {
		return []caldav.Calendar{}, err
	}

	fmt.Println(homeSet)

	calendars, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return []caldav.Calendar{}, err
	}

	return calendars, nil
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
			Name:  "VCALENDAR",
			Props: []string{"VERSION"},
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

		// fmt.Printf("\t\t%s = %s\n", summary.Name, summary.Value)
		// fmt.Printf("\t\t%s = %s\n", dtstart.Name, dtstart.Value)
		// fmt.Printf("\t\t%s = %s\n", "TZID", tzid)

		location, err := time.LoadLocation(tzid)
		if err != nil {
			// log.Fatal(err)
			continue
		}

		startTime, err := time.ParseInLocation("20060102T150405", dtstart.Value, location)
		if err != nil {
			// log.Fatal(err)
			continue
		}

		event := Event{startTime, summary.Value}
		events = append(events, event)
	}

	return events, nil
}

func main() {
	httpClient := &http.Client{}
	username := os.ExpandEnv("$USERNAME")
	password := os.ExpandEnv("$PASSWORD")
	calendarPath := os.ExpandEnv("$CALENDAR_PATH")

	authorizedClient := webdav.HTTPClientWithBasicAuth(httpClient, username, password)

	caldavClient, err := caldav.NewClient(authorizedClient, "https://caldav.fastmail.com/dav/calendars/user/"+username)
	if err != nil {
		log.Fatalf("NewClient: %s", err)
	}

	gctx := context.Background()
	gctx, _ = signal.NotifyContext(gctx, os.Interrupt)

	if calendarPath == "" {
		path := "/dav/principals/user/" + username
		calendars, err := discover(gctx, caldavClient, path)
		if err != nil {
			log.Fatal(err)
		}

		for i, calendar := range calendars {
			fmt.Printf("cal %d: %s %s\n", i, calendar.Name, calendar.Path)
		}

		calendarPath = calendars[0].Path
	}

	log.Printf("calendar.Path: %q\n", calendarPath)

	today := time.Now()
	start := today.AddDate(0, -1, 0)
	end := today.AddDate(0, 7, 0)

	events, err := query(gctx, caldavClient, calendarPath, start, end)
	if err != nil {
		log.Fatal(err)
	}

	sort.Slice(
		events,
		func(i, j int) bool { return events[i].Start.Sub(events[j].Start).Seconds() > 0 },
	)

	for _, evt := range events {
		fmt.Printf("%s\t%s\n", evt.Start.Format(time.DateTime), evt.Title)
	}
}

func printComponent(cal *ical.Component, level int) {
	for i := 0; i < level; i++ {
		fmt.Printf("\t")
	}
	fmt.Printf("%q %q ", cal.Name, cal.Props)
	fmt.Println()
	for _, child := range cal.Children {
		printComponent(child, level+1)
	}
	fmt.Println()
}
