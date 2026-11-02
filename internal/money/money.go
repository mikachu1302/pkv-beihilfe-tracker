// Package money handles euro amounts as integer cents and percentages as
// basis points, so no calculation ever touches floating point rounding.
package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Cents is an amount in euro cents.
type Cents int64

// ErrInvalid is returned for text that is not an amount.
var ErrInvalid = errors.New("not a valid amount")

// FromFloat converts a euro amount to cents. It rounds half away from zero on
// the shortest decimal representation of f, so 1.005 becomes 1.01.
func FromFloat(eur float64) Cents {
	c, err := ParseDecimal(strconv.FormatFloat(eur, 'f', -1, 64))
	if err != nil { // NaN or Inf
		return 0
	}
	return c
}

// ParseDecimal reads the machine format: optional sign, digits, optional "."
// and any number of decimals ("286.4", "-12.50", "151.237142857143").
// More than two decimals are rounded half away from zero.
func ParseDecimal(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" && frac == "" || !digits(intPart) || !digits(frac) {
		return 0, ErrInvalid
	}
	return build(neg, intPart, frac)
}

// Parse reads an amount as people write it or OCR returns it: "286,40",
// "1.234,56 €", "286.40", "-12,50", "12,50-", "EUR 7". A single "." followed by
// exactly three digits is a German thousands separator ("1.234" = 1234 €).
func Parse(s string) (Cents, error) {
	s = strings.NewReplacer("€", "", "EUR", "", "Eur", "", "eur", "",
		"\u00a0", "", "\u202f", "", " ", "", "'", "").Replace(strings.TrimSpace(s))
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasSuffix(s, "-"):
		neg, s = true, s[:len(s)-1]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	lastDot, lastComma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	var intPart, frac string
	switch {
	case lastDot >= 0 && lastComma >= 0:
		sep, thousands := lastComma, "."
		if lastDot > lastComma {
			sep, thousands = lastDot, ","
		}
		intPart, frac = strings.ReplaceAll(s[:sep], thousands, ""), s[sep+1:]
	case lastComma >= 0:
		if strings.Count(s, ",") > 1 { // 1,234,567
			intPart = strings.ReplaceAll(s, ",", "")
		} else {
			intPart, frac = s[:lastComma], s[lastComma+1:]
		}
	case lastDot >= 0:
		if after := s[lastDot+1:]; strings.Count(s, ".") > 1 || len(after) == 3 {
			intPart = strings.ReplaceAll(s, ".", "")
		} else {
			intPart, frac = s[:lastDot], after
		}
	default:
		intPart = s
	}
	if intPart == "" && frac == "" || !digits(intPart) || !digits(frac) {
		return 0, ErrInvalid
	}
	return build(neg, intPart, frac)
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func build(neg bool, intPart, frac string) (Cents, error) {
	if intPart == "" {
		intPart = "0"
	}
	if len(intPart) > 15 {
		return 0, ErrInvalid
	}
	euros, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	f := frac + "00"
	v := euros*100 + int64(f[0]-'0')*10 + int64(f[1]-'0')
	if len(frac) > 2 && frac[2] >= '5' {
		v++
	}
	if neg {
		v = -v
	}
	return Cents(v), nil
}

// String returns the machine format used in files: "-1234.50".
func (c Cents) String() string {
	v, sign := int64(c), ""
	if v < 0 {
		v, sign = -v, "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// DE returns the German format without currency sign: "-1.234,50".
func (c Cents) DE() string {
	v, sign := int64(c), ""
	if v < 0 {
		v, sign = -v, "-"
	}
	euros := strconv.FormatInt(v/100, 10)
	var b strings.Builder
	for i, r := range euros {
		if i > 0 && (len(euros)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s%s,%02d", sign, b.String(), v%100)
}

// Abs returns the absolute amount.
func (c Cents) Abs() Cents {
	if c < 0 {
		return -c
	}
	return c
}

// MulRate returns c × r, rounded half away from zero to whole cents.
func (c Cents) MulRate(r Rate) Cents {
	p := int64(c) * int64(r)
	q, rem := p/int64(Full), p%int64(Full)
	switch {
	case rem*2 >= int64(Full):
		q++
	case rem*2 <= -int64(Full):
		q--
	}
	return Cents(q)
}

// MarshalJSON writes the amount as a string ("286.40") so files stay exact and readable.
func (c Cents) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// UnmarshalJSON accepts a string or a number in the machine format.
func (c *Cents) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	}
	v, err := ParseDecimal(s)
	if err != nil {
		return fmt.Errorf("amount %q: %w", s, err)
	}
	*c = v
	return nil
}

// Rate is a percentage in basis points: 5000 means 50 %.
type Rate int64

// Full is 100 %.
const Full Rate = 10000

// Percent converts a percentage (50, 30.5) to a Rate.
func Percent(p float64) Rate { return Rate(math.Round(p * 100)) }

// RateOf returns part / whole as a Rate (0 if whole is 0).
func RateOf(part, whole Cents) Rate {
	if whole == 0 {
		return 0
	}
	return Rate(math.Round(float64(part) * float64(Full) / float64(whole)))
}

// Percent returns the rate as a percentage (50, 30.5).
func (r Rate) Percent() float64 { return float64(r) / 100 }

// String returns the percentage without sign ("50", "30.5").
func (r Rate) String() string { return strconv.FormatFloat(r.Percent(), 'f', -1, 64) }

// MarshalJSON writes the percentage as a number: 50 means 50 %.
func (r Rate) MarshalJSON() ([]byte, error) { return []byte(r.String()), nil }

// UnmarshalJSON accepts a number or a string such as "70", "30,5" or "50 %".
func (r *Rate) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		return nil
	}
	s = strings.ReplaceAll(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%")), ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("rate %q: %w", string(b), err)
	}
	*r = Percent(f)
	return nil
}
