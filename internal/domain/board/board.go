// Package board is the project board's domain: what is being worked on.
//
// It used to hold the rules for writing the board too — a Command with an
// expected revision and a request id, and Decide — beside a store table and a
// command ledger for them. Nothing ever called any of it (D38): the board this
// daemon shows is the Swift app's, read-only, and the board it will write is
// designed afresh (docs/board-redesign.md) with its own receipts (D03). They
// are gone rather than kept for later, because a later that finds them will
// find the second spelling of a receipt that D03 removed.
package board

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Project is one place work happens. It is derived from what is on the machine
// rather than declared: a directory somebody is running an assistant in is a
// project, and one nobody is in has stopped being one.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayPath string `json:"displayPath"`
	// SessionCount is how many sessions are in it right now. It is a reading,
	// not a stored count, so it cannot drift from the thing it describes.
	SessionCount int `json:"sessionCount"`
}

// DeriveProjects turns a set of working directories into the board's projects.
func DeriveProjects(cwds []string) []Project {
	counts := map[string]int{}
	for _, cwd := range cwds {
		if cwd == "" {
			continue
		}
		counts[cwd]++
	}
	out := make([]Project, 0, len(counts))
	for path, n := range counts {
		out = append(out, Project{
			ID:           ID("project", path),
			Name:         name(path),
			DisplayPath:  path,
			SessionCount: n,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayPath < out[j].DisplayPath })
	return out
}

func name(path string) string {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	return parts[len(parts)-1]
}

// ID is a stable identity derived from a path, so the same directory is the
// same project across restarts without a stored mapping to go out of date.
func ID(prefix, path string) string {
	sum := sha256.Sum256([]byte(path))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}
