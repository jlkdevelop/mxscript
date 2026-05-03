package interpreter

import (
	"testing"
	"time"
)

func TestParseCronAccepts(t *testing.T) {
	cases := []string{
		"* * * * *",
		"0 9 * * 1-5",
		"*/5 * * * *",
		"0 0,12 * * *",
		"15 14 1 * *",
		"30 6 * * 0",
		"*/15 9-17 * * 1-5",
	}
	for _, src := range cases {
		if _, err := ParseCron(src); err != nil {
			t.Errorf("%q: parse failed: %v", src, err)
		}
	}
}

func TestParseCronRejects(t *testing.T) {
	cases := []string{
		"",                // empty
		"a b c d e",       // non-numeric
		"60 0 * * *",      // minute out of range
		"0 24 * * *",      // hour out of range
		"0 0 32 * *",      // dom out of range
		"0 0 * 13 *",      // month out of range
		"0 0 * * 7",       // dow out of range (0-6)
		"0 0 * * * *",     // 6 fields
		"0 0 * *",         // 4 fields
		"5-1 * * * *",     // reversed range
		"*/0 * * * *",     // step zero
		"0 9 * * 1-5/abc", // bad step
	}
	for _, src := range cases {
		if _, err := ParseCron(src); err == nil {
			t.Errorf("%q: expected parse error, got nil", src)
		}
	}
}

func TestCronMatchEveryMinute(t *testing.T) {
	s, _ := ParseCron("* * * * *")
	for _, ts := range []string{
		"2026-05-02T12:00:00Z",
		"2026-12-31T23:59:00Z",
		"2027-02-28T00:00:00Z",
	} {
		t1, _ := time.Parse(time.RFC3339, ts)
		if !s.Match(t1) {
			t.Errorf("%q: should match * * * * *", ts)
		}
	}
}

func TestCronMatchWeekdayMornings(t *testing.T) {
	// "0 9 * * 1-5" — 09:00 Monday through Friday.
	s, _ := ParseCron("0 9 * * 1-5")
	cases := []struct {
		ts   string
		want bool
	}{
		{"2026-05-04T09:00:00Z", true},  // Monday
		{"2026-05-08T09:00:00Z", true},  // Friday
		{"2026-05-09T09:00:00Z", false}, // Saturday
		{"2026-05-04T08:00:00Z", false}, // Monday but 08:00
		{"2026-05-04T09:01:00Z", false}, // Monday but 09:01
	}
	for _, c := range cases {
		ts, _ := time.Parse(time.RFC3339, c.ts)
		if got := s.Match(ts); got != c.want {
			t.Errorf("%s: got %v, want %v", c.ts, got, c.want)
		}
	}
}

func TestCronEveryFiveMinutesNext(t *testing.T) {
	s, _ := ParseCron("*/5 * * * *")
	from, _ := time.Parse(time.RFC3339, "2026-05-02T12:03:30Z")
	next := s.Next(from)
	want, _ := time.Parse(time.RFC3339, "2026-05-02T12:05:00Z")
	if !next.Equal(want) {
		t.Errorf("next: got %s, want %s", next, want)
	}
}

func TestCronStepInRange(t *testing.T) {
	// "*/15 9-17 * * *" — every 15 min from 9 to 17.
	s, _ := ParseCron("*/15 9-17 * * *")
	cases := []struct {
		ts   string
		want bool
	}{
		{"2026-05-02T09:00:00Z", true},
		{"2026-05-02T09:15:00Z", true},
		{"2026-05-02T09:14:00Z", false},
		{"2026-05-02T17:45:00Z", true},
		{"2026-05-02T18:00:00Z", false},
		{"2026-05-02T08:45:00Z", false},
	}
	for _, c := range cases {
		ts, _ := time.Parse(time.RFC3339, c.ts)
		if got := s.Match(ts); got != c.want {
			t.Errorf("%s: got %v, want %v", c.ts, got, c.want)
		}
	}
}

func TestCronDomDowOrSemantics(t *testing.T) {
	// Vixie cron: when both DOM and DOW are restricted, fire on either.
	// "0 0 1,15 * 1-5" fires on the 1st, 15th, OR any weekday — i.e.
	// every weekday plus the 1st/15th regardless of weekday.
	s, _ := ParseCron("0 0 1,15 * 1-5")
	cases := []struct {
		ts   string
		want bool
	}{
		{"2026-05-01T00:00:00Z", true},  // 1st (Friday) — both match
		{"2026-05-02T00:00:00Z", false}, // 2nd Saturday — neither
		{"2026-05-04T00:00:00Z", true},  // 4th Monday — DOW match only
		{"2026-05-15T00:00:00Z", true},  // 15th Friday — both
		{"2026-05-17T00:00:00Z", false}, // 17th Sunday — neither
	}
	for _, c := range cases {
		ts, _ := time.Parse(time.RFC3339, c.ts)
		if got := s.Match(ts); got != c.want {
			t.Errorf("%s: got %v, want %v", c.ts, got, c.want)
		}
	}
}

func TestCronNextAcrossMonthBoundary(t *testing.T) {
	// "0 0 1 * *" — midnight on the 1st of each month.
	s, _ := ParseCron("0 0 1 * *")
	from, _ := time.Parse(time.RFC3339, "2026-05-15T12:00:00Z")
	next := s.Next(from)
	want, _ := time.Parse(time.RFC3339, "2026-06-01T00:00:00Z")
	if !next.Equal(want) {
		t.Errorf("next: got %s, want %s", next, want)
	}
}
