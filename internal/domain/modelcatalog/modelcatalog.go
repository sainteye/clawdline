// Package modelcatalog holds the one definition of what a model choice is for
// each agent this machine starts: the launchable model name, each agent's
// effort vocabulary and the catalog shape every console reads through
// GET /v1/model-catalog (docs/model-catalog.md). It is pure: discovering the
// models is internal/app/modelcatalog's job.
package modelcatalog

import (
	"slices"
	"strings"
	"time"
)

// The agents a model catalog is kept for, in the order every picker lists them.
const (
	Claude      = "claude"
	Codex       = "codex"
	Antigravity = "antigravity"
	Grok        = "grok"
)

// Agents is every agent with a catalog.
var Agents = []string{Claude, Codex, Antigravity, Grok}

// KnownAgent reports whether name is one of Agents.
func KnownAgent(name string) bool { return slices.Contains(Agents, name) }

// MaxModelNameBytes is the longest model name a launcher accepts.
const MaxModelNameBytes = 64

// ModelName is SessionLaunchPolicy.modelName: `[a-z0-9._-]`, 1…64, never
// opening with `-`. No string it admits holds a character a shell reads.
// Every launcher and every saved model choice uses this one rule.
func ModelName(raw string) (string, bool) {
	if raw == "" || len(raw) > MaxModelNameBytes || strings.HasPrefix(raw, "-") {
		return "", false
	}
	for _, r := range raw {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return "", false
		}
	}
	return raw, true
}

// efforts is the one table of effort names a launch from this machine may
// carry, per agent. Codex lists two rather than its own five: the value travels
// from a brief a person wrote to a command line, so it is a closed list, and
// the default is already what an unset field gets. Antigravity's standalone
// CLI takes the closed --effort vocabulary below. Claude Code and Grok take no
// effort from a launch.
var efforts = map[string][]string{
	Codex:       {"high", "xhigh"},
	Antigravity: {"low", "medium", "high", "xhigh", "max"},
}

// Efforts is agent's effort vocabulary, in the order a refusal lists it; an
// empty slice for an agent that takes none. The caller owns the copy.
func Efforts(agent string) []string {
	return append([]string{}, efforts[agent]...)
}

// KnownEffort reports whether effort is in agent's vocabulary. Exact match.
func KnownEffort(agent, effort string) bool { return slices.Contains(efforts[agent], effort) }

// Source says which tier answered a catalog.
type Source string

const (
	// Native is the fast tier: the agent's own files or CLI, read without
	// starting an agent session.
	Native Source = "native"
	// Agent is the agent's own answer, from a disposable ACP discovery session.
	Agent Source = "agent"
)

// Model is one choice a picker offers. Value always passes ModelName; the
// agent's own "no model" default is never an entry, so a picker offers
// "Agent default (<DefaultName>)" first.
type Model struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Catalog is one agent's model list as every console reads it.
type Catalog struct {
	Agent  string  `json:"agent"`
	Models []Model `json:"models"`
	// DefaultName is what an unset model runs, as the agent named it; empty
	// when unknown.
	DefaultName string   `json:"default_name"`
	Efforts     []string `json:"efforts"`
	Source      Source   `json:"source"`
	// ObservedAt is when Models was read; zero before anything was.
	ObservedAt time.Time `json:"observed_at"`
	// Refreshing is true while an agent discovery for this agent is queued
	// or running; a reader polls until it is false.
	Refreshing bool `json:"refreshing"`
	// Problem is the typed reason the last agent discovery failed, while the
	// list shown is the last good one. Empty after a success.
	Problem string `json:"problem,omitempty"`
	// ProblemField narrows Problem, such as the check that refused.
	ProblemField string `json:"problem_field,omitempty"`
}

// The catalog's bounds, registered in internal/domain/capacity.
const (
	// MaxCatalogModels is the most models kept for one agent; the rest of a
	// longer answer is dropped in the agent's own order.
	MaxCatalogModels = 64
	// MaxRefreshSeconds is how long an agent discovery answer is served
	// before the next read starts a background refresh.
	MaxRefreshSeconds = 600
	// MaxRetrySeconds is how long a failed discovery waits before a read may
	// start another one; the last good list is served meanwhile.
	MaxRetrySeconds = 60
	// MaxProbeSeconds is the deadline for one agent discovery, process start
	// and cleanup included.
	MaxProbeSeconds = 90
)
