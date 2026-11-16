package projectassistantwebhook

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

// GitHub field paths in upstream hooks.rs order: the first non-empty string wins.
var (
	githubText = [][]string{
		{"pull_request", "title"},
		{"issue", "title"},
		{"discussion", "title"},
		{"release", "name"},
		{"release", "tag_name"},
		{"comment", "body"},
		{"review", "body"},
		{"head_commit", "message"},
		{"workflow_run", "name"},
	}
	githubURL = [][]string{
		{"comment", "html_url"},
		{"review", "html_url"},
		{"pull_request", "html_url"},
		{"issue", "html_url"},
		{"discussion", "html_url"},
		{"release", "html_url"},
		{"workflow_run", "html_url"},
		{"compare"},
	}
)

// github is upstream's normalization: {source:"github", event:"<name>.<action>"
// (or "<name>" without a string action), repo, sender, text, url, payload}.
// repo and sender keep whatever JSON value the delivery carried there, or nil;
// text and url are the first non-empty string, or nil. payload is the whole
// delivery, unmodified.
func github(name string, payload map[string]any) pa.RoutineEvent {
	event := name
	if action, ok := payload["action"].(string); ok {
		event = name + "." + action
	}
	return pa.RoutineEvent{
		"source":  "github",
		"event":   event,
		"repo":    at(payload, "repository", "full_name"),
		"sender":  at(payload, "sender", "login"),
		"text":    firstString(payload, githubText),
		"url":     firstString(payload, githubURL),
		"payload": payload,
	}
}

func at(v any, path ...string) any {
	for _, key := range path {
		object, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = object[key]
	}
	return v
}

func firstString(payload map[string]any, paths [][]string) any {
	for _, path := range paths {
		if s, ok := at(payload, path...).(string); ok && s != "" {
			return s
		}
	}
	return nil
}
