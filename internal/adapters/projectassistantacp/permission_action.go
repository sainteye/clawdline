package projectassistantacp

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// permissionAction reads what a native permission request asks to do, so the
// card can say it before the person answers, as Codync's card does (title by
// tool kind, the command or input, and file diffs). It reads the ACP
// toolCall: title, kind, rawInput and diff content. It is display text only:
// nothing here authorizes, and a request whose toolCall cannot be read still
// becomes an offer, with no action shown.
func permissionAction(params json.RawMessage) *pa.PermissionAction {
	var p struct {
		ToolCall struct {
			Title    string          `json:"title"`
			Kind     string          `json:"kind"`
			RawInput json.RawMessage `json:"rawInput"`
			Content  []struct {
				Type    string `json:"type"`
				Path    string `json:"path"`
				OldText string `json:"oldText"`
				NewText string `json:"newText"`
				Content struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"toolCall"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	t := p.ToolCall
	a := &pa.PermissionAction{Title: t.Title, ToolKind: t.Kind, Detail: inputDetail(t.RawInput), Diffs: []pa.PermissionDiff{}}
	var text []string
	for _, block := range t.Content {
		switch {
		case block.Type == "diff" && block.Path != "":
			a.Diffs = append(a.Diffs, pa.PermissionDiff{Path: block.Path, OldText: block.OldText, NewText: block.NewText})
		case block.Type == "content" && block.Content.Type == "text" && block.Content.Text != "":
			text = append(text, block.Content.Text)
		}
	}
	if a.Detail == "" {
		a.Detail = strings.Join(text, "\n")
	}
	if a.Title == "" && a.ToolKind == "" && a.Detail == "" && len(a.Diffs) == 0 {
		return nil
	}
	shorten(a, capacity.MaximumBotPermissionActionBytes)
	return a
}

// inputDetail is the part of a tool's input a person reads first: a shell
// command, a URL, a path, else the input itself as compact JSON.
func inputDetail(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil {
		for _, name := range []string{"command", "cmd", "url", "file_path", "path", "pattern", "query"} {
			value, ok := fields[name]
			if !ok {
				continue
			}
			var s string
			if json.Unmarshal(value, &s) == nil && s != "" {
				return s
			}
			var parts []string
			if json.Unmarshal(value, &parts) == nil && len(parts) > 0 {
				return strings.Join(parts, " ")
			}
		}
	}
	var compact strings.Builder
	if b, e := json.Marshal(json.RawMessage(raw)); e == nil {
		compact.Write(b)
	}
	return compact.String()
}

// shorten keeps the whole action within limit bytes: the title and detail
// first, then each diff in order. Anything cut is marked so the card can say
// it is not the whole change.
func shorten(a *pa.PermissionAction, limit int) {
	left := limit
	take := func(s string, most int) string {
		if most < 0 {
			most = 0
		}
		if len(s) <= most {
			return s
		}
		a.Shortened = true
		cut := most
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return s[:cut]
	}
	a.Title = take(a.Title, min(left, 512))
	left -= len(a.Title)
	a.ToolKind = take(a.ToolKind, min(left, 64))
	left -= len(a.ToolKind)
	a.Detail = take(a.Detail, min(left, limit/4))
	left -= len(a.Detail)
	kept := a.Diffs[:0]
	for _, d := range a.Diffs {
		if left <= 0 {
			a.Shortened = true
			break
		}
		d.Path = take(d.Path, min(left, 1024))
		left -= len(d.Path)
		d.OldText = take(d.OldText, left/2)
		left -= len(d.OldText)
		d.NewText = take(d.NewText, left)
		left -= len(d.NewText)
		kept = append(kept, d)
	}
	a.Diffs = kept
}
