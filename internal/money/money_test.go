package money

import (
	"encoding/json"
	"testing"
)

func TestParse(t *testing.T) {
	cases := map[string]Cents{
		"286,40":        28640,
		"286.40":        28640,
		"1.234,56 €":    123456,
		"1,234.56":      123456,
		"1.234":         123400, // German thousands separator
		"1.234.567":     123456700,
		"12.5":          1250,
		"-12,50":        -1250,
		"12,50-":        -1250,
		"EUR 7":         700,
		",5":            50,
		"0,005":         1, // rounded half away from zero
		"1\u00a0234,00": 123400,
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-", "abc", "12,3a", "1,2,3x", "€"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestParseDecimal(t *testing.T) {
	cases := map[string]Cents{
		"73.18":            7318,
		"151.237142857143": 15124,
		"2.345":            235,
		"2.3449":           234,
		"-2.345":           -235,
		"7":                700,
		"+0.1":             10,
	}
	for in, want := range cases {
		got, err := ParseDecimal(in)
		if err != nil || got != want {
			t.Errorf("ParseDecimal(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1,5", "1.2.3", "x", "1e3"} {
		if _, err := ParseDecimal(bad); err == nil {
			t.Errorf("ParseDecimal(%q) should fail", bad)
		}
	}
}

func TestFromFloat(t *testing.T) {
	cases := map[float64]Cents{1.005: 101, 151.237142857143: 15124, 73.18: 7318, -0.005: -1, 0: 0}
	for in, want := range cases {
		if got := FromFloat(in); got != want {
			t.Errorf("FromFloat(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestFormat(t *testing.T) {
	if got := Cents(123456).DE(); got != "1.234,56" {
		t.Errorf("DE = %q", got)
	}
	if got := Cents(-5).DE(); got != "-0,05" {
		t.Errorf("DE = %q", got)
	}
	if got := Cents(-123456789).String(); got != "-1234567.89" {
		t.Errorf("String = %q", got)
	}
}

func TestMulRate(t *testing.T) {
	cases := []struct {
		c    Cents
		r    Rate
		want Cents
	}{
		{28640, Percent(70), 20048},
		{28640, Percent(30), 8592},
		{4510, Percent(70), 3157},
		{1, Percent(50), 1},
		{-1, Percent(50), -1},
		{3, Percent(50), 2},
		{1000, Percent(30.5), 305},
	}
	for _, c := range cases {
		if got := c.c.MulRate(c.r); got != c.want {
			t.Errorf("%d × %s %% = %d, want %d", c.c, c.r, got, c.want)
		}
	}
}

func TestJSON(t *testing.T) {
	type rec struct {
		A Cents `json:"a"`
		R Rate  `json:"r"`
	}
	b, err := json.Marshal(rec{A: 28640, R: Percent(30.5)})
	if err != nil || string(b) != `{"a":"286.40","r":30.5}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	var got rec
	if err := json.Unmarshal([]byte(`{"a":286.4,"r":"70 %"}`), &got); err != nil || got.A != 28640 || got.R != 7000 {
		t.Fatalf("unmarshal = %+v, %v", got, err)
	}
	if err := json.Unmarshal([]byte(`{"a":"x"}`), &got); err == nil {
		t.Fatal("invalid amount accepted")
	}
	if RateOf(18020, 28640) != 6292 || RateOf(1, 0) != 0 {
		t.Fatal("RateOf")
	}
}
