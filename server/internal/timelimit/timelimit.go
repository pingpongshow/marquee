// Package timelimit holds the time limits of kids' profiles: a daily allowance of video
// (a different one at weekends, if set) and viewing hours. It works out whether video may
// play now, and counts the minutes of video each person watches per day (the server's
// local day).
package timelimit

import (
	"errors"
	"fmt"
	"time"
)

// Limits are one person's time limits. Daily nil or 0 = no daily limit; Weekend nil = the
// same as Daily, 0 = no limit on Saturdays and Sundays. From and Until ("HH:MM") are the
// viewing hours; both empty = any time. A window may cross midnight (20:00–07:00).
type Limits struct {
	Daily, Weekend *int
	From, Until    string
}

// Active reports whether any limit is set.
func (l Limits) Active() bool {
	return (l.Daily != nil && *l.Daily > 0) || (l.Weekend != nil && *l.Weekend > 0) || l.hasWindow()
}

func (l Limits) hasWindow() bool { return l.From != "" && l.Until != "" && l.From != l.Until }

// MinutesOn is the allowance for the day containing t (0 = no limit that day).
func (l Limits) MinutesOn(t time.Time) int {
	if wd := t.Weekday(); (wd == time.Saturday || wd == time.Sunday) && l.Weekend != nil {
		return *l.Weekend
	}
	if l.Daily != nil {
		return *l.Daily
	}
	return 0
}

// ParseClock reads "HH:MM" as minutes after midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, fmt.Errorf("%q isn't a time (HH:MM)", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Validate checks limits an administrator entered.
func (l Limits) Validate() error {
	for _, v := range []*int{l.Daily, l.Weekend} {
		if v != nil && (*v < 0 || *v > 1440) {
			return errors.New("a time limit must be 0 to 1440 minutes")
		}
	}
	if (l.From == "") != (l.Until == "") {
		return errors.New("viewing hours need both a start and an end")
	}
	if l.From != "" {
		if _, err := ParseClock(l.From); err != nil {
			return err
		}
		if _, err := ParseClock(l.Until); err != nil {
			return err
		}
		if l.From == l.Until {
			return errors.New("viewing hours must start and end at different times")
		}
	}
	return nil
}

// Status is whether video may play now, for GET /me/time-limit and enforcement.
type Status struct {
	Limited    bool
	AllowedNow bool
	// LimitMinutes, UsedMinutes and MinutesLeft are set when today has a daily limit.
	LimitMinutes, UsedMinutes, MinutesLeft *int
	From, Until                            string
	// Reason says why video can't play now; ResetsAt is when it may again.
	Reason   string
	ResetsAt *time.Time
}

func minuteOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func nextMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}

func atMinute(day time.Time, min int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, min/60, min%60, 0, 0, day.Location())
}

// inWindow reports whether t falls inside the viewing hours (always true without them).
func (l Limits) inWindow(t time.Time) bool {
	if !l.hasWindow() {
		return true
	}
	from, _ := ParseClock(l.From)
	until, _ := ParseClock(l.Until)
	m := minuteOfDay(t)
	if from < until {
		return m >= from && m < until
	}
	return m >= from || m < until // crosses midnight
}

// windowStartFrom is the first moment at or after t inside the viewing hours.
func (l Limits) windowStartFrom(t time.Time) time.Time {
	if l.inWindow(t) {
		return t
	}
	from, _ := ParseClock(l.From)
	start := atMinute(t, from)
	if !start.After(t) {
		start = atMinute(nextMidnight(t), from)
	}
	return start
}

// windowEnd is when the viewing hours containing t end (zero without viewing hours).
func (l Limits) windowEnd(t time.Time) time.Time {
	if !l.hasWindow() {
		return time.Time{}
	}
	until, _ := ParseClock(l.Until)
	end := atMinute(t, until)
	if !end.After(t) {
		end = atMinute(nextMidnight(t), until)
	}
	return end
}

// Evaluate works out the status at now, given the milliseconds of video watched today.
func Evaluate(l Limits, usedMS int64, now time.Time) Status {
	st := Status{Limited: l.Active(), AllowedNow: true}
	if !st.Limited {
		return st
	}
	if l.hasWindow() {
		st.From, st.Until = l.From, l.Until
	}
	limit := l.MinutesOn(now)
	usedUp := false
	if limit > 0 {
		used := int(usedMS / 60000)
		leftMS := int64(limit)*60000 - usedMS
		left := 0
		if leftMS > 0 {
			left = int((leftMS + 59999) / 60000) // a part-minute left still shows as a minute
		}
		usedUp = leftMS <= 0
		// Video ends with the viewing hours too.
		if end := l.windowEnd(now); !end.IsZero() && l.inWindow(now) {
			if m := int(end.Sub(now).Round(time.Minute) / time.Minute); m < left {
				left = m
			}
		}
		st.LimitMinutes, st.UsedMinutes, st.MinutesLeft = &limit, &used, &left
	}
	inWindow := l.inWindow(now)
	if !usedUp && inWindow {
		return st
	}
	st.AllowedNow = false
	back := now
	if usedUp {
		back = nextMidnight(now)
	}
	back = l.windowStartFrom(back)
	st.ResetsAt = &back
	if usedUp {
		st.Reason = "Time's up for today — back " + when(back)
	} else {
		st.Reason = fmt.Sprintf("Viewing hours are %s–%s", l.From, l.Until)
	}
	return st
}

// when says when video may play again: "at 07:00", or "at midnight".
func when(back time.Time) string {
	if back.Equal(midnight(back)) {
		return "at midnight"
	}
	return "at " + back.Format("15:04")
}
