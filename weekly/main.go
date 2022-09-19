// Weekly review template

package main

import (
	"fmt"
	"log"
	"os"
	"text/template"
	"time"
)

type Params struct {
	Year int
	WeekNumber int
	YearProgress string
	NextYear int
	MondayDate string
}

func main() {
	const templateStr = `
# {{.Year}} Week {{.WeekNumber}}

    {{.Year}} {{.YearProgress}} {{.NextYear}}

[RescueTime report](https://www.rescuetime.com/dashboard/for/the/week/of/{{.MondayDate}})

## Time and Task Audit

> Did your actions last week match up to your priorities?
> Did you complete what you set out to do?
> Does your calendar (and commitments) match your priorities and values?
> What was your allocation of $10/hour work (i.e. answering emails) vs. $10,000/hour work (i.e. building a new feature)

## Journalling

> What went well?
> Where did you get stuck?
> What did you learn?

## Planning

- [ ] Clutter: Clear your desk
- [ ] Email: Clear your inbox
- [ ] Calendar: Review schedule for next week, allocate time for important work.
- [ ] Desktop: Get rid of digital clutter by clearing out your desktop and downloads folder
- [ ] Notes: Go through all your notes and either file them away or turn them into action items
- [ ] Tasks: Clear your to-do list, set your priorities for next week, move tasks and follow-ups into next week’s list
`
	template := template.Must(template.New("weekly").Parse(templateStr))

	now := time.Now()
	var weekday int = int(now.Weekday())

	var startOfWeek time.Time
	var offset time.Duration
	if weekday == 1 {
		d, _ := time.ParseDuration("168h")
		offset = -d
	} else {
		log.Fatalf("todo: implement me!")
	}
	startOfWeek = now.Add(offset)

	year, month, date := startOfWeek.Date()
	endOfYear, _ := time.Parse("2006-01-02", fmt.Sprintf("%d-12-31", year))
	_, weeksInYear := endOfYear.ISOWeek()

	var params Params
	params.Year, params.WeekNumber = startOfWeek.ISOWeek()
	params.NextYear = year + 1
	params.MondayDate = fmt.Sprintf("%d-%02d-%02d", year, month, date)
	params.YearProgress = "["
	for i := 0; i < weeksInYear; i++ {
		if i < params.WeekNumber {
			params.YearProgress += "#"
		} else {
			params.YearProgress += "."
		}
	}
	params.YearProgress += "]"
	template.Execute(os.Stdout, params)
}
