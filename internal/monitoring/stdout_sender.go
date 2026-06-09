package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
	"github.com/valentinkolb/pulse-injestors/internal/validation"
)

type StdoutSender struct {
	Writer io.Writer
	Pretty bool
	Report bool
}

func (s StdoutSender) PostBatch(ctx context.Context, batch pulse.Batch) error {
	_ = ctx
	if err := validation.Batch(batch); err != nil {
		return fmt.Errorf("local batch validation: %w", err)
	}
	w := s.Writer
	if w == nil {
		w = os.Stdout
	}
	if s.Report {
		return WriteReport(w, batch)
	}
	enc := json.NewEncoder(w)
	if s.Pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(batch)
}
