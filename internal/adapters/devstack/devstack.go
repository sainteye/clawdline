// Package devstack reads a project's dev stack as the project describes it,
// in a `.devstack.json` at its root — the file the Swift app's server list
// reads (`DevStack.swift`).
//
// **It never runs a command that file names.** The Swift app ran `status`,
// `up`, `down`, `restart` and `logs` from its native window only, and only
// after a person had trusted the file's exact bytes. This daemon's surface is
// HTTP, reachable by every paired device, and a route that runs a repository's
// commands would be code execution for all of them. What is left needs no
// trust, as the Swift app's own Tier 0 needed none: reading the file, and
// asking each port it declares whether anything is listening. Probing a TCP
// port executes nothing.
package devstack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Filename is what a project calls the file.
const Filename = ".devstack.json"

// maxSpecBytes bounds one read of a `.devstack.json` (read). A larger file is
// not a spec, and the walk goes on up past it.
const maxSpecBytes = 64 << 10

// maxDeclared bounds the processes kept from one file (page).
const maxDeclared = 32

// maxNameRunes bounds a stack's or a process's name as drawn (page).
const maxNameRunes = 64

// MaxStacks bounds one answer (page). The bar draws nine; the rest are cut,
// and the answer says so.
const MaxStacks = 64

// maxCandidates bounds the directories one discovery walks up from (read).
// The icon registry here names a few hundred; past this the rest are not
// looked at, and the answer says so.
const maxCandidates = 2048

// maxProbeLanes is how many ports are asked at once (lane).
const maxProbeLanes = 16

// ProbeTimeout is how long one port is given to accept. Loopback answers a
// closed port at once; this is for the port something holds and ignores.
const ProbeTimeout = 150 * time.Millisecond

// The commands a file may name, in the order a reader lists them.
var commandKeys = []string{"status", "up", "down", "restart", "logs", "attach"}

// Process is one process the file declares.
type Process struct {
	Name  string
	Port  int
	URL   string
	State string // running or stopped: all a probe can tell
}

// Spec is the file as read.
type Spec struct {
	// Root is the directory the file was found in.
	Root     string
	Name     string
	Declared []Process
	// Commands are the keys the file names, in commandKeys order. None of
	// them is ever run here.
	Commands []string
}

// Declares says whether the file names a command.
func (s Spec) Declares(command string) bool {
	for _, c := range s.Commands {
		if c == command {
			return true
		}
	}
	return false
}

// Stack is a spec with its ports asked.
type Stack struct {
	Spec
	// State is running, partial, stopped or unknown.
	State string
	// Unknown says why State is unknown: status_not_run or nothing_declared.
	Unknown   string
	Processes []Process
}

// Parse is `DevStack.parse`: every field but `name` optional, an unknown
// field ignored rather than fatal, a process without a name skipped. A reader
// that threw the whole file away because one key moved would be worse than one
// that shows the parts it still recognises.
func Parse(data []byte, root string) (Spec, bool) {
	var obj map[string]any
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return Spec{}, false
	}
	spec := Spec{Root: root, Name: filepath.Base(root)}
	if name, ok := obj["name"].(string); ok && strings.TrimSpace(name) != "" {
		spec.Name = name
	}
	spec.Name = oneLine(spec.Name)
	for _, key := range commandKeys {
		if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
			spec.Commands = append(spec.Commands, key)
		}
	}
	rows, _ := obj["processes"].([]any)
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		p := Process{Name: oneLine(name), State: "stopped"}
		if n, ok := row["port"].(float64); ok && n == math.Trunc(n) && n > 0 && n < 65536 {
			p.Port = int(n)
		}
		if u, ok := row["url"].(string); ok {
			p.URL = strings.TrimSpace(u)
		}
		spec.Declared = append(spec.Declared, p)
		if len(spec.Declared) == maxDeclared {
			break
		}
	}
	return spec, true
}

// oneLine is a name as a row draws it: no control characters, cut to
// maxNameRunes.
func oneLine(s string) string {
	mapped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	runes := []rune(strings.Join(strings.Fields(mapped), " "))
	if len(runes) > maxNameRunes {
		runes = runes[:maxNameRunes]
	}
	return string(runes)
}

// finder walks up from directories to the first `.devstack.json`, remembering
// every directory it has answered for, so a few hundred paths sharing parents
// cost a few hundred stats rather than thousands.
type finder struct {
	home string
	seen map[string]*Spec // directory -> the spec found from it, nil for none
}

// find is `DevStack.find(fromCwd:)`: up from dir, stopping at the home
// directory, which is checked, and never above it.
func (f *finder) find(dir string) *Spec {
	dir = filepath.Clean(dir)
	var walked []string
	var found *Spec
	for {
		if spec, ok := f.seen[dir]; ok {
			found = spec
			break
		}
		walked = append(walked, dir)
		if spec, ok := readSpec(dir); ok {
			found = &spec
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || dir == f.home || dir == "/" {
			break
		}
		dir = parent
	}
	for _, d := range walked {
		f.seen[d] = found
	}
	return found
}

// readSpec reads dir's `.devstack.json` when it is a regular file of at most
// maxSpecBytes that parses.
func readSpec(dir string) (Spec, bool) {
	file, err := os.Open(filepath.Join(dir, Filename))
	if err != nil {
		return Spec{}, false
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxSpecBytes {
		return Spec{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSpecBytes+1))
	if err != nil || len(data) > maxSpecBytes {
		return Spec{}, false
	}
	return Parse(data, dir)
}

// Discovery is what one walk found.
type Discovery struct {
	Specs []Spec
	// Truncated says some directories were not looked at, or more stacks were
	// found than MaxStacks.
	Truncated bool
}

// Discover is `Controller.refreshStacks`'s first half: every project that
// describes a stack, whether or not a session is open in it — the project
// whose servers have quietly fallen over is exactly the one nobody has a
// session in. `known` is the icon registry's directories, `live` the working
// directory of every live session.
//
// One difference, said here: a root that is a linked Git worktree is kept only
// when a live session is in it. The registry names every worktree anybody has
// opened, each carries a copy of its project's file, and the Swift app drew
// one row per copy under the same name.
func Discover(known, live []string, home string) Discovery {
	f := &finder{home: filepath.Clean(home), seen: map[string]*Spec{}}
	byRoot := map[string]Spec{}
	var out Discovery
	walked := 0
	visit := func(dir string, fromLive bool) {
		if dir == "" || !filepath.IsAbs(dir) {
			return
		}
		if walked == maxCandidates {
			out.Truncated = true
			return
		}
		walked++
		spec := f.find(dir)
		if spec == nil {
			return
		}
		if _, had := byRoot[spec.Root]; had {
			return
		}
		if !fromLive && linkedWorktree(spec.Root) {
			return
		}
		byRoot[spec.Root] = *spec
	}
	for _, dir := range live {
		visit(dir, true)
	}
	for _, dir := range known {
		visit(dir, false)
	}
	for _, spec := range byRoot {
		out.Specs = append(out.Specs, spec)
	}
	sort.Slice(out.Specs, func(i, j int) bool {
		if out.Specs[i].Name != out.Specs[j].Name {
			return out.Specs[i].Name < out.Specs[j].Name
		}
		return out.Specs[i].Root < out.Specs[j].Root
	})
	if len(out.Specs) > MaxStacks {
		out.Specs, out.Truncated = out.Specs[:MaxStacks], true
	}
	return out
}

// linkedWorktree says root's `.git` is a file: a checkout made with `git
// worktree add`, whose repository lives somewhere else.
func linkedWorktree(root string) bool {
	st, err := os.Lstat(filepath.Join(root, ".git"))
	return err == nil && st.Mode().IsRegular()
}

// Prober asks whether anything accepts a connection on 127.0.0.1:port.
type Prober func(ctx context.Context, port int) bool

// Listening is the production Prober: a direct connect, as the Swift app's
// `isListening` is, rather than `lsof`, which is slow and on a busy machine
// fond of reading every open file on the system to answer.
func Listening(ctx context.Context, port int) bool {
	if port <= 0 || port >= 65536 {
		return false
	}
	d := net.Dialer{Timeout: ProbeTimeout}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Read is `DevStack.probeDeclared`, for every spec at once: each declared port
// asked, maxProbeLanes at a time, and a verdict for the stack.
//
// A stack that declares nothing is `unknown`, never `stopped`: it is very
// likely running, and a mark that says "down" about a live site makes the
// next real outage look identical to it.
func Read(ctx context.Context, specs []Spec, probe Prober) []Stack {
	out := make([]Stack, len(specs))
	lanes := make(chan struct{}, maxProbeLanes)
	var wg sync.WaitGroup
	for i, spec := range specs {
		out[i] = Stack{Spec: spec, Processes: append([]Process(nil), spec.Declared...)}
		for j := range out[i].Processes {
			p := &out[i].Processes[j]
			if p.Port == 0 {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case lanes <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-lanes }()
				if probe(ctx, p.Port) {
					p.State = "running"
				}
			}()
		}
	}
	wg.Wait()
	for i := range out {
		s := &out[i]
		if len(s.Processes) == 0 {
			s.State = "unknown"
			s.Unknown = "nothing_declared"
			if s.Declares("status") {
				s.Unknown = "status_not_run"
			}
			continue
		}
		up := 0
		for _, p := range s.Processes {
			if p.State == "running" {
				up++
			}
		}
		switch up {
		case 0:
			s.State = "stopped"
		case len(s.Processes):
			s.State = "running"
		default:
			s.State = "partial"
		}
	}
	return out
}
