package serviceprobe

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe/probetest"
)

func TestHTTPBoundaries(t *testing.T) {
	secret := "fixture-private-password"
	passfile := probetest.File(t, secret+"\n")
	for _, tc := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"valid", `{"ok":true}`, 200, false}, {"status", secret, 403, true}, {"invalid", secret, 200, true}, {"size", strings.Repeat("x", MaxBytes+1), 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "probe" || pass != secret {
					t.Error("incorrect Basic auth")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer s.Close()
			c, err := (HTTP{URL: s.URL, Username: "probe", PasswordFile: passfile, CAFile: probetest.CA(t, s)}).Open()
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var value map[string]any
			err = c.JSON(context.Background(), "/", &value)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatal("secret in error")
			}
			if got := c.Text(secret + " " + base64.StdEncoding.EncodeToString([]byte("probe:"+secret))); strings.Contains(got, secret) || strings.Contains(got, base64.StdEncoding.EncodeToString([]byte("probe:"+secret))) {
				t.Fatal("not redacted")
			}
		})
	}
}
func TestNoRedirectOrProxy(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	t.Setenv("HTTP_PROXY", target.URL)
	t.Setenv("HTTPS_PROXY", target.URL)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer s.Close()
	c, e := (HTTP{URL: s.URL}).Open()
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	var v any
	if c.JSON(context.Background(), "/", &v) == nil {
		t.Fatal("redirect accepted")
	}
	if reached {
		t.Fatal("redirect or proxy reached")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.JSON(ctx, "/", &v) == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestURLAndFiles(t *testing.T) {
	for _, s := range []string{"", "file:///etc/passwd", "http://u:secret@localhost", "http://localhost/?password=secret", "https://localhost/#secret", "http://"} {
		if ValidURL(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"http://127.0.0.1:8222", "https://[::1]:8443"} {
		if !ValidURL(s) {
			t.Fatalf("rejected %q", s)
		}
	}
	for _, s := range []string{"", "a\nb", "a\x00b", strings.Repeat("a", 4097)} {
		if _, e := Password(probetest.File(t, s)); e == nil {
			t.Fatal("invalid password accepted")
		}
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Password(fifo); e == nil {
		t.Fatal("FIFO accepted")
	}
	if _, e := TLS(probetest.File(t, "not a CA"), "", false); e == nil {
		t.Fatal("invalid CA accepted")
	}
	if _, e := Read(strings.NewReader("1234"), 3); e == nil {
		t.Fatal("limit ignored")
	}
	for _, s := range []string{"NaN", "Inf", "-1", "garbage"} {
		if _, e := Number(s); e == nil {
			t.Fatal("invalid number accepted")
		}
	}
}
