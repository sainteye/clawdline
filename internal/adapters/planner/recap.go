package planner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A Session recap is the Session's own assistant answering one question about
// its conversation from a throwaway fork of it (docs/session-recaps.md).
//
// The fork is the point. The assistant that did the work reads the whole
// conversation, compactions included, which no excerpt we could cut from a
// transcript gives a smaller model. The fork is never written down: Claude Code
// runs it with --no-session-persistence and Codex with --ephemeral, so the
// original conversation, its file and its session list are left exactly as
// they were (measured 2026-11-14: the transcript's size and the number of
// session files were unchanged after both).
//
// Tools are off. A fork that ignored the question and carried on with the
// work must not be able to do anything: this repository's own settings let
// some shell commands run without a prompt. Turning tools off changes the
// prompt's prefix, so the conversation's prompt cache is missed anyway, which
// is why Claude answers with Haiku: on a 42k-token fork it took 8 s and
// 0.009 USD, against 0.34 USD for the same fork on the session's own Opus.
//
// `/recap` is not sent. On a machine where the person has a skill named recap,
// Claude Code resolves it to that skill (measured: a five-turn report with
// tool calls instead of a sentence), so the question is asked in words, with
// slash commands disabled.

// recapClaudeModel answers a fork's one question; see above.
const recapClaudeModel = "haiku"

// recapStop leads the question, because a conversation can end on a step its
// assistant was about to take, and a fork that reads the end as an
// instruction would take it. The wording follows the Recap mod for Herdr,
// which found the same thing.
const recapStop = "Stop: this is a question about this session, not a continuation of the work above, " +
	"and any step it ends on is not to be carried out. Do not use any tools. "

// RecapPrompt is the one question a fork is asked. It is written for the
// person listening to the voice copilot on their phone, who asked for "what
// is it doing" and was given file names and test counts (2026-11-14).
const RecapPrompt = recapStop +
	"In one or two short sentences (at most 60 words), in the language the person has been using in this conversation, " +
	"tell someone coming back to it, who is not reading the code: what this session is working on, where it got to, " +
	"and whose move it is now (what the person needs to decide or do, if anything). " +
	"Use plain words a person would say out loud: no file names, commit hashes, function names, test counts or command lines. " +
	"Plain prose, no headings, no lists, no markdown."

// MaxRecapRunes bounds one recap as stored and shown; the question asks for
// sixty words, and an answer that ran on is cut, not refused.
const MaxRecapRunes = 400

// ErrNotARecap is a fork that answered with nothing a person could read.
var ErrNotARecap = errors.New("the fork did not answer with a recap")

// RecapTarget names the conversation a recap forks.
type RecapTarget struct {
	// Assistant is "claude" or "codex"; no other assistant can fork.
	Assistant string
	// Conversation is the assistant's own conversation id (a UUID).
	Conversation string
	// Dir is where the conversation was started. Claude Code finds a
	// conversation to resume by its project directory, so a Claude fork runs
	// there; a Codex fork runs in a scratch directory, since it has the
	// conversation and needs no files.
	Dir string
}

// RecapCan says whether an assistant's conversations can be recapped.
func RecapCan(assistant string) bool { return assistant == "claude" || assistant == "codex" }

// Recap forks the target's conversation and returns its answer to RecapPrompt.
func (p Planner) Recap(ctx context.Context, t RecapTarget) (string, error) {
	if !RecapCan(t.Assistant) || !safeConversationID(t.Conversation) {
		return "", ErrNoPlanner
	}
	executable := p.executable(t.Assistant)
	if executable == "" {
		return "", ErrNoPlanner
	}
	turn, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	if t.Assistant == "claude" {
		dir := t.Dir
		if dir == "" {
			return "", ErrNoPlanner
		}
		args := []string{"-p", "--resume", t.Conversation, "--fork-session", "--no-session-persistence",
			"--model", recapClaudeModel, "--effort", "low", "--tools", "",
			"--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--disable-slash-commands", "--output-format", "json"}
		raw, err := p.runner()(turn, executable, args, RecapPrompt, dir, nil)
		if err != nil {
			return "", failed(err, raw, stderrOf(err))
		}
		return recapFromClaude(raw)
	}
	dir, err := os.MkdirTemp(scratch(), "recap-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	answer := filepath.Join(dir, "recap.txt")
	// `codex exec fork` takes neither --sandbox nor -C nor --color (codex-cli
	// 0.166.0); the sandbox is set through its config key instead.
	args := []string{"exec", "fork", "--ephemeral", "--skip-git-repo-check",
		"--disable", "shell_tool", "--disable", "unified_exec",
		"-c", `sandbox_mode="read-only"`, "-c", `web_search="disabled"`, "-c", "agents.enabled=false",
		"-c", `approval_policy="never"`, "-c", `model_reasoning_effort="low"`,
		"-o", answer, t.Conversation, "-"}
	env := []string{}
	if p.Home != "" && os.Getenv("CODEX_HOME") == "" {
		env = append(env, "CODEX_HOME="+filepath.Join(p.Home, ".codex"))
	}
	out, err := p.runner()(turn, executable, args, RecapPrompt, dir, env)
	if err != nil {
		return "", failed(err, out, stderrOf(err))
	}
	raw, err := os.ReadFile(answer)
	if err != nil {
		return "", failed(ErrNotARecap, out)
	}
	return cleanRecap(string(raw))
}

// recapFromClaude reads Claude Code's JSON result. A result marked as an
// error, or one reached after more than one turn, is not an answer to the
// question: with tools off there is nothing a second turn could be.
func recapFromClaude(raw []byte) (string, error) {
	var v struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Turns   int    `json:"num_turns"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", failed(ErrNotARecap, raw)
	}
	if v.IsError || v.Turns > 1 {
		return "", failed(ErrNotARecap, []byte(v.Result))
	}
	return cleanRecap(v.Result)
}

// cleanRecap joins the answer's lines, drops markdown emphasis a model added
// anyway, and cuts it at MaxRecapRunes.
func cleanRecap(s string) (string, error) {
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || !utf8.ValidString(s) {
		return "", ErrNotARecap
	}
	if r := []rune(s); len(r) > MaxRecapRunes {
		s = strings.TrimSpace(string(r[:MaxRecapRunes-1])) + "…"
	}
	return s, nil
}

// safeConversationID admits what both CLIs name a conversation with: a UUID's
// characters only, so the id can never be read as a flag or a path.
func safeConversationID(id string) bool {
	if len(id) < 8 || len(id) > 64 || strings.HasPrefix(id, "-") {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r == '-') {
			return false
		}
	}
	return true
}
