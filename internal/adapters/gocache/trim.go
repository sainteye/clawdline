// Package gocache keeps the person's default shared Go build cache
// (`go env GOCACHE`) under a total budget. Go itself removes only entries
// unused for five days and has no ceiling, so a dozen worktrees compiling,
// vetting and testing in parallel grew it to 75 GB within two days
// (2026-11-23).
//
// Trim removes only names Go writes (`xx/<64 hex>-a|-d`), oldest-used first,
// never an entry used within the protection window, and only when a complete
// measurement shows the cache over budget or its filesystem under the reserve.
// An incomplete or failed measurement is unknown and removes nothing.
package gocache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Code is a typed refusal or failure.
type Code string

const (
	// CodeNotGoCache: the directory lacks Go's own README marker.
	CodeNotGoCache Code = "go_cache_not_recognized"
	// CodeUnmeasured: listing or statting the cache failed.
	CodeUnmeasured Code = "go_cache_unmeasured"
	// CodeScanBound: the cache holds more files than one trim may list.
	CodeScanBound Code = "go_cache_scan_bound"
	// CodeDiskUnknown: free space on the cache's filesystem is unavailable.
	CodeDiskUnknown Code = "go_cache_disk_unknown"
	// CodeRemoveFailed: an entry could not be removed.
	CodeRemoveFailed Code = "go_cache_remove_failed"
	// CodeInterrupted: the context ended during the trim.
	CodeInterrupted Code = "go_cache_interrupted"
)

// Error carries the code and its cause.
type Error struct {
	Code Code
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// CodeOf answers the code of err, or "" when it is not an *Error.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func fail(code Code, err error) error { return &Error{Code: code, Err: err} }

// entryName is what Go's cache writes: an action (-a) or output (-d) file in
// a two-hex-digit subdirectory. Anything else is not ours to remove.
var entryName = regexp.MustCompile(`^[0-9a-f]{64}-[ad]$`)
var subdirName = regexp.MustCompile(`^[0-9a-f]{2}$`)

// Options are one trim's inputs. Every function field is replaceable so a
// test can inject a failure.
type Options struct {
	Dir          string
	BudgetBytes  int64
	ReserveBytes int64
	Protect      time.Duration
	MaxFiles     int64
	Now          func() time.Time
	DiskFree     func(dir string) (int64, error)
	Lstat        func(path string) (fs.FileInfo, error)
	ReadDir      func(dir string) ([]fs.DirEntry, error)
	Remove       func(path string) error
	DryRun       bool
}

// Report is what a trim measured and did.
type Report struct {
	Dir         string `json:"dir"`
	BeforeBytes int64  `json:"before_bytes"`
	AfterBytes  int64  `json:"after_bytes"`
	FreeBefore  int64  `json:"free_before_bytes"`
	Files       int64  `json:"files"`
	Removed     int64  `json:"removed"`
	FreedBytes  int64  `json:"freed_bytes"`
	Protected   int64  `json:"protected"`
	Unknown     int64  `json:"unrecognized"`
	NeedBytes   int64  `json:"need_bytes"`
	Triggered   string `json:"triggered,omitempty"` // "budget", "reserve" or ""
	DryRun      bool   `json:"dry_run,omitempty"`
}

type entry struct {
	path  string
	size  int64
	mtime time.Time
}

func (o *Options) defaults() {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Lstat == nil {
		o.Lstat = os.Lstat
	}
	if o.ReadDir == nil {
		o.ReadDir = os.ReadDir
	}
	if o.Remove == nil {
		o.Remove = os.Remove
	}
	if o.DiskFree == nil {
		o.DiskFree = DiskFree
	}
}

// Recognized reports whether dir is a Go build cache: Go writes a README
// beginning with this sentence into every cache it creates.
func Recognized(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "README"))
	if err != nil {
		return false
	}
	const marker = "This directory holds cached build artifacts from the Go build system."
	return len(b) >= len(marker) && string(b[:len(marker)]) == marker
}

// Trim measures the cache and, only under measured budget or reserve
// pressure, removes recognized entries oldest-used first until both are met
// or only protected entries remain.
func Trim(ctx context.Context, o Options) (Report, error) {
	o.defaults()
	rep := Report{Dir: o.Dir, DryRun: o.DryRun}
	if info, err := o.Lstat(o.Dir); err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		if err == nil {
			err = errors.New("not a real directory")
		}
		return rep, fail(CodeNotGoCache, err)
	}
	if !Recognized(o.Dir) {
		return rep, fail(CodeNotGoCache, errors.New("README marker missing"))
	}
	free, err := o.DiskFree(o.Dir)
	if err != nil {
		return rep, fail(CodeDiskUnknown, err)
	}
	rep.FreeBefore = free
	entries, total, err := list(ctx, o, &rep)
	if err != nil {
		return rep, err
	}
	rep.BeforeBytes, rep.AfterBytes = total, total
	need := int64(0)
	if total > o.BudgetBytes {
		need, rep.Triggered = total-o.BudgetBytes, "budget"
	}
	if free < o.ReserveBytes && o.ReserveBytes-free > need {
		need, rep.Triggered = o.ReserveBytes-free, "reserve"
	}
	rep.NeedBytes = need
	if need == 0 {
		return rep, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.Before(entries[j].mtime) })
	cutoff := o.Now().Add(-o.Protect)
	for _, e := range entries {
		if rep.FreedBytes >= need {
			break
		}
		if err := ctx.Err(); err != nil {
			return rep, fail(CodeInterrupted, err)
		}
		if !e.mtime.Before(cutoff) {
			// Sorted oldest first: every remaining entry is protected too.
			rep.Protected++
			continue
		}
		// Re-read just before unlinking: a build that reused the entry since
		// the listing refreshed its mtime and keeps it.
		info, err := o.Lstat(e.path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return rep, fail(CodeUnmeasured, err)
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			rep.Protected++
			continue
		}
		if !o.DryRun {
			if err := o.Remove(e.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return rep, fail(CodeRemoveFailed, err)
			}
		}
		rep.Removed++
		rep.FreedBytes += e.size
		rep.AfterBytes -= e.size
	}
	return rep, nil
}

// list reads the two-level layout without following links. Any failure, or
// more files than the bound, is unknown and authorizes nothing.
func list(ctx context.Context, o Options, rep *Report) ([]entry, int64, error) {
	subs, err := o.ReadDir(o.Dir)
	if err != nil {
		return nil, 0, fail(CodeUnmeasured, err)
	}
	var out []entry
	var total int64
	for _, sub := range subs {
		if err := ctx.Err(); err != nil {
			return nil, 0, fail(CodeInterrupted, err)
		}
		if !sub.IsDir() || !subdirName.MatchString(sub.Name()) {
			continue
		}
		dir := filepath.Join(o.Dir, sub.Name())
		files, err := o.ReadDir(dir)
		if err != nil {
			return nil, 0, fail(CodeUnmeasured, err)
		}
		for _, f := range files {
			rep.Files++
			if rep.Files > o.MaxFiles {
				return nil, 0, fail(CodeScanBound, fmt.Errorf("more than %d files", o.MaxFiles))
			}
			info, err := f.Info()
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return nil, 0, fail(CodeUnmeasured, err)
			}
			total += info.Size()
			if !info.Mode().IsRegular() || !entryName.MatchString(f.Name()) {
				rep.Unknown++
				continue
			}
			out = append(out, entry{path: filepath.Join(dir, f.Name()), size: info.Size(), mtime: info.ModTime()})
		}
	}
	return out, total, nil
}

// TreeBytes sums apparent file sizes under dir without following links, for
// a low-space notice's largest sources. An unreadable subdirectory makes the
// sum a lower bound (complete=false); more than maxFiles, or failing to read
// dir itself, is unknown (err != nil).
func TreeBytes(ctx context.Context, dir string, maxFiles int64) (total int64, complete bool, err error) {
	var files int64
	complete = true
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path != dir && errors.Is(err, fs.ErrPermission) {
				complete = false
				return fs.SkipDir
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files++
		if files > maxFiles {
			return errors.New("scan bound")
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return total, complete, nil
}
