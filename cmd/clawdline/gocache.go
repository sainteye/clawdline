package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/gocache"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// goCacheTrimTimeout bounds one trim so a post-build step can never hold the
// compile slot for long; an interrupted trim has removed only what it reported.
const goCacheTrimTimeout = 2 * time.Minute

// goCacheCommand is `clawdline go-cache trim [--dry-run] [--json]`. It trims
// only the cache `go env GOCACHE` names, and only when GOCACHE is the
// default one: an explicit GOCACHE (a gate, a task, the Linux wrapper's own)
// stays with its owner. Exit 0 trimmed or nothing to do, 2 refused/unknown.
func goCacheCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "trim" {
		fmt.Fprintln(stderr, "usage: clawdline go-cache trim [--dry-run] [--json]")
		return 2
	}
	fs := flag.NewFlagSet("go-cache trim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dry := fs.Bool("dry-run", false, "measure and report without removing")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	dir, explicit, err := goEnvCache()
	if err != nil {
		fmt.Fprintf(stderr, "go_cache_unmeasured: go env: %v\n", err)
		return 2
	}
	if explicit {
		fmt.Fprintln(stderr, "go_cache_explicit: GOCACHE is set by its owner; not trimming it")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), goCacheTrimTimeout)
	defer cancel()
	rep, err := gocache.Trim(ctx, gocache.Options{
		Dir: dir, BudgetBytes: capacity.MaxSharedGoCacheBytes,
		ReserveBytes: capacity.MaxSharedGoCacheReserveBytes,
		Protect:      time.Duration(capacity.MaxSharedGoCacheProtectSeconds) * time.Second,
		MaxFiles:     capacity.MaxSharedGoCacheScanFiles, DryRun: *dry,
	})
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(rep)
	} else if rep.Triggered != "" || err != nil {
		fmt.Fprintf(stderr, "go_cache_trim: before=%d after=%d freed=%d removed=%d protected=%d unrecognized=%d free=%d trigger=%s dry_run=%v\n",
			rep.BeforeBytes, rep.AfterBytes, rep.FreedBytes, rep.Removed, rep.Protected, rep.Unknown, rep.FreeBefore, rep.Triggered, rep.DryRun)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

// goEnvCache answers `go env GOCACHE` and whether GOCACHE was set
// explicitly (environment or go env -w) rather than defaulted.
func goEnvCache() (dir string, explicit bool, err error) {
	out, err := exec.Command("go", "env", "-json", "GOCACHE").Output()
	if err != nil {
		return "", false, err
	}
	var v struct{ GOCACHE string }
	if err := json.Unmarshal(out, &v); err != nil {
		return "", false, err
	}
	if v.GOCACHE == "" || v.GOCACHE == "off" || !filepath.IsAbs(v.GOCACHE) {
		return "", false, fmt.Errorf("unusable GOCACHE %q", v.GOCACHE)
	}
	changed, err := exec.Command("go", "env", "-changed", "GOCACHE").Output()
	if err != nil {
		return "", false, err
	}
	return v.GOCACHE, strings.TrimSpace(string(changed)) != "", nil
}
