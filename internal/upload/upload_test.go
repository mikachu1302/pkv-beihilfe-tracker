package upload

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func start(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	now := func() time.Time { return time.Date(2026, 11, 2, 10, 15, 30, 0, time.Local) }
	s, err := Start(dir, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	return s, dir
}

// local returns the link (the server only listens on the address in it).
func local(s *Server) string { return s.URL }

var jpeg = "\xFF\xD8\xFF\xE0jpeg-daten"

func TestPageAndUpload(t *testing.T) {
	s, dir := start(t)
	res, err := http.Get(local(s))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("Beleg an den Computer senden")) {
		t.Fatalf("page: %d %s", res.StatusCode, body[:min(len(body), 80)])
	}
	res, err = http.Post(local(s), "image/jpeg", strings.NewReader(jpeg))
	if err != nil {
		t.Fatal(err)
	}
	name, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(name) != "Handy-20261102-101530-1.jpg" {
		t.Fatalf("upload: %d %s", res.StatusCode, name)
	}
	if b, _ := os.ReadFile(dir + "/" + string(name)); string(b) != jpeg || s.Received() != 1 {
		t.Errorf("stored %q, received %d", b, s.Received())
	}
}

func TestRejects(t *testing.T) {
	s, dir := start(t)
	base := local(s)
	wrong := base[:strings.LastIndex(base, "/")+1] + "falsch"
	res, err := http.Get(wrong)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("wrong token: %d", res.StatusCode)
	}
	res, err = http.Post(base, "text/html", strings.NewReader("<script>"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("html accepted: %d", res.StatusCode)
	}
	// Content that does not match the declared type.
	res, err = http.Post(base, "application/pdf", strings.NewReader("MZ\x90\x00 kein pdf"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("executable declared as PDF accepted: %d", res.StatusCode)
	}
	big := append([]byte("%PDF-1.7 "), bytes.Repeat([]byte("x"), MaxFileSize)...)
	if res, err := http.Post(base, "application/pdf", bytes.NewReader(big)); err == nil {
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Errorf("oversized file accepted: %d", res.StatusCode)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files left behind: %v", entries)
	}
	s.Stop()
	if res, err := http.Get(base); err == nil {
		res.Body.Close()
		t.Error("server still running after Stop")
	}
}

func TestSessionLimit(t *testing.T) {
	s, _ := start(t)
	for i := 0; i <= MaxFiles; i++ {
		res, err := http.Post(local(s), "image/jpeg", strings.NewReader(jpeg))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if want := 200; i == MaxFiles {
			if res.StatusCode != 400 {
				t.Errorf("file %d beyond the limit accepted", i+1)
			}
		} else if res.StatusCode != want {
			t.Fatalf("file %d: %d", i+1, res.StatusCode)
		}
	}
}

func TestMatchesType(t *testing.T) {
	for _, c := range []struct {
		head, ctype string
		want        bool
	}{
		{"\x89PNG\r\n\x1a\n....", "image/png", true},
		{"RIFF\x00\x00\x00\x00WEBPVP8 ", "image/webp", true},
		{"\x00\x00\x00\x18ftypheic\x00\x00", "image/heic", true},
		{"\x00\x00\x00\x18ftypisom\x00\x00", "image/heic", false},
		{"%PDF-1.4", "image/jpeg", false},
		{"", "application/pdf", false},
	} {
		if got := matchesType([]byte(c.head), c.ctype); got != c.want {
			t.Errorf("matchesType(%q, %s) = %v", c.head, c.ctype, got)
		}
	}
}

func sendPage(t *testing.T, s *Server, group, ctype, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, local(s), strings.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	if group != "" {
		req.Header.Set(GroupHeader, group)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(b))
}

func TestMultiPageDocument(t *testing.T) {
	s, dir := start(t)
	pdf := "%PDF-1.4 seite"
	var names []string
	for _, p := range []struct{ group, ctype, body string }{
		{"beleg0001", "image/jpeg", jpeg},
		{"beleg0001", "application/pdf", pdf},
		{"", "image/jpeg", jpeg}, // a single document in between
		{"beleg0002", "image/jpeg", jpeg},
		{"beleg0001", "image/jpeg", jpeg},
	} {
		code, name := sendPage(t, s, p.group, p.ctype, p.body)
		if code != 200 {
			t.Fatalf("upload: %d %s", code, name)
		}
		names = append(names, name)
	}
	want := []string{"Handy-20261102-101530/01 Seite.jpg", "Handy-20261102-101530/02 Seite.pdf", "Handy-20261102-101530-3.jpg",
		"Handy-20261102-101530-2/01 Seite.jpg", "Handy-20261102-101530/03 Seite.jpg"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("page %d: %q, want %q", i, names[i], want[i])
		}
	}
	if b, _ := os.ReadFile(dir + "/Handy-20261102-101530/02 Seite.pdf"); string(b) != pdf {
		t.Errorf("stored %q", b)
	}
	if s.Received() != 5 {
		t.Errorf("received %d", s.Received())
	}
	for _, bad := range []string{"../x", "KURZ", strings.Repeat("a", 40), "a b c d e f"} {
		if code, _ := sendPage(t, s, bad, "image/jpeg", jpeg); code != http.StatusBadRequest {
			t.Errorf("group %q: %d", bad, code)
		}
	}
}
