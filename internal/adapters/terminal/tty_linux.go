//go:build linux

package terminal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// What is on a terminal, asked of /proc.
//
// **Without it a person's close could not end a session that would not read
// its quit word.** On 2026-09-26 a tmux pane on a Linux host held a Claude
// Code stopped at its "do you trust this folder" question: `/exit` never
// showed on the input line, so the paste was refused, and with no reading of
// the processes behind the pty the ladder had nothing to signal. Three closes
// from the phone answered `close_quit_refused` and the pane stayed. macOS asks
// `kern.proc.tty` (iterm_close_darwin.go); this is the same answer from the
// files Linux keeps for every process.

// procRoot is where the process table is read.
var procRoot = "/proc"

// ttyProcesses is every process whose controlling terminal is tty, and that
// terminal's foreground process group.
//
// Each process's `stat` names its controlling tty and the tty's foreground
// group, so the listing is a walk of /proc keeping the rows whose tty is this
// one. A process that leaves during the walk is simply not in it; a process
// that could not be read for any other reason makes the listing incomplete,
// and an incomplete listing is not an answer — the ladder only signals what it
// can name.
func ttyProcesses(tty string) ([]ttyProc, int, error) {
	path := tty
	if !strings.HasPrefix(path, "/dev/") {
		path = "/dev/" + path
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, 0, errors.New("no device number for " + path)
	}
	boot, err := bootTime()
	if err != nil {
		return nil, 0, err
	}
	return procsOnTTY(procRoot, unix.Major(st.Rdev), unix.Minor(st.Rdev), boot, path)
}

// procsOnTTY is ttyProcesses once the device is known, reading root.
func procsOnTTY(root string, major, minor uint32, boot time.Time, name string) ([]ttyProc, int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, err
	}
	var out []ttyProc
	fg := -1
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			return nil, 0, err
		}
		st, err := parseProcStat(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("%s/%d/stat: %w", root, pid, err)
		}
		if st.ttyMajor != major || st.ttyMinor != minor {
			continue
		}
		if fg == -1 {
			fg = st.tpgid
		} else if st.tpgid != fg {
			return nil, 0, errors.New("the foreground group of " + name + " changed while it was read")
		}
		p := ttyProc{PID: st.pid, PPID: st.ppid, PGID: st.pgrp, Comm: st.comm}
		if !boot.IsZero() {
			p.Start = boot.Add(time.Duration(st.startTicks) * (time.Second / clockTicks))
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, 0, errors.New("no process on " + name)
	}
	return out, fg, nil
}

// clockTicks is USER_HZ, the unit of a stat's start time. It is 100 on every
// Linux architecture Go builds for; the kernel keeps it fixed for userspace
// precisely so that /proc can be read without asking.
const clockTicks = 100

// procStat is what the close needs out of one /proc/<pid>/stat line.
type procStat struct {
	pid, ppid, pgrp, tpgid int
	comm                   string
	ttyMajor, ttyMinor     uint32
	startTicks             uint64
}

// parseProcStat reads a stat line (proc_pid_stat(5)).
//
// The command is in parentheses and may itself hold spaces and parentheses, so
// it is everything between the first "(" and the *last* ")"; the fields are
// counted from there.
func parseProcStat(raw []byte) (procStat, error) {
	open := bytes.IndexByte(raw, '(')
	shut := bytes.LastIndexByte(raw, ')')
	if open < 0 || shut < open {
		return procStat{}, errors.New("no command in parentheses")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw[:open])))
	if err != nil {
		return procStat{}, fmt.Errorf("pid: %w", err)
	}
	// After the command: state(3) ppid(4) pgrp(5) session(6) tty_nr(7)
	// tpgid(8) … starttime(22), so field n is rest[n-3].
	rest := strings.Fields(string(raw[shut+1:]))
	if len(rest) < 20 {
		return procStat{}, fmt.Errorf("%d fields after the command", len(rest))
	}
	num := func(n int) (int64, error) { return strconv.ParseInt(rest[n-3], 10, 64) }
	ppid, err1 := num(4)
	pgrp, err2 := num(5)
	ttyNr, err3 := num(7)
	tpgid, err4 := num(8)
	start, err5 := strconv.ParseUint(rest[22-3], 10, 64)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		return procStat{}, err
	}
	// tty_nr packs the device as the kernel's old encoding: major in bits
	// 8–15, minor in bits 0–7 and 20–31.
	nr := uint32(ttyNr)
	return procStat{
		pid: pid, ppid: int(ppid), pgrp: int(pgrp), tpgid: int(tpgid),
		comm:     string(raw[open+1 : shut]),
		ttyMajor: (nr >> 8) & 0xff, ttyMinor: (nr & 0xff) | ((nr >> 12) & 0xfff00),
		startTicks: start,
	}, nil
}

// bootTime is when this machine booted, from /proc/stat's `btime`, the instant
// a stat's start time counts from. A machine that does not say gives no start
// times, which only costs the pid-reuse check its second half.
func bootTime() (time.Time, error) {
	f, err := os.Open(filepath.Join(procRoot, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("btime: %w", err)
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, sc.Err()
}
