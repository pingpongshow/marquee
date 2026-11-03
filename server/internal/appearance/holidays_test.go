package appearance

import (
	"testing"
	"time"
)

func TestEaster(t *testing.T) {
	for y, want := range map[int]string{
		2024: "2024-03-31", 2025: "2025-04-20", 2026: "2026-04-05", 2027: "2027-03-28", 2028: "2028-04-16",
		2029: "2029-04-01", 2030: "2030-04-21", 2035: "2035-03-25", 2038: "2038-04-25", 2040: "2040-04-01",
	} {
		if got := Easter(y).Format(time.DateOnly); got != want {
			t.Errorf("Easter %d = %s, want %s", y, got, want)
		}
	}
}

func TestMardiGras(t *testing.T) {
	for y, want := range map[int]string{2025: "2025-03-04", 2026: "2026-02-17", 2027: "2027-02-09", 2028: "2028-02-29", 2029: "2029-02-13"} {
		got := MardiGras(y)
		if got.Format(time.DateOnly) != want || got.Weekday() != time.Tuesday {
			t.Errorf("Mardi Gras %d = %s (%s), want %s", y, got.Format(time.DateOnly), got.Weekday(), want)
		}
	}
}

func TestThanksgiving(t *testing.T) {
	for y, want := range map[int]string{2024: "2024-11-28", 2025: "2025-11-27", 2026: "2026-11-26", 2027: "2027-11-25", 2030: "2030-11-28", 2040: "2040-11-22"} {
		got := Thanksgiving(y)
		if got.Format(time.DateOnly) != want || got.Weekday() != time.Thursday {
			t.Errorf("Thanksgiving %d = %s, want %s", y, got.Format(time.DateOnly), want)
		}
	}
}

// Every year of the tables is there, and each date is a plausible one for its holiday.
func TestTablesThrough2040(t *testing.T) {
	for y := 2026; y <= 2040; y++ {
		if d, ok := chineseNewYear[y]; !ok || d[0] > 2 || (d[0] == 1 && d[1] < 21) || (d[0] == 2 && d[1] > 21) {
			t.Errorf("Chinese New Year %d: %v", y, d)
		}
		if d, ok := diwali[y]; !ok || d[0] < 10 || d[0] > 11 || (d[0] == 10 && d[1] < 13) || (d[0] == 11 && d[1] > 16) {
			t.Errorf("Diwali %d: %v", y, d)
		}
		if d, ok := hanukkah[y]; !ok || d[0] < 11 || (d[0] == 11 && d[1] < 26) {
			t.Errorf("Hanukkah %d: %v", y, d)
		}
	}
}

func TestHolidaysAreThemes(t *testing.T) {
	for _, h := range holidays {
		if !KnownTheme(h.id) {
			t.Errorf("%s has no theme", h.id)
		}
	}
}

func TestHolidayAt(t *testing.T) {
	for _, c := range []struct{ date, want string }{
		{"2027-06-15", ""},
		// Fixed windows and their edges.
		{"2027-10-23", ""},
		{"2027-10-24", "halloween"},
		{"2027-10-31", "halloween"},
		{"2027-11-01", "dia-de-los-muertos"},
		{"2027-11-02", "dia-de-los-muertos"},
		{"2027-11-03", ""},
		{"2027-02-07", "chinese-new-year"}, // the day after New Year's Day, within its own days
		{"2028-07-01", "independence-day"},
		{"2028-07-05", "independence-day"},
		{"2028-07-06", ""},
		{"2028-03-14", "st-patricks"},
		{"2028-11-30", ""},
		{"2028-12-01", "christmas"},
		// New Year crosses the year's end, from either side.
		{"2029-12-29", ""},
		{"2029-12-30", "new-year"},
		{"2030-01-02", "new-year"},
		{"2030-01-03", ""},
		// Moving dates.
		{"2028-04-09", "easter"},
		{"2028-04-17", "easter"},
		{"2028-04-18", ""},
		{"2028-11-17", "thanksgiving"},
		{"2028-11-24", "thanksgiving"},
		{"2028-11-25", ""},
		{"2030-02-26", ""},
		{"2028-02-23", "mardi-gras"},
		{"2028-02-29", "mardi-gras"},
		// Tables.
		{"2028-01-22", ""},
		{"2028-01-23", "chinese-new-year"},
		{"2028-02-02", "chinese-new-year"},
		{"2028-02-03", ""},
		{"2031-11-12", "diwali"},
		{"2031-11-16", "diwali"},
		{"2031-11-17", ""},
	} {
		at, _ := time.Parse(time.DateOnly, c.date)
		if got := HolidayAt(at); got != c.want {
			t.Errorf("%s: %q, want %q", c.date, got, c.want)
		}
	}
}

func TestHolidayOverlaps(t *testing.T) {
	for _, c := range []struct{ date, want, why string }{
		{"2027-12-23", "christmas", "before Hanukkah begins"},
		{"2027-12-24", "christmas", "Christmas Eve beats the first candle: the shorter of the two"},
		{"2027-12-25", "christmas", "Christmas Day during Hanukkah"},
		{"2027-12-26", "hanukkah", "Hanukkah's own days beat Christmas's last day"},
		{"2027-12-30", "hanukkah", "Hanukkah's own days beat New Year's lead-up"},
		{"2027-12-31", "new-year", "New Year's Eve beats Hanukkah, being shorter"},
		{"2028-01-01", "new-year", "New Year's Day beats Hanukkah's last day"},
		{"2028-01-02", "new-year", "back to New Year's window after Hanukkah"},
		{"2029-12-05", "hanukkah", "an early Hanukkah beats the start of Christmas's window"},
		{"2029-12-10", "christmas", "and Christmas follows it"},
		{"2029-02-13", "mardi-gras", "Mardi Gras and Chinese New Year on one day: the shorter"},
		{"2029-02-14", "valentines", "the day itself, shortest of those on it"},
		{"2029-02-15", "chinese-new-year", "New Year's own days again"},
		{"2029-02-12", "valentines", "both lead-ups, the same length: the earlier in the list"},
		{"2027-02-13", "valentines", "both lead-ups: Valentine's is shorter than Chinese New Year's window"},
		{"2035-10-31", "halloween", "Halloween and Diwali's window: Halloween itself"},
		{"2035-10-30", "diwali", "Diwali's own day beats Halloween's lead-up"},
	} {
		at, _ := time.Parse(time.DateOnly, c.date)
		if got := HolidayAt(at); got != c.want {
			t.Errorf("%s: %q, want %q (%s)", c.date, got, c.want, c.why)
		}
	}
}

// Past the end of a table there's no theme rather than a guess, but the holidays that can
// be calculated carry on.
func TestPastTablesEnd(t *testing.T) {
	for _, date := range []string{"2041-02-01", "2041-02-02", "2041-10-23", "2041-12-18"} {
		at, _ := time.Parse(time.DateOnly, date)
		switch got := HolidayAt(at); got {
		case "chinese-new-year", "diwali", "hanukkah":
			t.Errorf("%s: %q past the table's end", date, got)
		}
	}
	at, _ := time.Parse(time.DateOnly, "2041-04-21")
	if got := HolidayAt(at); got != "easter" {
		t.Errorf("Easter 2041: %q", got)
	}
}

// The date is the server's: what matters is the calendar day in now's time zone.
func TestHolidayUsesNowsTimeZone(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*3600)
	losAngeles := time.FixedZone("PST", -8*3600)
	instant := time.Date(2027, time.December, 1, 2, 0, 0, 0, time.UTC)
	if got := HolidayAt(instant.In(tokyo)); got != "christmas" {
		t.Errorf("Tokyo, 1 December: %q", got)
	}
	if got := HolidayAt(instant.In(losAngeles)); got != "" {
		t.Errorf("Los Angeles, 30 November: %q", got)
	}
}

func TestActive(t *testing.T) {
	halloween := time.Date(2028, time.October, 30, 12, 0, 0, 0, time.UTC)
	june := time.Date(2027, time.June, 15, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		chosen       string
		auto         bool
		now          time.Time
		theme, want2 string
	}{
		{"ocean", true, halloween, "halloween", "halloween"},
		{"ocean", false, halloween, "ocean", ""},
		{"ocean", true, june, "ocean", ""},
		{"", false, june, "marquee", ""},
		{"gone-since", false, june, "marquee", ""},
	} {
		theme, h := Active(c.chosen, c.auto, c.now)
		if theme != c.theme || h != c.want2 {
			t.Errorf("Active(%q, %v, %s) = %q, %q", c.chosen, c.auto, c.now.Format(time.DateOnly), theme, h)
		}
	}
}
