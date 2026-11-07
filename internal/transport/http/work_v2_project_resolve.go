package http

import (
	"path/filepath"
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
// repository (a checkout and its worktree); the lower id is chosen so the
// same request always lands on the same entry.
func (c workV2ProjectCatalog) only(match func(workV2ProjectWire) bool) (workV2ProjectWire, bool) {
	var found workV2ProjectWire
	for id, p := range c {
		if match(p) && (found.ID == "" || id < found.ID) {
			found = p
		}
	}
	return found, found.ID != ""
}
