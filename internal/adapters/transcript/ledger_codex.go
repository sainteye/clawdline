package transcript

import (
	"encoding/json"
	"strings"
)

// The token ledger's Codex half. A rollout says a turn's usage once, in an
// `event_msg` of type `token_count` written after the turn's own items: the
// tool calls before it are what that call did, and the outputs after it are
// what arrived for the next.

func (s *LedgerState) codexLine(rec object) {
	payload, ok := rec.object("payload")
	if !ok {
		return
	}
	kind, _ := payload.str("type")
	switch outer, _ := rec.str("type"); outer {
	case "turn_context":
		if model, ok := payload.str("model"); ok && model != "" {
			s.Model = model
		}
	case "event_msg":
		if kind == "token_count" {
			s.codexTokens(payload)
		}
	case "response_item":
		s.codexItem(kind, payload)
	}
}

func (s *LedgerState) codexItem(kind string, item object) {
	switch kind {
	case "message":
		role, _ := item.str("role")
		text := codexText(item["content"])
		switch role {
		case "user":
			c := codexUserCategory(text)
			if c == CategoryTalk {
				c = s.personCategory(text)
			}
			s.arrive(c, estimate(text))
		case "developer", "system":
			s.arrive(CategoryHarness, estimate(text))
		}
	case "function_call", "custom_tool_call", "local_shell_call", "web_search_call":
		name, _ := item.str("name")
		var input json.RawMessage
		switch kind {
		case "function_call":
			// Arguments are JSON inside a string.
			if args, ok := item.str("arguments"); ok {
				input = json.RawMessage(args)
			}
		case "local_shell_call":
			name = "local_shell"
			if action, ok := item.object("action"); ok {
				input, _ = json.Marshal(map[string]json.RawMessage{"command": action["command"]})
			}
		case "web_search_call":
			name = "WebSearch"
		}
		c := Classify(name, input)
		if kind == "custom_tool_call" && name == "apply_patch" {
			c = CategoryImpl
		}
		if s.CodexActions == nil {
			s.CodexActions = Sizes{}
		}
		s.CodexActions[c]++
		if id, ok := item.str("call_id"); ok {
			if s.CodexPending == nil {
				s.CodexPending = map[string]Category{}
			}
			s.CodexPending[id] = c
		}
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
		id, _ := item.str("call_id")
		c, ok := s.Tools[id]
		if !ok {
			if c, ok = s.CodexPending[id]; !ok {
				c = CategoryOther
			}
		}
		out := item["output"]
		if o, ok := rawObject(out); ok {
			if inner, ok := o["output"]; ok {
				out = inner
			} else if inner, ok := o["content"]; ok {
				out = inner
			}
		}
		s.arrive(c, resultTokens(out))
	}
}

// codexTokens is one call. A `token_count` repeated with the same running
// total is the same call said again — Codex writes one on rate-limit news too.
func (s *LedgerState) codexTokens(payload object) {
	info, ok := payload.object("info")
	if !ok {
		return
	}
	last, ok := info.object("last_token_usage")
	if !ok {
		return
	}
	if totals, ok := info.object("total_token_usage"); ok {
		total := looseInt(totals, "total_tokens")
		if total != 0 && total == s.CodexTotal {
			return
		}
		s.CodexTotal = total
	}
	input := looseInt(last, "input_tokens")
	cached := min(looseInt(last, "cached_input_tokens"), input)
	output := looseInt(last, "output_tokens")
	actions := s.CodexActions
	s.CodexActions = nil
	s.Tools, s.CodexPending = s.CodexPending, nil
	if !s.openCall(s.Model, input-cached, 0, 0, cached) {
		return
	}
	s.Open = &ledgerCall{Model: s.Model, Output: output, Actions: actions, Above: s.PrevContext > aboveContext}
	s.closeOpen()
}

// codexUserCategory is what a user message the harness wrote is: Codex puts
// the repository's instructions and its environment in user messages.
func codexUserCategory(text string) Category {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "# AGENTS.md instructions"), strings.HasPrefix(t, "<user_instructions>"),
		strings.HasPrefix(t, "<INSTRUCTIONS>"):
		return CategoryRules
	case strings.HasPrefix(t, "<environment_context>"):
		return CategoryHarness
	}
	return CategoryTalk
}
