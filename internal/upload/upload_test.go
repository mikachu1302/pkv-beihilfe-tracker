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

// local rewrites the URL to localhost (the LAN address may not be reachable in CI).
func local(s *Server) string {
	i := strings.LastIndex(s.URL, ":")
	return "http://127.0.0.1" + s.URL[i:]
}

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
	res, err = http.Post(local(s), "image/jpeg", strings.NewReader("jpeg-daten"))
	if err != nil {
		t.Fatal(err)
	}
	name, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(name) != "Handy-20261102-101530-1.jpg" {
		t.Fatalf("upload: %d %s", res.StatusCode, name)
	}
	if b, _ := os.ReadFile(dir + "/" + string(name)); string(b) != "jpeg-daten" || s.Received() != 1 {
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
	big := bytes.Repeat([]byte("x"), MaxFileSize+1)
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
