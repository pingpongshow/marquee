package appearance

import "time"

// Dates that follow another calendar, which need a table. Past a table's end the holiday has
// no window at all, rather than a guess.

// chineseNewYear is the first day of the first lunar month, from the Hong Kong Observatory's
// Gregorian-Lunar Calendar Conversion Tables.
var chineseNewYear = map[int][2]int{
	2026: {2, 17}, 2027: {2, 6}, 2028: {1, 26}, 2029: {2, 13}, 2030: {2, 3},
	2031: {1, 23}, 2032: {2, 11}, 2033: {1, 31}, 2034: {2, 19}, 2035: {2, 8},
	2036: {1, 28}, 2037: {2, 15}, 2038: {2, 4}, 2039: {1, 24}, 2040: {2, 12},
}

// diwali is the night of Lakshmi Puja: the evening of the new moon after the Sun enters
// sidereal Libra, as almanacs give it for New Delhi. Local calendars can differ by a day.
var diwali = map[int][2]int{
	2026: {11, 8}, 2027: {10, 29}, 2028: {10, 17}, 2029: {11, 5}, 2030: {10, 26},
	2031: {11, 14}, 2032: {11, 2}, 2033: {10, 22}, 2034: {11, 10}, 2035: {10, 30},
	2036: {10, 18}, 2037: {11, 7}, 2038: {10, 27}, 2039: {11, 15}, 2040: {11, 4},
}

// hanukkah is the evening of the first candle (the day before 25 Kislev, from Hebcal).
var hanukkah = map[int][2]int{
	2026: {12, 4}, 2027: {12, 24}, 2028: {12, 12}, 2029: {12, 1}, 2030: {12, 20},
	2031: {12, 9}, 2032: {11, 27}, 2033: {12, 16}, 2034: {12, 6}, 2035: {12, 25},
	2036: {12, 13}, 2037: {12, 2}, 2038: {12, 21}, 2039: {12, 11}, 2040: {11, 29},
}

// day is a calendar date with no time or zone, so windows compare the same in any zone.
func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Easter is Western Easter Sunday in year y (the anonymous Gregorian computus).
func Easter(y int) time.Time {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	date := (h+l-7*m+114)%31 + 1
	return day(y, time.Month(month), date)
}

// MardiGras is the Tuesday 47 days before Easter.
func MardiGras(y int) time.Time { return Easter(y).AddDate(0, 0, -47) }

// Thanksgiving is the fourth Thursday of November.
func Thanksgiving(y int) time.Time {
	first := day(y, time.November, 1)
	toThursday := (int(time.Thursday) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, toThursday+21)
}

func fixed(m time.Month, d int) func(int) (time.Time, bool) {
	return func(y int) (time.Time, bool) { return day(y, m, d), true }
}

func moving(f func(int) time.Time) func(int) (time.Time, bool) {
	return func(y int) (time.Time, bool) { return f(y), true }
}

func table(t map[int][2]int) func(int) (time.Time, bool) {
	return func(y int) (time.Time, bool) {
		d, ok := t[y]
		if !ok {
			return time.Time{}, false
		}
		return day(y, time.Month(d[0]), d[1]), true
	}
}

// holiday is a theme's window around a date. from and to are days from the anchor, inclusive;
// coreFrom and coreTo are the holiday itself, as opposed to the days leading up to it.
type holiday struct {
	id               string
	anchor           func(y int) (time.Time, bool)
	from, to         int
	coreFrom, coreTo int
}

var holidays = []holiday{
	{"new-year", fixed(time.January, 1), -2, 1, -1, 0},            // 30 December to 2 January
	{"chinese-new-year", table(chineseNewYear), -3, 7, 0, 2},      //
	{"valentines", fixed(time.February, 14), -6, 0, 0, 0},         // from 8 February
	{"mardi-gras", moving(MardiGras), -6, 0, 0, 0},                // from the Wednesday before
	{"st-patricks", fixed(time.March, 17), -3, 0, 0, 0},           // from 14 March
	{"easter", moving(Easter), -7, 1, -2, 1},                      // Palm Sunday to Easter Monday
	{"independence-day", fixed(time.July, 4), -3, 1, 0, 0},        // 1 to 5 July
	{"halloween", fixed(time.October, 31), -7, 0, 0, 0},           // from 24 October
	{"dia-de-los-muertos", fixed(time.November, 2), -1, 0, -1, 0}, // 1 and 2 November
	{"diwali", table(diwali), -2, 2, 0, 0},                        // the five days, Dhanteras to Bhai Dooj
	{"thanksgiving", moving(Thanksgiving), -6, 1, 0, 1},           // the Friday before to Black Friday
	{"hanukkah", table(hanukkah), 0, 8, 0, 8},                     // first candle to the eighth day
	{"christmas", fixed(time.December, 25), -24, 1, -1, 0},        // 1 to 26 December
}

// HolidayAt returns the holiday theme for now's calendar date in now's time zone, or "" for
// none. When windows overlap, a holiday's own days beat another's lead-up, then the shorter
// span wins (the earlier in the list if they tie), so Christmas Day shows Christmas during a
// late Hanukkah, while the rest of a Hanukkah in December shows Hanukkah.
func HolidayAt(now time.Time) string {
	y, m, d := now.Date()
	today := day(y, m, d)
	best, bestCore, bestSpan := "", false, 0
	for _, h := range holidays {
		// A window can cross New Year, so look at the anchors either side too.
		for _, year := range []int{y - 1, y, y + 1} {
			anchor, ok := h.anchor(year)
			if !ok || today.Before(anchor.AddDate(0, 0, h.from)) || today.After(anchor.AddDate(0, 0, h.to)) {
				continue
			}
			core := !today.Before(anchor.AddDate(0, 0, h.coreFrom)) && !today.After(anchor.AddDate(0, 0, h.coreTo))
			span := h.to - h.from
			if core {
				span = h.coreTo - h.coreFrom
			}
			if best == "" || core && !bestCore || core == bestCore && span < bestSpan {
				best, bestCore, bestSpan = h.id, core, span
			}
		}
	}
	return best
}
