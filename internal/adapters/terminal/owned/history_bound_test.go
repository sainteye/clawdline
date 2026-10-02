//go:build !windows

package owned

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/terminal"
)

func TestBoundedHistoryOutputStopsBeforeRetainingExcess(t *testing.T) {
	out := boundedOutput{limit: 8}
	if _, err := out.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("567890")); err == nil || !out.exceeded {
		t.Fatal("large capture did not stop")
	}
	if !bytes.Equal(out.Bytes(), []byte("12345678")) {
		t.Fatalf("retained %q", out.Bytes())
	}
}

func TestCaptureStopsAtByteLimit(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "capture")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '1234567890'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := &Server{binary: bin, environ: func() []string { return os.Environ() }}
	out, err := s.callWithOutputLimit(context.Background(), "", 8, "capture-pane")
	if code, ok := terminal.CodeOf(err); !ok || code != terminal.CodeHistoryTooLarge {
		t.Fatalf("capture output = %q, refusal = %v", out, err)
	}
}
