package cardigann

import (
	"errors"
	"strings"
	"time"
)

// goDateTokens are the reference-time elements the converter emits, longest
// first so a scan never reads "2006" as "2" or "-07:00" as "-07". Literals
// between them never contain a digit or a month, day or zone name — the
// converter refuses such layouts — so this scan is a faithful reading of
// every layout in a converted definition.
var goDateTokens = []string{
	"January", "Monday", "Z07:00", "-07:00", "Z0700", "-0700",
	"2006", "Jan", "Mon", "-07",
	"01", "02", "03", "04", "05", "06", "15", "PM", "pm",
	"1", "2", "3", "4", "5",
}

// dateParts reports which calendar fields a Go layout sets.
func dateParts(layout string) (year, month, day bool) {
	for i := 0; i < len(layout); {
		matched := ""
		for _, tok := range goDateTokens {
			if strings.HasPrefix(layout[i:], tok) {
				matched = tok
				break
			}
		}
		if matched == "" {
			i++
			continue
		}
		switch matched {
		case "2006", "06":
			year = true
		case "January", "Jan", "01", "1":
			month = true
		case "02", "2":
			day = true
		default:
		}
		i += len(matched)
	}
	return year, month, day
}

// ParseDate parses value with the first of a dateparse filter's layouts that
// accepts it, the way .NET's ParseExact does on upstream's engines: a layout
// without a zone reads the time in now's location, and calendar fields the
// layout does not carry come from now — no year takes now's year, no date at
// all takes now's date, a missing month or day alone is the first. Go's own
// time.Parse leaves them at year 0, January 1st, which is never what a
// tracker printing "10/05 12:00" or "12:34" meant.
func ParseDate(layouts []string, value string, now time.Time) (time.Time, error) {
	if len(layouts) == 0 {
		return time.Time{}, errors.New("dateparse: no layout")
	}
	var err error
	for _, layout := range layouts {
		var t time.Time
		if t, err = time.ParseInLocation(layout, value, now.Location()); err != nil {
			continue
		}
		hasYear, hasMonth, hasDay := dateParts(layout)
		y, m, d := t.Date()
		switch {
		case !hasYear && !hasMonth && !hasDay:
			y, m, d = now.Date()
		case !hasYear:
			y = now.Year()
		default:
		}
		return time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(),
			t.Location()), nil
	}
	return time.Time{}, err
}
