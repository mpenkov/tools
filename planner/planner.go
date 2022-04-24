//
// Output a template for a simple weekly planner to standard output
//
package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

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

	today = toMonday(today)

	year, week := today.ISOWeek()
	fmt.Printf("# %04d Week %d\n\n", year, week)
	for i := 0; i < 7; i++ {
		t := today.AddDate(0, 0, i)
		fmt.Printf("## %s\n\n", t.Format("2006-01-02 Mon"))
	}
}
