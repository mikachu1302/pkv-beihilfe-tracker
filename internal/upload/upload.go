// Package upload lets a phone in the same network send photos and scans
// into the inbox folder: the desktop shows a QR code with a one-time link,
// the phone's browser opens a small page and uploads – no app, no cloud.
//
// The server only runs while the dialog is open (and at most TTL), accepts
// only the random token path, images and PDFs, and writes files atomically
// (hidden temp file + rename) so the folder watcher never sees half files.
package upload

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed page.html
var page []byte

// MaxFileSize is the largest accepted file.
const MaxFileSize = 40 << 20

var allowed = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/heic":      ".heic",
	"image/heif":      ".heif",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

// Server is a running upload server.
type Server struct {
	URL     string // link for the phone (may use a LAN address)
	Expires time.Time

	token string
	dir   string
	now   func() time.Time
	ln    net.Listener
	srv   *http.Server
	timer *time.Timer

	mu    sync.Mutex
	count int
	once  sync.Once
}

// Start listens on all interfaces (a free port) and writes uploads into dir.
func Start(dir string, ttl time.Duration, now func() time.Time) (*Server, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	s := &Server{token: hex.EncodeToString(b[:]), dir: dir, now: now, ln: ln, Expires: now().Add(ttl)}
	host := LANAddress()
	if host == "" {
		host = "localhost"
	}
	s.URL = fmt.Sprintf("http://%s:%d/u/%s", host, ln.Addr().(*net.TCPAddr).Port, s.token)
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute}
	go func() { _ = s.srv.Serve(ln) }()
	s.timer = time.AfterFunc(ttl, func() { _ = s.Stop() })
	return s, nil
}

// Stop ends the server (idempotent).
func (s *Server) Stop() error {
	var err error
	s.once.Do(func() {
		s.timer.Stop()
		err = s.srv.Close()
	})
	return err
}

// Received returns how many files were uploaded so far.
func (s *Server) Received() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// ServeHTTP serves the page (GET) and receives files (POST) on /u/<token>.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok, ok := strings.CutPrefix(r.URL.Path, "/u/")
	if !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' blob:")
		_, _ = w.Write(page)
	case http.MethodPost:
		name, err := s.receive(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, name)
	default:
		http.Error(w, "nicht erlaubt", http.StatusMethodNotAllowed)
	}
}

// receive stores one file sent as the raw request body with its MIME type.
func (s *Server) receive(r *http.Request) (string, error) {
	ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	ext, ok := allowed[ctype]
	if !ok {
		return "", fmt.Errorf("Dateityp %q wird nicht angenommen (nur Fotos und PDF)", ctype)
	}
	body := http.MaxBytesReader(nil, r.Body, MaxFileSize)
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tmp, body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return "", fmt.Errorf("Datei größer als %d MB", MaxFileSize>>20)
		}
		return "", err
	}
	s.mu.Lock()
	s.count++
	n := s.count
	s.mu.Unlock()
	name := fmt.Sprintf("Handy-%s-%d%s", s.now().Format("20060102-150405"), n, ext)
	if err := os.Rename(tmp.Name(), filepath.Join(s.dir, name)); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return name, nil
}

// LANAddress returns a private IPv4 address of this machine (192.168.x.x
// preferred), or "" if there is none.
func LANAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	best, bestRank := "", 99
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			rank := 3
			switch {
			case ip[0] == 192 && ip[1] == 168:
				rank = 1
			case ip[0] == 10:
				rank = 2
			}
			if rank < bestRank {
				best, bestRank = ip.String(), rank
			}
		}
	}
	return best
}
