package civil

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	want := New(2026, time.March, 4)
	for _, s := range []string{"2026-03-04", "04.03.2026", "4.3.2026", "04.03.26", " 4.3.26 "} {
		got, err := Parse(s)
		if err != nil || !got.Equal(want) {
			t.Errorf("Parse(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "2026-13-01", "32.01.2026", "04.03.2026x", "heute"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) should fail", s)
		}
	}
}

func TestArithmetic(t *testing.T) {
	d := MustParse("2024-02-29")
	if got := d.AddYears(3); got.String() != "2027-03-01" {
		t.Errorf("AddYears = %s", got)
	}
	if n := MustParse("2026-01-01").DaysUntil(MustParse("2026-03-01")); n != 59 {
		t.Errorf("DaysUntil = %d", n)
	}
	if n := MustParse("2026-03-01").DaysUntil(MustParse("2026-01-01")); n != -59 {
		t.Errorf("DaysUntil = %d", n)
	}
	if !MustParse("2026-03-10").Between(MustParse("2026-03-01"), MustParse("2026-03-10")) {
		t.Error("Between should be inclusive")
	}
	if (Date{}).Year() != 0 || (Date{}).String() != "" || (Date{}).DE() != "" {
		t.Error("zero date")
	}
	if MustParse("2026-03-04").DE() != "04.03.2026" {
		t.Error("DE")
	}
}

func TestJSON(t *testing.T) {
	type rec struct {
		A Date `json:"a"`
		B Date `json:"b,omitzero"`
	}
	b, err := json.Marshal(rec{A: MustParse("2026-03-04")})
	if err != nil || string(b) != `{"a":"2026-03-04"}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	b, _ = json.Marshal(rec{})
	if string(b) != `{"a":null}` {
		t.Fatalf("zero marshal = %s", b)
	}
	var got rec
	if err := json.Unmarshal([]byte(`{"a":"04.03.2026","b":""}`), &got); err != nil || got.A.String() != "2026-03-04" || !got.B.IsZero() {
		t.Fatalf("unmarshal = %+v, %v", got, err)
	}
	if err := json.Unmarshal([]byte(`{"a":"morgen"}`), &got); err == nil {
		t.Fatal("invalid date accepted")
	}
}
