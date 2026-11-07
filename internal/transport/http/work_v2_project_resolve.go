package http

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// workV2ProjectNotFound is the refusal an agent sees when the Project it named
// matches nothing. It says what is accepted, because the bare "choose from the
// catalog" sent a Gemini Session into the daemon's SQLite file to reverse a
// path digest after three refused guesses (2026-11-07).
const workV2ProjectNotFound = "No Project this machine lists matches that value. Pass the id `clawdline project list` shows, " +
	"the Project's directory, or nothing to use the Project this Session is working in."

// resolve finds the catalog entry an agent meant. An agent may name a Project
// by its catalog id, by the `project-…` identity other routes print, by the
// Project's directory, or not at all, in which case the Session's own working
// directory decides. Each form has to land on exactly one catalog entry; a
// value that matches none is refused rather than guessed.
func (c workV2ProjectCatalog) resolve(value, sessionCWD string) (workV2ProjectWire, bool) {
	value = strings.TrimSpace(value)
	if p, ok := c[value]; ok && value != "" {
		return p, true
	}
	if value == "" {
		value = strings.TrimSpace(sessionCWD)
	}
	switch {
	case value == "":
		return workV2ProjectWire{}, false
	case projects.IsProjectID(value):
		return c.only(func(p workV2ProjectWire) bool { return projects.ProjectID(p.Path) == value })
	case filepath.IsAbs(value):
		clean := filepath.Clean(value)
		if p, ok := c[projects.PlaceID(clean)]; ok {
			return p, true
		}
		canonical, ok := projects.CanonicalProjectKey(clean)
		if !ok {
			canonical = clean
		}
		return c.only(func(p workV2ProjectWire) bool { return filepath.Clean(p.Path) == canonical })
	}
	return workV2ProjectWire{}, false
}

// only answers the one entry that matches. Two catalog places can share a
// repository (the repository root and a folder inside it both resolve to the
// root). The place that is the repository root itself answers for it; when no
// such place exists and several match, the value is ambiguous and refused, so
// an item never lands under whichever folder hashes lowest.
func (c workV2ProjectCatalog) only(match func(workV2ProjectWire) bool) (workV2ProjectWire, bool) {
	var found []workV2ProjectWire
	for _, p := range c {
		if match(p) {
			found = append(found, p)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	for _, p := range found {
		if p.ID == projects.PlaceID(filepath.Clean(p.Path)) {
			return p, true
		}
	}
	return workV2ProjectWire{}, false
}

// sorted is the catalog in the order a person reads it: by name, then id.
func (c workV2ProjectCatalog) sorted() []workV2ProjectWire {
	out := make([]workV2ProjectWire, 0, len(c))
	for _, p := range c {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].ID < out[j].ID
	})
	return out
}
