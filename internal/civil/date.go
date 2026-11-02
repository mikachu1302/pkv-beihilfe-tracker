// Package civil provides a calendar date without time of day or time zone.
package civil

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Date is a calendar day. The zero value means "no date".
type Date struct{ t time.Time }

// ErrInvalid is returned for text that is not a date.
var ErrInvalid = errors.New("not a valid date")

// layouts accepted by Parse: ISO and German (with or without leading zeros, 2- or 4-digit year).
var layouts = []string{"2006-01-02", "2.1.2006", "2.1.06"}

// New returns the date year-month-day (out-of-range values are normalised like time.Date).
func New(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// Of returns the calendar day of t in t's location.
func Of(t time.Time) Date {
	y, m, d := t.Date()
	return New(y, m, d)
}

// Today returns the current local date.
func Today() Date { return Of(time.Now()) }

// Parse reads "2026-03-14", "14.03.2026", "14.3.2026" or "14.03.26".
func Parse(s string) (Date, error) {
	s = strings.TrimSpace(s)
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return Of(t), nil
		}
	}
	return Date{}, fmt.Errorf("%w: %q", ErrInvalid, s)
}

// MustParse is Parse for constants in tests; it panics on invalid input.
func MustParse(s string) Date {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// IsZero reports whether d is unset.
func (d Date) IsZero() bool { return d.t.IsZero() }

// Year returns the year (0 for the zero date).
func (d Date) Year() int {
	if d.IsZero() {
		return 0
	}
	return d.t.Year()
}

// Time returns midnight UTC of d.
func (d Date) Time() time.Time { return d.t }

// String returns "2006-01-02", or "" for the zero date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format("2006-01-02")
}

// DE returns "02.01.2006", or "" for the zero date.
func (d Date) DE() string {
	if d.IsZero() {
		return ""
	}
	return d.t.Format("02.01.2006")
}

// Compare returns -1, 0 or +1.
func (d Date) Compare(o Date) int { return d.t.Compare(o.t) }

// Before reports whether d is before o.
func (d Date) Before(o Date) bool { return d.t.Before(o.t) }

// After reports whether d is after o.
func (d Date) After(o Date) bool { return d.t.After(o.t) }

// Equal reports whether d and o are the same day.
func (d Date) Equal(o Date) bool { return d.t.Equal(o.t) }

// AddDays returns d plus n days.
func (d Date) AddDays(n int) Date { return Date{d.t.AddDate(0, 0, n)} }

// AddYears returns d plus n years (29 February becomes 1 March in non-leap years).
func (d Date) AddYears(n int) Date { return Date{d.t.AddDate(n, 0, 0)} }

// DaysUntil returns the number of days from d to o (negative if o is earlier).
func (d Date) DaysUntil(o Date) int { return int(o.t.Sub(d.t).Hours() / 24) }

// Between reports whether d lies in [from, to] (inclusive).
func (d Date) Between(from, to Date) bool { return !d.Before(from) && !d.After(to) }

// MarshalJSON writes "2006-01-02", or null for the zero date.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON accepts null, "" or any format Parse understands.
func (d *Date) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*d = Date{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*d = Date{}
		return nil
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}
