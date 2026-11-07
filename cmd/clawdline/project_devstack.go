package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
)

// projectDevStack prints what Clawdline reads from the `.devstack.json` that
// belongs to dir: each declared server, whether its port answers, and the
// address its Project Link row opens. The guide told agents to verify with
// GET /v1/devstacks, which only a paired device could call; a Gemini Session
// read the orchestrator token file, was refused, and reversed the adapter's
// source instead (2026-11-07).
func projectDevStack(stdout, stderr io.Writer, b *broker, dir string, asJSON bool) int {
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline project devstack:", err)
		return 2
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	a, err := b.request(http.MethodGet, "/v1/devstacks", nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline project devstack:", err)
		return 3
	}
	if !a.ok() {
		return report(stdout, stderr, "project devstack", a)
	}
	var reply contract.DevStacksReply
	if err := json.Unmarshal(a.Body, &reply); err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "project.devstack_unreadable", "clawdline project devstack: the daemon's server list could not be read; update the running daemon"))
		return 3
	}
	var mine []contract.DevStack
	for _, s := range reply.Stacks {
		if sameOrInside(s.Root, abs) || sameOrInside(abs, s.Root) {
			mine = append(mine, s)
		}
	}
	if asJSON {
		body, _ := json.MarshalIndent(map[string]any{"stacks": mine, "truncated": reply.Truncated}, "", "  ")
		fmt.Fprintln(stdout, string(body))
	}
	if len(mine) == 0 {
		fmt.Fprintf(stderr, cliCopy("misc", "project.devstack_none", "clawdline project devstack: Clawdline reads no .devstack.json for %s. It looks in Projects with an icon entry and in the directories of live Sessions, walking up to the home directory.\n"), abs)
		if reply.Truncated {
			fmt.Fprintln(stderr, cliCopy("misc", "project.devstack_truncated", "The server list was cut short, so this is not proof that none exists."))
		}
		return 1
	}
	if asJSON {
		return 0
	}
	for _, s := range mine {
		fmt.Fprintf(stdout, "%s  %s  %s\n", s.Name, s.State, s.Root)
		for _, p := range s.Processes {
			link := p.URL
			if link == "" && p.Port > 0 {
				link = fmt.Sprintf("http://127.0.0.1:%d", p.Port)
			}
			port := "-"
			if p.Port > 0 {
				port = fmt.Sprint(p.Port)
			}
			fmt.Fprintf(stdout, "  %-12s %-8s port %-6s %s\n", p.Name, p.State, port, link)
		}
	}
	return 0
}

// sameOrInside reports whether path is root or lies below it.
func sameOrInside(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
