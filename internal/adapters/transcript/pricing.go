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

// Cost is what a Summary's tokens cost at list price: cache reads at a tenth
// of the input price, 1-hour cache writes at twice it and 5-minute writes at a
// quarter more, rounded to a hundredth of a cent. Writes the Summary does not
// split are priced as 1-hour: every Claude Code cache write measured on
// 2026-09-25 was one (docs/token-ledger.md "Price"). The Swift app's
// `Orchestrator.cost(of:)` priced them all at 1.25× and under-reported.
func Cost(u Summary) (float64, bool) {
	write1h, write5m := u.CacheWrite1h, u.CacheWrite5m
	if rest := u.CacheWrite - write1h - write5m; rest > 0 {
		write1h += rest
	}
	dollars, ok := usageCost(u.Model, float64(u.Input), float64(write1h), float64(write5m),
		float64(u.CacheRead), float64(u.Output))
	if !ok {
		return 0, false
	}
	return math.Round(dollars*10_000) / 10_000, true
}

// usageCost is the unrounded price of one set of token counts, and false when
// the model has no price: unknown, never zero.
func usageCost(model string, input, write1h, write5m, read, output float64) (float64, bool) {
	in, out, ok := Price(model)
	if !ok {
		return 0, false
	}
	return (input*in + output*out + read*in*0.1 + write1h*in*2 + write5m*in*1.25) / 1_000_000, true
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
