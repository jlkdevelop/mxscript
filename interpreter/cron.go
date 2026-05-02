// cron.go — minimal but correct Vixie-cron parser + scheduler used by
// the cron(spec, fn) builtin. Supports the standard 5-field expression
// (minute hour day-of-month month day-of-week) with `*`, lists, ranges,
// and `*/step`. Day-of-month and day-of-week combine with OR when both
// are restricted, matching cron's documented behavior.
package interpreter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule is a parsed cron expression. Each field is a 64-bit
// bitmask of allowed values: bit `i` set means value `i` is in the
// schedule. We use bitmasks so nextFiring's match check is a single
// AND operation per field per minute.
type CronSchedule struct {
	Minute uint64 // bits 0..59
	Hour   uint64 // bits 0..23
	Dom    uint64 // bits 1..31  (bit 0 unused)
	Month  uint64 // bits 1..12  (bit 0 unused)
	Dow    uint64 // bits 0..6   (Sunday = 0)

	// domStar / dowStar record whether the original field was `*`.
	// When both DOM and DOW are restricted, a minute matches if
	// EITHER matches (Vixie cron's "OR" behavior). When only one is
	// restricted, the unrestricted field is ignored.
	domStar bool
	dowStar bool
}

// ParseCron parses a 5-field cron expression and returns a schedule.
//
//	"0 9 * * 1-5"      every weekday at 09:00
//	"*/5 * * * *"      every 5 minutes
//	"0 0,12 * * *"     midnight and noon
//	"15 14 1 * *"      14:15 on the 1st of each month
func ParseCron(spec string) (*CronSchedule, error) {
	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d in %q", len(fields), spec)
	}
	s := &CronSchedule{}
	var err error
	if s.Minute, err = parseCronField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	if s.Hour, err = parseCronField(fields[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	if s.Dom, err = parseCronField(fields[2], 1, 31); err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	if s.Month, err = parseCronField(fields[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	if s.Dow, err = parseCronField(fields[4], 0, 6); err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}
	s.domStar = fields[2] == "*"
	s.dowStar = fields[4] == "*"
	return s, nil
}

// parseCronField turns one field into a bitmask. Accepts:
//
//	*               every value in [min, max]
//	n               single value
//	a-b             inclusive range
//	*/k             every k starting from min
//	a-b/k           every k from a to b
//	a,b,c           list of any of the above
func parseCronField(field string, min, max int) (uint64, error) {
	var bits uint64
	for _, item := range strings.Split(field, ",") {
		step := 1
		// Pull off the /step suffix if present.
		if idx := strings.Index(item, "/"); idx >= 0 {
			s, err := strconv.Atoi(item[idx+1:])
			if err != nil || s < 1 {
				return 0, fmt.Errorf("invalid step in %q", item)
			}
			step = s
			item = item[:idx]
		}
		// Resolve the range.
		var lo, hi int
		switch {
		case item == "*":
			lo, hi = min, max
		case strings.Contains(item, "-"):
			parts := strings.SplitN(item, "-", 2)
			a, errA := strconv.Atoi(parts[0])
			b, errB := strconv.Atoi(parts[1])
			if errA != nil || errB != nil {
				return 0, fmt.Errorf("invalid range %q", item)
			}
			lo, hi = a, b
		default:
			n, err := strconv.Atoi(item)
			if err != nil {
				return 0, fmt.Errorf("invalid value %q", item)
			}
			lo, hi = n, n
		}
		if lo < min || hi > max || lo > hi {
			return 0, fmt.Errorf("value %d-%d out of range [%d,%d]", lo, hi, min, max)
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

// Match reports whether the schedule fires at minute `t`. Seconds are
// ignored; callers should align `t` to the minute before checking.
func (s *CronSchedule) Match(t time.Time) bool {
	if s.Minute&(1<<uint(t.Minute())) == 0 {
		return false
	}
	if s.Hour&(1<<uint(t.Hour())) == 0 {
		return false
	}
	if s.Month&(1<<uint(t.Month())) == 0 {
		return false
	}
	domMatch := s.Dom&(1<<uint(t.Day())) != 0
	dowMatch := s.Dow&(1<<uint(t.Weekday())) != 0
	// Vixie cron OR semantics: when both are restricted, either
	// matches; when only one is restricted, the other is ignored.
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return dowMatch
	case s.dowStar:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}

// Next returns the earliest time at or after `from` (rounded to the
// next minute) at which the schedule fires. We bound the search at
// four years to handle pathological specs like "* * 29 2 *" that only
// fire on a leap-year February 29.
func (s *CronSchedule) Next(from time.Time) time.Time {
	t := from.Add(time.Minute).Truncate(time.Minute)
	deadline := t.Add(4 * 365 * 24 * time.Hour)
	for t.Before(deadline) {
		if s.Match(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{} // never fires within 4 years
}
