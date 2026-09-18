package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/devices"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	httptransport "github.com/sainteye/clawdline-go/internal/transport/http"
)

// capacityCommand is `clawdline doctor capacity --drill <row>`: fill one row
// of the capacity register on purpose, through its real write path, and show
// that it says so (docs/limits.md §4.7, layer 3).
//
// **Only in a throwaway directory.** By default it makes a new temporary one
// and removes it afterwards. A directory given with --dir must be empty or not
// exist yet: a directory with anything in it may be somebody's — this
// daemon's, a test's, a person's — and filling somebody's audit to see it
// rotate is exactly the kind of test that wrote fake landings into a real
// store (limits §3.5).
//
// It exits 0 only when the row went ok → warn → critical → full, its at-limit
// behaviour happened exactly once, and exactly one notice was produced.
func capacityCommand(args []string) {
	fs := flag.NewFlagSet("doctor capacity", flag.ContinueOnError)
	drill := fs.String("drill", "", "the row to fill on purpose: audit.security")
	dir := fs.String("dir", "", "a throwaway state directory, empty or not there yet (default: a new temporary one)")
	limit := fs.String("limit", "4KiB", "the limit to run the row at; it may only be lower than the register's")
	keep := fs.Bool("keep", false, "leave the directory behind to look at")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *drill == "" {
		fmt.Fprintln(os.Stderr, "usage: clawdline doctor capacity --drill audit.security [--dir D] [--limit 4KiB] [--keep]")
		os.Exit(2)
	}
	if *drill != capacity.AuditSecurity {
		// Named, not ignored: the register has the row, the drill does not
		// fill it yet.
		fmt.Fprintf(os.Stderr, "clawdline: %s has no drill yet; the rows with one are: %s\n", *drill, capacity.AuditSecurity)
		os.Exit(2)
	}
	resolved, problems := capacity.Resolve(capacity.Register(), *drill+"="+*limit)
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "clawdline: %s\n", strings.Join(problems, "; "))
		os.Exit(2)
	}
	var row capacity.Resolved
	for _, r := range resolved {
		if r.Entry.Name == *drill {
			row = r
		}
	}

	path, cleanup, err := drillDir(*dir, *keep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(2)
	}
	defer cleanup()
	files, err := devices.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	files.SetAuditLimit(row.Limit)

	tracker := capacity.NewTracker()
	states := []string{}
	notices := []string{}
	var last capacity.Status
	observe := func() {
		st, events := tracker.Observe(row, files.AuditReading(), time.Now())
		if n := len(states); n == 0 || states[n-1] != string(st.State) {
			states = append(states, string(st.State))
		}
		for _, e := range events {
			if e.Kind == capacity.EventNotify {
				notices = append(notices, fmt.Sprintf("%v at %v of %v", e.Payload["state"], e.Payload["used"], e.Payload["limit"]))
			}
		}
		last = st
	}
	observe()
	// Twice the lines the limit could hold at the shortest line this writes,
	// so a row that never rotates ends the drill instead of running forever.
	budget := int(row.Limit/40)*2 + 10
	written := 0
	for ; written < budget && last.Reading.Counters.Rotated == 0; written++ {
		files.Audit("capacity.drill", map[string]string{"n": strconv.Itoa(written)})
		observe()
	}

	fmt.Printf("dir       %s\n", path)
	fmt.Printf("row       %s (%s), limit %d %s, at the limit: %s\n", row.Entry.Name, row.Entry.Class, row.Limit, row.Entry.Unit, row.Entry.AtLimit)
	fmt.Printf("wrote     %d audit lines through the audit's own writer\n", written)
	fmt.Printf("states    %s → rotated=%d\n", strings.Join(states, " → "), last.Reading.Counters.Rotated)
	fmt.Printf("notices   %d", len(notices))
	if len(notices) > 0 {
		fmt.Printf(" (%s; %d more held back by the one-a-day rule)", strings.Join(notices, ", "), last.Suppressed)
	}
	fmt.Println()
	body, _ := json.MarshalIndent(httptransport.CapacityEntry(last), "          ", "  ")
	fmt.Printf("row now   %s\n", body)

	want := []string{"ok", "warn", "critical", "full"}
	climbed := len(states) >= len(want) && strings.Join(states[:len(want)], ",") == strings.Join(want, ",")
	if !climbed || last.Reading.Counters.Rotated != 1 || len(notices) != 1 || last.Reading.Failing {
		fmt.Println("result    FAILED: the row did not go ok → warn → critical → full, rotate exactly once and produce exactly one notice")
		os.Exit(1)
	}
	fmt.Println("result    ok")
}

// drillDir is the directory the drill may fill, and how to put it away.
func drillDir(dir string, keep bool) (string, func(), error) {
	if dir == "" {
		made, err := os.MkdirTemp("", "clawdline-capacity-drill-")
		if err != nil {
			return "", nil, err
		}
		if keep {
			return made, func() {}, nil
		}
		return made, func() { _ = os.RemoveAll(made) }, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	for _, theirs := range append(foreignDirs(), config.Dir()) {
		if t, err := filepath.Abs(theirs); err == nil && (abs == t || strings.HasPrefix(abs, t+string(filepath.Separator))) {
			return "", nil, fmt.Errorf("%s is a state directory in use, not a throwaway one", dir)
		}
	}
	entries, err := os.ReadDir(abs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, err
	}
	if len(entries) > 0 {
		// A daemon's directory has its token and its store in it; a person's
		// has something. Either way, not ours to fill.
		for _, e := range entries {
			if e.Name() == devices.LocalTokenFile || e.Name() == store.DBFile {
				return "", nil, fmt.Errorf("%s is a daemon's state directory (it has %s)", dir, e.Name())
			}
		}
		return "", nil, fmt.Errorf("%s is not empty; the drill fills only a directory made for it", dir)
	}
	// Made here, so removed here unless asked to keep it; an empty directory
	// that was already there is left as it was found, empty.
	existed := err == nil
	if !existed {
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return "", nil, err
		}
	}
	return abs, func() {
		if keep {
			return
		}
		if existed {
			entries, _ := os.ReadDir(abs)
			for _, e := range entries {
				_ = os.RemoveAll(filepath.Join(abs, e.Name()))
			}
			return
		}
		_ = os.RemoveAll(abs)
	}, nil
}
