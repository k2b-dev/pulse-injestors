package certificates

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

const maxCertificateBytes = 1 << 20

var errCertificateTooLarge = errors.New("certificate file exceeds 1 MiB")

type Collector struct{ Paths []string }

func (c Collector) Name() string { return "certificates" }
func (c Collector) Collect(_ context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	b := monitoring.NewBuilder(scope)
	seen := map[string]bool{}
	var errs []error
	failed := func(path string) {
		b.EventDetails("system.certificate.collect.failed", nil, monitoring.EventDetails{Attributes: map[string]any{"path": path, "error": "certificate is unreadable or invalid"}})
	}
	for _, pattern := range c.Paths {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			failed(pattern)
			continue
		}
		for _, path := range paths {
			if seen[path] {
				continue
			}
			seen[path] = true
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				failed(path)
				continue
			}
			var raw []byte
			if info.Size() > maxCertificateBytes {
				err = errCertificateTooLarge
			} else {
				raw, err = readCertificateFile(path)
			}
			if err != nil {
				if errors.Is(err, errCertificateTooLarge) {
					errs = append(errs, fmt.Errorf("%s: %w", path, err))
				}
				failed(path)
				continue
			}
			var cert *x509.Certificate
			for len(raw) > 0 {
				block, rest := pem.Decode(raw)
				if block == nil {
					break
				}
				raw = rest
				if block.Type != "CERTIFICATE" {
					continue
				}
				cert, err = x509.ParseCertificate(block.Bytes)
				break
			}
			if err != nil || cert == nil {
				failed(path)
				continue
			}
			label := cert.Subject.CommonName
			if label == "" {
				label = filepath.Base(path)
			}
			cs := scope
			host := entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions)
			id := entity.ID(host, path)
			if utf8.RuneCountInString("certificate:"+id) > 500 {
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(path)))
				id = entity.ID(host, hash[:16])
				label = path
			}
			if runes := []rune(label); len(runes) > 240 {
				label = string(runes[:240])
			}
			cs.Resource = pulse.ResourceRef{Type: "certificate", ID: id, Label: label}
			cb := monitoring.NewBuilder(cs)
			cb.Metric("system.certificate.expires_in", "gauge", cert.NotAfter.Sub(scope.Timestamp).Seconds(), "seconds", nil)
			cb.State("system.certificate.not_after", cert.NotAfter.UTC().Format(time.RFC3339), nil)
			b.Merge(cb.Batch())
		}
	}
	return b.Batch(), errors.Join(errs...)
}

// LimitReader also bounds reads if the file grows after Stat.
func readCertificateFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCertificateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxCertificateBytes {
		return nil, errCertificateTooLarge
	}
	return raw, nil
}
