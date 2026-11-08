package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dateLayout is what the date filters write and the date field reads first:
// Prowlarr's Rfc1123ZPattern, with a whole offset where .NET writes "+2".
const dateLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

var errNoDate = errors.New("not a date")

var (
	timeAgoRe   = regexp.MustCompile(`(?i)\bago`)
	todayRe     = regexp.MustCompile(`(?i)\btoday(?:[\s,]+(?:at)?\s*|[\s,]*|$)`)
	tomorrowRe  = regexp.MustCompile(`(?i)\btomorrow(?:[\s,]+(?:at)?\s*|[\s,]*|$)`)
	yesterdayRe = regexp.MustCompile(`(?i)\byesterday(?:[\s,]+(?:at)?\s*|[\s,]*|$)`)
	weekdayRe   = regexp.MustCompile(
		`(?i)\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+`,
	)
	missingYearRe  = regexp.MustCompile(`^(\d{1,2}-\d{1,2})(\s|$)`)
	missingYearRe2 = regexp.MustCompile(`^(\d{1,2}\s+\w{3})\s+(\d{1,2}:\d{1,2}.*)$`)
	timeAgoPartRe  = regexp.MustCompile(`\s*?([\d.]+)\s*?([^\d\s.]+)\s*?`)
	allDigitsRe    = regexp.MustCompile(`^\d+$`)
)

// fromTimeAgo is DateTimeUtil.FromTimeAgo: "2 hours 1 day ago" counted back
// from now, months as 30 days and years as 365.
func fromTimeAgo(s string, now time.Time) (time.Time, error) {
	s = strings.ToLower(s)
	if strings.Contains(s, "now") {
		return now, nil
	}
	s = strings.NewReplacer(",", "", "ago", "", "and", "").Replace(s)
	var ago time.Duration
	for _, m := range timeAgoPartRe.FindAllStringSubmatch(s, -1) {
		v, ok := coerceFloat(m[1])
		if !ok {
			return time.Time{}, fmt.Errorf("%w: %q", errNoDate, s)
		}
		unit := m[2]
		var per time.Duration
		switch {
		case strings.Contains(unit, "sec") || unit == "s":
			per = time.Second
		case strings.Contains(unit, "min") || unit == "m":
			per = time.Minute
		case strings.Contains(unit, "hour") || strings.Contains(unit, "hr") ||
			unit == "h":
			per = time.Hour
		case strings.Contains(unit, "day") || unit == "d":
			per = 24 * time.Hour
		case strings.Contains(unit, "week") || strings.Contains(unit, "wk") ||
			unit == "w":
			per = 7 * 24 * time.Hour
		case strings.Contains(unit, "month") || unit == "mo":
			per = 30 * 24 * time.Hour
		case strings.Contains(unit, "year") || unit == "y":
			per = 365 * 24 * time.Hour
		default:
			return time.Time{}, fmt.Errorf("%w: unknown unit %q", errNoDate, unit)
		}
		ago += time.Duration(v * float64(per))
	}
	return now.Add(-ago), nil
}

// fromUnknown is DateTimeUtil.FromUnknown: what the date field and the
// fuzzytime filter read a free-form date with.
func fromUnknown(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t, nil
	}
	if allDigitsRe.MatchString(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).In(now.Location()), nil
		}
	}
	if strings.Contains(strings.ToLower(s), "now") {
		return now, nil
	}
	if timeAgoRe.MatchString(s) {
		return fromTimeAgo(s, now)
	}
	today := time.Date(
		now.Year(),
		now.Month(),
		now.Day(),
		0,
		0,
		0,
		0,
		now.Location(),
	)
	for _, rel := range []struct {
		re   *regexp.Regexp
		days int
	}{{todayRe, 0}, {yesterdayRe, -1}, {tomorrowRe, 1}} {
		if m := rel.re.FindString(s); m != "" || rel.re.MatchString(s) {
			tod, err := timeOfDay(strings.Replace(s, m, "", 1))
			if err != nil {
				return time.Time{}, err
			}
			return today.AddDate(0, 0, rel.days).Add(tod), nil
		}
	}
	if m := weekdayRe.FindStringSubmatch(s); m != nil {
		tod, err := timeOfDay(strings.Replace(s, m[0], "", 1))
		if err != nil {
			return time.Time{}, err
		}
		d := today
		for !strings.EqualFold(d.Weekday().String(), m[1]) {
			d = d.AddDate(0, 0, -1)
		}
		return d.Add(tod), nil
	}
	if m := missingYearRe.FindStringSubmatch(s); m != nil {
		s = strings.Replace(s, m[1], strconv.Itoa(now.Year())+"-"+m[1], 1)
	}
	if m := missingYearRe2.FindStringSubmatch(s); m != nil {
		s = m[1] + " " + strconv.Itoa(now.Year()) + " " + m[2]
	}
	return fuzzyTime(s, now)
}

// timeOfDay reads what follows "today at": empty is midnight.
func timeOfDay(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	for _, l := range []string{"15:04:05", "15:04", "3:04:05 PM", "3:04 PM", "3:04PM", "3PM"} {
		if t, err := time.Parse(l, strings.ToUpper(s)); err == nil {
			return time.Duration(t.Hour())*time.Hour +
				time.Duration(t.Minute())*time.Minute +
				time.Duration(t.Second())*time.Second, nil
		}
	}
	return 0, fmt.Errorf("%w: time %q", errNoDate, s)
}

// fuzzyLayouts stand in for DateTimeRoutines, the free-form parser Prowlarr
// falls back on: the shapes trackers print, month-first where the two are
// ambiguous, as its USDate default reads them.
var fuzzyLayouts = []string{
	time.RFC3339Nano,
	time.RFC1123Z,
	time.RFC1123,
	time.RFC850,
	time.ANSIC,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
	"2006.01.02 15:04:05",
	"2006.01.02",
	"01/02/2006 15:04:05",
	"01/02/2006 15:04",
	"01/02/2006 3:04:05 PM",
	"01/02/2006 3:04 PM",
	"01/02/2006",
	"1/2/2006 15:04",
	"1/2/2006",
	"01-02-2006 15:04",
	"01-02-2006",
	"02.01.2006 15:04:05",
	"02.01.2006 15:04",
	"02.01.2006",
	"2 Jan 2006 15:04:05",
	"2 Jan 2006 15:04",
	"2 Jan 2006",
	"02 Jan 2006",
	"2 January 2006 15:04",
	"2 January 2006",
	"Jan 2 2006 15:04",
	"Jan 2 2006",
	"Jan 2, 2006 15:04",
	"Jan 2, 2006 3:04 PM",
	"Jan 2, 2006",
	"January 2 2006",
	"January 2, 2006 15:04",
	"January 2, 2006",
	"Mon Jan 2 2006",
	"Mon, Jan 2, 2006",
	"2-Jan-2006",
	"02-Jan-2006 15:04",
	"02-Jan-06",
}

func fuzzyTime(s string, now time.Time) (time.Time, error) {
	s = strings.Join(strings.Fields(s), " ")
	for _, l := range fuzzyLayouts {
		if t, err := time.ParseInLocation(l, s, now.Location()); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q", errNoDate, s)
}
