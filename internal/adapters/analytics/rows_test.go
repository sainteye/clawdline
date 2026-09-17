package analytics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A record that cannot be read yet must make the Usage read say "busy", not
// answer with whatever the other records add up to: an empty or short
// portfolio looks exactly like a quiet month. Once the record can be read,
// the same collector answers with it.
func TestRowsAreBusyWhileARecordIsStuck(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A FIFO blocks the reader's open until something writes to it: a record
	// on a disk that has stopped answering.
	stuck := filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jsonl")
	if err := syscall.Mkfifo(stuck, 0o600); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}
	c := NewCollector(home, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if rows, err := c.Rows(ctx, time.Time{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy while the record is stuck, got %d rows, err %v", len(rows), err)
	}

	w, err := os.OpenFile(stuck, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Two lines of one reply, as Claude Code writes them: each carries the
	// usage, and the Swift app adds both.
	line := `{"type":"assistant","timestamp":"2026-09-01T00:00:00Z","cwd":"/nowhere","entrypoint":"cli","message":{"id":"m1","model":"claude-opus-5","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}` + "\n"
	if _, err := w.WriteString(line + line); err != nil {
		t.Fatal(err)
	}
	w.Close()

	rows, err := c.Rows(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want one row, got %d", len(rows))
	}
	r := rows[0]
	got := [4]int64{*r.Tokens[0], *r.Tokens[1], *r.Tokens[2], *r.Tokens[3]}
	if got != [4]int64{2, 4, 6, 8} || r.Model != "claude-opus-5" || r.Origin != "manual" {
		t.Fatalf("row = %v model %q origin %q", got, r.Model, r.Origin)
	}
}
