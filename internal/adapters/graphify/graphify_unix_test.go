//go:build unix

package graphify

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestIndexAndSourceSymlinksCannotBorrowOtherRoots(t *testing.T) {
	for _, kind := range []string{"index", "source", "graph"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture(t)
			other := fixture(t)
			name, target := "graphify-out", filepath.Join(other, "graphify-out")
			if kind == "source" {
				name, target = "source.go", filepath.Join(other, "source.go")
			}
			if kind == "graph" {
				name, target = "graphify-out/graph.json", filepath.Join(other, "graphify-out/graph.json")
			}
			if err := os.RemoveAll(filepath.Join(r, name)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(r, name)); err != nil {
				t.Fatal(err)
			}
			s, err := Inspect(context.Background(), r)
			if err != nil || s.State == Matched {
				t.Fatalf("%+v %v", s, err)
			}
		})
	}
}

func TestProbeRefusesAFIFOWithoutWaitingForAWriter(t *testing.T) {
	r := fixture(t)
	p := filepath.Join(r, "graphify-out/graph.json")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Inspect(context.Background(), r)
	if err != nil || s.State == Matched {
		t.Fatalf("%+v %v", s, err)
	}
}
