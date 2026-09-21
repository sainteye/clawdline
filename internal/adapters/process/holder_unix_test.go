//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// The real table, not a fixture: this test listens and asks who does.
func TestTheSystemNamesThisProcessAsTheListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := HolderOf(ctx, port)
	if err != nil {
		t.Fatalf("port %d, which this test holds: %v", port, err)
	}
	if h.PID != os.Getpid() {
		t.Fatalf("port %d: pid %d, and this test is %d", port, h.PID, os.Getpid())
	}
	if h.Command == "" || h.Started.IsZero() || time.Since(h.Started) < 0 || time.Since(h.Started) > time.Hour {
		t.Fatalf("pid %d: command %q, started %v", h.PID, h.Command, h.Started)
	}

	// The same port once it is let go is the system answering "nobody", which
	// is not a lookup that failed.
	_ = ln.Close()
	if _, err := HolderOf(ctx, port); !errors.Is(err, ErrNoHolder) {
		t.Fatalf("port %d after close: %v", port, err)
	}
}
