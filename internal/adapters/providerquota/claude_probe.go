package providerquota

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ClaudeUsageArgs start Claude Code as a print-mode control endpoint that
// never receives a prompt: no user, project or local settings (so no hooks or
// plugins of the person's run), no MCP servers, and no transcript on disk.
// `--bare` is deliberately absent: it also skips the keychain, which is where
// Claude Code keeps the sign-in this read depends on.
var ClaudeUsageArgs = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json",
	"--verbose", "--setting-sources", "", "--no-session-persistence", "--strict-mcp-config"}

// claudeUsageRequest is the Agent SDK's experimental `get_usage` control
// request. `skip_behaviors` skips the scan of a week of local transcripts.
const claudeUsageRequest = `{"type":"control_request","request_id":"clawdline-usage","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// ProbeClaude asks the Claude Code at executable for the plan windows it
// reads with its own sign-in. Clawdline never sees or refreshes a credential,
// and no model request is made. The problem is one of the footer's codes.
func ProbeClaude(ctx context.Context, executable string) ([]Window, string) {
	if executable == "" {
		return nil, "no_record"
	}
	dir, err := os.MkdirTemp("", "clawdline-claude-usage-")
	if err != nil {
		return nil, "unavailable"
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, executable, ClaudeUsageArgs...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "unavailable"
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "unavailable"
	}
	if cmd.Start() != nil {
		return nil, "unavailable"
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if _, err := io.WriteString(stdin, claudeUsageRequest); err != nil {
		return nil, "unavailable"
	}
	lines := bufio.NewScanner(io.LimitReader(stdout, ResponseBytesLimit))
	lines.Buffer(make([]byte, 0, 64<<10), ResponseBytesLimit)
	for lines.Scan() {
		windows, problem, ours := claudeUsageReply(lines.Bytes(), time.Now())
		if ours {
			return windows, problem
		}
	}
	return nil, "unavailable"
}

// claudeUsageReply reads one stream-json line; ours is false for any line
// that is not the answer to this probe's request.
func claudeUsageReply(line []byte, now time.Time) (windows []Window, problem string, ours bool) {
	var msg struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Available  *bool                      `json:"rate_limits_available"`
				RateLimits map[string]json.RawMessage `json:"rate_limits"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &msg) != nil || msg.Type != "control_response" ||
		msg.Response.RequestID != "clawdline-usage" {
		return nil, "", false
	}
	if msg.Response.Subtype != "success" || msg.Response.Response.Available == nil {
		// An older Claude Code without `get_usage` answers with an error.
		return nil, "unavailable", true
	}
	if !*msg.Response.Response.Available {
		// API key, Bedrock, Vertex or signed out: no plan windows exist.
		return nil, "no_reading", true
	}
	for _, w := range []struct{ key, name string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		var v struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    *string  `json:"resets_at"`
		}
		raw, ok := msg.Response.Response.RateLimits[w.key]
		if !ok || json.Unmarshal(raw, &v) != nil || v.Utilization == nil || *v.Utilization < 0 || *v.Utilization > 100 {
			continue
		}
		window := Window{Name: w.name, UsedPercent: *v.Utilization}
		if v.ResetsAt != nil {
			at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*v.ResetsAt))
			if err == nil {
				if !at.After(now) {
					continue // The window has reset since the provider measured it.
				}
				reset := at.Unix()
				window.ResetsAt = &reset
			}
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return nil, "no_reading", true
	}
	return windows, "", true
}
