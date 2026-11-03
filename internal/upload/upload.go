// Package upload lets a phone in the same network send photos and scans
// into the inbox folder: the desktop shows a QR code with a secret link,
// the phone's browser opens a small page and uploads – no app, no cloud.
//
// The server only runs while the dialog is open (and at most TTL) and only
// listens on the network address in the link. It accepts only the random
// token path (the link can be used for several photos until it expires),
// images and PDFs recognised by their content, at most MaxFiles files and
// MaxTotalSize bytes, and writes files atomically (hidden temp file +
// rename) so the folder watcher never sees half files.
//
// The connection is plain HTTP inside the local network: a self-signed
// certificate would make the phone's browser show a frightening warning.
// Whoever can read the Wi-Fi traffic could read the uploads; on a home
// network with WPA2/WPA3 that is the people already in it.
package upload

import (
	"bytes"
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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed page.html
var page []byte

// Limits per upload session.
const (
	MaxFileSize  = 40 << 20  // largest accepted file
	MaxFiles     = 100       // files per session
	MaxTotalSize = 500 << 20 // bytes per session
)

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

	mu     sync.Mutex
	count  int   // files received
	bytes  int64 // bytes received or being received
	groups map[string]*group
	once   sync.Once
}

// group is a multi-page document being photographed: its pages go into one
// folder of the inbox, which the app shows as one document.
type group struct {
	folder string
	pages  int
}

// GroupHeader marks a page of a multi-page document: the same random ID for
// all pages of one document, sent one after another.
const GroupHeader = "X-Beleg"

var groupIDRe = regexp.MustCompile(`^[a-z0-9]{8,32}$`)

// Start listens on the LAN address (or only locally if there is none) on a
// free port and writes uploads into dir.
func Start(dir string, ttl time.Duration, now func() time.Time) (*Server, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	host := LANAddress()
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return nil, err
	}
	s := &Server{token: hex.EncodeToString(b[:]), dir: dir, now: now, ln: ln, Expires: now().Add(ttl), groups: map[string]*group{}}
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
// With GroupHeader it is the next page of a multi-page document.
func (s *Server) receive(r *http.Request) (string, error) {
	ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	ext, ok := allowed[ctype]
	if !ok {
		return "", errors.New("Nur Fotos und PDF-Dateien können gesendet werden")
	}
	groupID := r.Header.Get(GroupHeader)
	if groupID != "" && !groupIDRe.MatchString(groupID) {
		return "", errors.New("Ungültige Kennung des mehrseitigen Belegs")
	}
	// Reserve the largest possible size up front, so parallel uploads
	// cannot exceed the limits together; the rest is given back below.
	s.mu.Lock()
	if s.count >= MaxFiles || s.bytes+MaxFileSize > MaxTotalSize {
		s.mu.Unlock()
		return "", errors.New("Für diese Verbindung wurden genug Dateien empfangen – bitte am Computer neu starten")
	}
	s.bytes += MaxFileSize
	s.mu.Unlock()
	var written int64
	defer func() {
		s.mu.Lock()
		s.bytes += written - MaxFileSize
		s.mu.Unlock()
	}()

	body := http.MaxBytesReader(nil, r.Body, MaxFileSize)
	head := make([]byte, 16)
	n, err := io.ReadFull(body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", errors.New("Die Datei ist leer oder unvollständig")
	}
	if !matchesType(head[:n], ctype) {
		return "", errors.New("Die Datei ist kein lesbares Foto oder PDF")
	}
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return "", err
	}
	written, err = io.Copy(tmp, io.MultiReader(bytes.NewReader(head[:n]), body))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		written = 0
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return "", fmt.Errorf("Die Datei ist größer als %d MB", MaxFileSize>>20)
		}
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	name := fmt.Sprintf("Handy-%s-%d%s", s.now().Format("20060102-150405"), s.count, ext)
	if groupID != "" {
		if name, err = s.pagePath(groupID, ext); err != nil {
			os.Remove(tmp.Name())
			return "", err
		}
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.dir, filepath.FromSlash(name))); err != nil {
		os.Remove(tmp.Name())
		return "", errors.New("Die Datei konnte am Computer nicht abgelegt werden – bitte erneut senden")
	}
	return name, nil
}

// pagePath returns the inbox name ("Handy-…/02 Seite.jpg") of the next page
// of a multi-page document, creating its folder for the first page (caller
// holds the lock). The page names follow the app's "NN name" convention; the
// number follows the highest page in the folder, and an existing file is
// never replaced (the user may have merged other pages into the folder).
func (s *Server) pagePath(id, ext string) (string, error) {
	g := s.groups[id]
	if g == nil {
		base := "Handy-" + s.now().Format("20060102-150405")
		folder := base
		for i := 2; ; i++ {
			if _, err := os.Lstat(filepath.Join(s.dir, folder)); errors.Is(err, os.ErrNotExist) {
				break
			}
			folder = fmt.Sprintf("%s-%d", base, i)
		}
		g = &group{folder: folder}
		s.groups[id] = g
	}
	dir := filepath.Join(s.dir, g.folder)
	// MkdirAll: also when the document was booked or split on the computer
	// meanwhile – the further pages then form a document of their own.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.New("Die Datei konnte am Computer nicht abgelegt werden – bitte erneut senden")
	}
	n := max(g.pages, highestPage(dir)) + 1
	for {
		name := fmt.Sprintf("%02d Seite%s", n, ext)
		if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			g.pages = n
			return g.folder + "/" + name, nil
		}
		n++
	}
}

var pageNumberRe = regexp.MustCompile(`^(\d{2,3}) `)

// highestPage returns the highest "NN " number of the files in dir.
func highestPage(dir string) int {
	entries, _ := os.ReadDir(dir)
	high := 0
	for _, e := range entries {
		if m := pageNumberRe.FindStringSubmatch(e.Name()); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > high {
				high = n
			}
		}
	}
	return high
}

// matchesType checks the first bytes of a file against its MIME type, so
// that only real images and PDFs end up in the inbox.
func matchesType(head []byte, ctype string) bool {
	switch ctype {
	case "image/jpeg":
		return bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF})
	case "image/png":
		return bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n"))
	case "application/pdf":
		return bytes.HasPrefix(head, []byte("%PDF-"))
	case "image/webp":
		return len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP"
	case "image/heic", "image/heif":
		// ISO base media file: size, "ftyp", brand.
		if len(head) < 12 || string(head[4:8]) != "ftyp" {
			return false
		}
		switch string(head[8:12]) {
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "mif1", "msf1", "avif":
			return true
		}
	}
	return false
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
