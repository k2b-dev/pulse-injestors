package certificates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func certificateFixture(t *testing.T) ([]byte, time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.test"}, NotBefore: now.Add(-2 * time.Hour), NotAfter: now.Add(-time.Hour)}
	der, err := x509.CreateCertificate(bytes.NewReader(make([]byte, 1024)), tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), now
}

func TestFirstPEMCertificateExpiryAndErrors(t *testing.T) {
	body, now := certificateFixture(t)
	root := t.TempDir()
	path := filepath.Join(root, "cert.pem")
	if err := os.WriteFile(path, append(body, body...), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "bad.pem")
	if err := os.WriteFile(bad, []byte("private invalid data"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := (Collector{Paths: []string{filepath.Join(root, "*.pem"), path, filepath.Join(root, "missing.pem")}}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "vm"}, Timestamp: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(b); err != nil {
		t.Fatal(err)
	}
	if len(b.Metrics) != 1 || b.Metrics[0].Value != -3600 || b.Metrics[0].Resource.ID != entity.ID("vm", path) || b.Metrics[0].Resource.Label != "example.test" || len(b.Events) != 1 {
		t.Fatalf("batch=%+v", b)
	}
	for _, e := range b.Events {
		if strings.Contains(e.Attributes["error"].(string), "private") || e.Attributes["path"] == nil {
			t.Fatal(e)
		}
	}
}

func TestCertificateReadLimit(t *testing.T) {
	body, now := certificateFixture(t)
	for _, size := range []int{maxCertificateBytes, maxCertificateBytes + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cert.pem")
			padded := append(append([]byte{}, body...), bytes.Repeat([]byte(" "), size-len(body))...)
			if err := os.WriteFile(path, padded, 0600); err != nil {
				t.Fatal(err)
			}
			b, err := (Collector{Paths: []string{path}}).Collect(context.Background(), monitoring.Scope{
				Resource: pulse.ResourceRef{Type: "host", ID: "vm"}, Timestamp: now})
			if size > maxCertificateBytes {
				if !errors.Is(err, errCertificateTooLarge) || !strings.Contains(err.Error(), path) || len(b.Metrics) != 0 || len(b.Events) != 1 {
					t.Fatalf("oversized file: batch=%+v err=%v", b, err)
				}
				// Exercise the read limit independently of the pre-read size check.
				if _, err := readCertificateFile(path); !errors.Is(err, errCertificateTooLarge) {
					t.Fatal(err)
				}
			} else if err != nil || len(b.Metrics) != 1 || len(b.Events) != 0 {
				t.Fatalf("exact limit: batch=%+v err=%v", b, err)
			}
		})
	}
}

func TestUnmatchedCertificateGlobIsQuiet(t *testing.T) {
	b, err := (Collector{Paths: []string{filepath.Join(t.TempDir(), "*.pem")}}).Collect(context.Background(), monitoring.Scope{})
	if err != nil || len(b.Events) != 0 || len(b.Metrics) != 0 {
		t.Fatalf("unmatched glob: %+v %v", b, err)
	}
}

func TestLongCertificatePathHasStableShortIdentity(t *testing.T) {
	body, now := certificateFixture(t)
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		root = filepath.Join(root, strings.Repeat("x", 90))
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, filename := range []string{"a.pem", "b.pem"} {
		path := filepath.Join(root, filename)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(path)))
		wantID := entity.ID("vm", hash[:16])
		for i := 0; i < 2; i++ {
			b, err := (Collector{Paths: []string{path}}).Collect(context.Background(), monitoring.Scope{
				Resource: pulse.ResourceRef{Type: "host", ID: "vm"}, Timestamp: now})
			if err != nil {
				t.Fatal(err)
			}
			if err := validation.Batch(b); err != nil {
				t.Fatal(err)
			}
			if len(b.Metrics) != 1 {
				t.Fatal(b)
			}
			resource := b.Metrics[0].Resource
			if resource.ID != wantID || resource.Label != string([]rune(path)[:240]) {
				t.Fatalf("resource=%+v, want ID=%s", resource, wantID)
			}
			if i == 0 && ids[resource.ID] {
				t.Fatal("different paths share an identity")
			}
			ids[resource.ID] = true
		}
	}
}
