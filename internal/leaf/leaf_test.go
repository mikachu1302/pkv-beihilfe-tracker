package leaf

import (
	"encoding/json"
	"testing"
)

func TestFlattenRoundTrip(t *testing.T) {
	in := json.RawMessage(`{"a":{"b/c":1,"d":{"e":[1,{"f":2}]}},"g":null,"h":"x~y"}`)
	leaves, err := Flatten(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leaves["/a/b~1c"]; !ok || string(leaves["/a/d/e"]) != `[1,{"f":2}]` {
		t.Errorf("leaves %v", leaves)
	}
	out, _ := Unflatten(leaves)
	var x, y any
	json.Unmarshal(in, &x)
	json.Unmarshal(out, &y)
	if a, b := mustJSON(x), mustJSON(y); a != b {
		t.Errorf("round trip %s != %s", a, b)
	}
	if _, err := Flatten(json.RawMessage(`[1]`)); err == nil {
		t.Error("non-object accepted")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestRebase(t *testing.T) {
	base := json.RawMessage(`{"id":"x","provider":"Dr. A","amount":"10.00","note":"alt","claims":{"bh":{"held":true}}}`)
	// I changed the amount and removed the note in a form opened on base …
	mine := json.RawMessage(`{"id":"x","provider":"Dr. A","amount":"12.00","claims":{"bh":{"held":true}}}`)
	// … while another device renamed the provider and submitted the claim.
	theirs := json.RawMessage(`{"id":"x","provider":"Dr. B","amount":"10.00","note":"alt","claims":{"bh":{"held":true,"submitted":{"date":"2026-11-02"}}}}`)
	got, err := Rebase(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"amount":"12.00","claims":{"bh":{"held":true,"submitted":{"date":"2026-11-02"}}},"id":"x","provider":"Dr. B"}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
