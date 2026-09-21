package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// capacityCommand is `clawdline doctor capacity --drill <row>`: fill one row
// of the capacity register on purpose, through its real write path, and show
// that it says so (docs/limits.md §4.7, layer 3). The rows it can fill, and
// what each one's at-limit behaviour is expected to move, are in doctor.go.
//
// **Only in a throwaway directory.** By default it makes a new temporary one
// and removes it afterwards. A directory given with --dir must be empty or not
// exist yet: a directory with anything in it may be somebody's — this
// daemon's, a test's, a person's — and filling somebody's audit to see it
// rotate is exactly the kind of test that wrote fake landings into a real
// store (limits §3.5).
//
// It exits 0 only when the row went ok → warn → critical → full, its at-limit
// behaviour happened exactly once — a refusal as the row's own typed error —
// and exactly one notice was produced.
func capacityCommand(args []string) {
	fs := flag.NewFlagSet("doctor capacity", flag.ContinueOnError)
	name := fs.String("drill", "", "the row to fill on purpose: "+strings.Join(drillNames(), ", "))
	dir := fs.String("dir", "", "a throwaway state directory, empty or not there yet (default: a new temporary one)")
	limit := fs.String("limit", "", "the limit to run the row at; it may only be lower than the register's (default: 4KiB for a row of bytes, 20 for a row of rows)")
	keep := fs.Bool("keep", false, "leave the directory behind to look at")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *name == "" {
		fmt.Fprintf(os.Stderr, "usage: clawdline doctor capacity --drill <%s> [--dir D] [--limit N] [--keep]\n", strings.Join(drillNames(), "|"))
		os.Exit(2)
	}
	d, ok := drills[*name]
	if !ok {
		// Named, not ignored: the register may have the row, and the drill
		// does not fill it.
		fmt.Fprintf(os.Stderr, "clawdline: %s has no drill; the rows with one are: %s\n", *name, strings.Join(drillNames(), ", "))
		os.Exit(2)
	}
	if *limit == "" {
		*limit = d.limit
	}
	resolved, problems := capacity.Resolve(capacity.Register(), *name+"="+*limit)
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "clawdline: %s\n", strings.Join(problems, "; "))
		os.Exit(2)
	}
	var row capacity.Resolved
	for _, r := range resolved {
		if r.Entry.Name == *name {
			row = r
		}
	}

	path, cleanup, err := drillDir(*dir, *keep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(2)
	}
	defer cleanup()
	write, read, err := d.open(path, row.Limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}

	tracker := capacity.NewTracker()
	states := []string{}
	notices := []string{}
	var last capacity.Status
	observe := func() {
		st, events := tracker.Observe(row, read(), time.Now())
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
	// Twice what the limit could hold at the smallest write any drill makes,
	// so a row that never acts ends the drill instead of running forever.
	budget := int(row.Limit)*2 + 10
	if row.Entry.Unit == capacity.Bytes {
		budget = int(row.Limit/40)*2 + 10
	}
	written := 0
	var refusal, unexpected error
	for ; written < budget && counterOf(last.Reading, d.action) == 0; written++ {
		if err := write(written); err != nil {
			if d.refusal != nil && errors.Is(err, d.refusal) {
				refusal = err
			} else {
				unexpected = err
			}
		}
		observe()
		if unexpected != nil {
			break
		}
	}

	fmt.Printf("dir       %s\n", path)
	fmt.Printf("row       %s (%s), limit %d %s, at the limit: %s\n", row.Entry.Name, row.Entry.Class, row.Limit, row.Entry.Unit, row.Entry.AtLimit)
	fmt.Printf("wrote     %d times through the row's own writer\n", written)
	fmt.Printf("states    %s → %s=%d\n", strings.Join(states, " → "), d.action, counterOf(last.Reading, d.action))
	if refusal != nil {
		fmt.Printf("refusal   %v\n", refusal)
	}
	if unexpected != nil {
		fmt.Printf("error     %v\n", unexpected)
	}
	fmt.Printf("notices   %d", len(notices))
	if len(notices) > 0 {
		fmt.Printf(" (%s; %d more held back by the one-a-day rule)", strings.Join(notices, ", "), last.Suppressed)
	}
	fmt.Println()
	body, _ := json.MarshalIndent(httptransport.CapacityEntry(last), "          ", "  ")
	fmt.Printf("row now   %s\n", body)

	want := []string{"ok", "warn", "critical", "full"}
	climbed := len(states) >= len(want) && strings.Join(states[:len(want)], ",") == strings.Join(want, ",")
	refusedAsTyped := d.refusal == nil || refusal != nil
	if !climbed || counterOf(last.Reading, d.action) != 1 || !refusedAsTyped || unexpected != nil ||
		len(notices) != 1 || last.Reading.Failing {
		fmt.Printf("result    FAILED: the row did not go ok → warn → critical → full, act (%s) exactly once and produce exactly one notice\n", d.action)
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
