package transcript

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Price is list price per million tokens, in US dollars, as the Swift app's
// `Orchestrator.price(forModel:)` has it. Codex bills against a plan, so an
// unknown model has no price rather than a made-up one.
func Price(model string) (input, output float64, ok bool) {
	switch {
	case strings.HasPrefix(model, "claude-fable-5"), strings.HasPrefix(model, "claude-mythos-5"):
		return 10, 50, true
	case strings.HasPrefix(model, "claude-opus"):
		return 5, 25, true
	case strings.HasPrefix(model, "claude-sonnet"):
		return 3, 15, true
	case strings.HasPrefix(model, "claude-haiku-4-5"):
		return 1, 5, true
	}
	return 0, 0, false
}

// Cost is `Orchestrator.cost(of:)`: cache reads at a tenth of the input price,
// cache writes at a quarter more, rounded to a hundredth of a cent.
func Cost(u Summary) (float64, bool) {
	in, out, ok := Price(u.Model)
	if !ok {
		return 0, false
	}
	dollars := float64(u.Input)*in +
		float64(u.Output)*out +
		float64(u.CacheRead)*in*0.1 +
		float64(u.CacheWrite)*in*1.25
	return math.Round(dollars/1_000_000*10_000) / 10_000, true
}

// ClaudeSessionCost is Claude Code's own running total for one session, as its
// status line wrote it down: `cost.total_cost_usd` in
// `~/.claude/statusline-cache/session-<id>.json`.
//
// The Swift app prefers this to the price it works out itself, because it is
// the assistant's own account and it counts what the transcript cannot see —
// work another model did on the session's behalf. Absent when no status line
// has written one.
func ClaudeSessionCost(home, sessionID string) (float64, bool) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) {
		return 0, false
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "statusline-cache", "session-"+sessionID+".json"))
	if err != nil {
		return 0, false
	}
	rec, ok := decodeObject(data)
	if !ok {
		return 0, false
	}
	cost, ok := rec.object("cost")
	if !ok {
		return 0, false
	}
	v, ok := looseFloat(cost["total_cost_usd"])
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return v, true
}

func looseFloat(raw json.RawMessage) (float64, bool) {
	if s, ok := rawString(raw); ok {
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil
	}
	var v float64
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return 0, false
	}
	return v, true
}

// Model is one row of the model picker: the id a session's current model is
// matched against, by prefix, and the name the status line shows for it.
type Model struct {
	ID      string
	Name    string
	Command string
}

// ClaudeModels are the models Claude Code answers `/model` with by alias. There
// is no file to read these from, so this is the Swift app's table as it is.
var ClaudeModels = []Model{
	{ID: "claude-fable-5", Name: "Fable 5", Command: "fable"},
	{ID: "claude-opus-5", Name: "Opus 5", Command: "opus"},
	{ID: "claude-sonnet-5", Name: "Sonnet 5", Command: "sonnet"},
	{ID: "claude-haiku-4-5", Name: "Haiku 4.5", Command: "haiku"},
}

// CodexModels is the list Codex's own picker shows, from
// `~/.codex/models_cache.json` — read rather than typed in, because the names
// change with the plan and the month. Only rows the picker would list.
func CodexModels(home string) []Model {
	data, err := os.ReadFile(filepath.Join(home, ".codex", "models_cache.json"))
	if err != nil {
		return nil
	}
	root, ok := decodeObject(data)
	if !ok {
		return nil
	}
	rows, ok := root.objects("models")
	if !ok {
		return nil
	}
	out := []Model{}
	for _, row := range rows {
		slug, ok := row.str("slug")
		if !ok || slug == "" {
			continue
		}
		if visibility, ok := row.str("visibility"); ok && visibility != "list" {
			continue
		}
		name := slug
		if shown, ok := row.str("display_name"); ok && shown != "" {
			name = shown
		}
		out = append(out, Model{ID: slug, Name: name, Command: slug})
	}
	return out
}

// Models is the picker for one assistant.
func Models(home, assistant string) []Model {
	switch assistant {
	case "claude":
		return ClaudeModels
	case "codex":
		return CodexModels(home)
	}
	return nil
}
