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

	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline setting get|set <key> [value]`: one of this machine's settings
// from a terminal, through the same route and the same checks the console's
// settings page writes with (/v1/settings). It is for the settings a person
// may want to change while an experiment runs and from somewhere with no
// browser — today only `claude_auto_compact_window` — and names them rather
// than taking any key, so a typo is a refusal here and not a question for the
// daemon.

// settingKeys is each key this command takes, and how its value is written.
var settingKeys = map[string]struct {
	// parse turns what was typed into the JSON value the route takes.
	parse func(string) (any, bool)
	// show is the value a person reads, from the route's answer.
	show func(json.RawMessage) string
	// takes is what a value may be, for the usage line and a refusal.
	takes string
}{
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
				return "off (Claude Code compacts near its own window)"
			}
			return strconv.FormatInt(*n, 10) + " tokens"
		},
		takes: "a number of tokens from 50000 to 1000000, or off",
	},
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
		return 0, nil, fmt.Errorf("the daemon did not answer: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, data, err
}

func settingUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: clawdline setting get <key> | clawdline setting set <key> <value>")
	for key, k := range settingKeys {
		fmt.Fprintf(stderr, "  %s: %s\n", key, k.takes)
	}
	return 2
}

// runSetting is the command, answering its exit status.
func runSetting(stdout, stderr io.Writer, args []string, call settingsCall) int {
	if len(args) < 2 {
		return settingUsage(stderr)
	}
	verb, key := args[0], args[1]
	k, known := settingKeys[key]
	if !known {
		fmt.Fprintf(stderr, "clawdline setting: %q is not a key this command sets\n", key)
		return settingUsage(stderr)
	}
	var body any
	switch {
	case verb == "get" && len(args) == 2:
	case verb == "set" && len(args) == 3:
		value, ok := k.parse(args[2])
		if !ok {
			fmt.Fprintf(stderr, "clawdline setting: %s is %s, not %q\n", key, k.takes, args[2])
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
		// the Swift envelope `{error: {code, message}}`.
		var flat contract.Refusal
		var nested contract.AuthRefusal
		switch {
		case json.Unmarshal(data, &flat) == nil && flat.Error != "":
			fmt.Fprintf(stderr, "clawdline setting: refused, %d %s: %s\n", status, flat.Error, flat.Detail)
		case json.Unmarshal(data, &nested) == nil && nested.Error.Code != "":
			fmt.Fprintf(stderr, "clawdline setting: refused, %d %s: %s\n", status, nested.Error.Code, nested.Error.Message)
		default:
			fmt.Fprintf(stderr, "clawdline setting: the daemon answered %d: %s\n", status, strings.TrimSpace(string(data)))
		}
		return 1
	}
	var snapshot map[string]json.RawMessage
	if json.Unmarshal(data, &snapshot) != nil {
		fmt.Fprintln(stderr, "clawdline setting: the daemon's answer was not the settings")
		return 1
	}
	fmt.Fprintf(stdout, "%s: %s\n", key, k.show(snapshot[key]))
	return 0
}
