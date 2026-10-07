package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

// `clawdline setting get|set <key> [value]`: one of this machine's settings
// from a terminal, through the same route and the same checks the console's
// settings page writes with (/v1/settings). It is for the settings a person
// may want to change while an experiment runs and from somewhere with no
// browser, and names them rather than taking any key, so a typo is a refusal
// here and not a question for the daemon.

// settingKeys is each key this command takes, and how its value is written.
func parseSettingBool(s string) (any, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true":
		return true, true
	case "off", "false":
		return false, true
	}
	return nil, false
}

func showSettingBool(raw json.RawMessage) string {
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return "unknown"
	}
	if value {
		return "on"
	}
	return "off"
}

var settingKeys = map[string]struct {
	// parse turns what was typed into the JSON value the route takes.
	parse func(string) (any, bool)
	// show is the value a person reads, from the route's answer.
	show func(json.RawMessage) string
	// takes is what a value may be, for the usage line and a refusal.
	takes string
}{
	"codex_default_model":  modelSetting("Codex"),
	"claude_default_model": modelSetting("Claude Code"),
	"claude_auto_compact_window": {
		parse: func(s string) (any, bool) {
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "off", "none", "0":
				return int64(0), true
			}
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			return n, err == nil
		},
		show: func(raw json.RawMessage) string {
			var n *int64
			if json.Unmarshal(raw, &n) != nil || n == nil || *n == 0 {
				return cliCopy("core", "setting.compaction_off", "off (Claude Code compacts near its own window)")
			}
			return fmt.Sprintf(cliCopy("core", "setting.tokens", "%d tokens"), *n)
		},
		takes: "a number of tokens from 50000 to 1000000, or off",
	},
	"planning_gate":     {parse: parseSettingBool, show: showSettingBool, takes: "on/off or true/false"},
	"verify_gate":       {parse: parseSettingBool, show: showSettingBool, takes: "on/off or true/false"},
	"update_auto_apply": {parse: parseSettingBool, show: showSettingBool, takes: "on/off or true/false"},
	"product_language": {
		parse: func(s string) (any, bool) {
			for _, language := range nextconfig.ProductLanguages {
				if s == language {
					return s, true
				}
			}
			return nil, false
		},
		show: func(raw json.RawMessage) string {
			if len(raw) == 0 {
				return "en"
			}
			return nextconfig.ProductLanguage(nextconfig.Values{Raw: map[string]json.RawMessage{"product_language": raw}})
		},
		takes: "en, zh-Hant, ja, zh-Hans, ko, es, pt-BR, fr or de",
	},
}

func modelSetting(assistant string) struct {
	parse func(string) (any, bool)
	show  func(json.RawMessage) string
	takes string
} {
	return struct {
		parse func(string) (any, bool)
		show  func(json.RawMessage) string
		takes string
	}{
		parse: func(raw string) (any, bool) {
			model := strings.TrimSpace(raw)
			switch strings.ToLower(model) {
			case "default", "off", "none":
				model = ""
			}
			return model, nextconfig.ValidDefaultModel(model)
		},
		show: func(raw json.RawMessage) string {
			var model *string
			if json.Unmarshal(raw, &model) != nil || model == nil || *model == "" {
				return fmt.Sprintf(cliCopy("core", "setting.model_default", "%s default"), assistant)
			}
			return *model
		},
		takes: "a model name, or default",
	}
}

func settingCommand(args []string) {
	os.Exit(runSetting(os.Stdout, os.Stderr, args, daemonSettings))
}

// settingsCall is one request to /v1/settings: GET with a nil body, POST with
// one. It answers the status and the body.
type settingsCall func(method string, body any) (int, []byte, error)

// daemonSettings reaches the running daemon with this machine's local token,
// the only one the route lets change anything.
func daemonSettings(method string, body any) (int, []byte, error) {
	req, err := daemonRequest(method, "/v1/settings", body)
	if err != nil {
		return 0, nil, err
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf(cliCopy("core", "setting.daemon_unreachable", "the daemon did not answer: %w"), err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, data, err
}

func settingUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, cliCopy("core", "setting.usage", "usage: clawdline setting get <key> | clawdline setting set <key> <value>"))
	for key, k := range settingKeys {
		fmt.Fprintf(stderr, "  %s: %s\n", key, settingTakes(key, k.takes))
	}
	return 2
}

func settingTakes(key, english string) string {
	switch key {
	case "codex_default_model", "claude_default_model":
		return cliCopy("core", "setting.takes_model", english)
	case "claude_auto_compact_window":
		return cliCopy("core", "setting.takes_tokens", english)
	case "planning_gate", "verify_gate":
		return cliCopy("core", "setting.takes_bool", english)
	case "product_language":
		return cliCopy("core", "setting.takes_language", english)
	}
	return english
}

// runSetting is the command, answering its exit status.
func runSetting(stdout, stderr io.Writer, args []string, call settingsCall) int {
	if len(args) < 2 {
		return settingUsage(stderr)
	}
	verb, key := args[0], args[1]
	k, known := settingKeys[key]
	if !known {
		fmt.Fprintf(stderr, cliCopy("core", "setting.invalid_key", "clawdline setting: %q is not a key this command sets\n"), key)
		return settingUsage(stderr)
	}
	var body any
	switch {
	case verb == "get" && len(args) == 2:
	case verb == "set" && len(args) == 3:
		value, ok := k.parse(args[2])
		if !ok {
			fmt.Fprintf(stderr, cliCopy("core", "setting.invalid_value", "clawdline setting: %s is %s, not %q\n"), key, settingTakes(key, k.takes), args[2])
			return 2
		}
		body = map[string]any{key: value}
	default:
		return settingUsage(stderr)
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	status, data, err := call(method, body)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline setting:", err)
		return 1
	}
	if status != http.StatusOK {
		// The route's refusal is `{error, detail}`; the gate's, before it, is
		// the nested envelope `{error: {code, message}}`.
		if refusal, ok := parseCLIHTTPRefusal(data); ok {
			fmt.Fprintf(stderr, cliCopy("core", "setting.refused", "clawdline setting: refused, %d %s: %s\n"), status, refusal.Code, refusal.humanDetail(currentCLILanguage()))
		} else {
			fmt.Fprintf(stderr, cliCopy("core", "setting.daemon_answered", "clawdline setting: the daemon answered %d: %s\n"), status, strings.TrimSpace(string(data)))
		}
		return 1
	}
	var snapshot map[string]json.RawMessage
	if json.Unmarshal(data, &snapshot) != nil {
		fmt.Fprintln(stderr, cliCopy("core", "setting.invalid_response", "clawdline setting: the daemon's answer was not the settings"))
		return 1
	}
	fmt.Fprintf(stdout, "%s: %s\n", key, k.show(snapshot[key]))
	return 0
}
