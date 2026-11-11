package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// errTurnTimeout is a turn that did not answer within its timeout; each
// caller names it in its own terms (ErrPolishTimeout, ErrBriefingTimeout).
var errTurnTimeout = errors.New("turn timed out")

// runTurn runs one tool-less turn with the named assistant's smallest model and
// returns the JSON object it answered with. Claude is held to the schema by its
// own flag; Codex writes its last message to a file in a throwaway directory.
// Neither may use tools, MCP servers, rules or the person's configuration.
func (p Planner) runTurn(ctx context.Context, assistant, system, input, schema string, timeout time.Duration, answerName string) ([]byte, error) {
	executable := p.executable(assistant)
	if executable == "" {
		return nil, ErrNoPlanner
	}
	if assistant == "claude" {
		args := []string{"-p", "--model", polishClaudeModel, "--effort", "low", "--system-prompt", system,
			"--output-format", "json", "--json-schema", schema, "--tools", "",
			"--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--setting-sources", "", "--no-session-persistence", "--disable-slash-commands"}
		turn, cancel := context.WithTimeout(ctx, timeout)
		raw, err := p.runner()(turn, executable, args, input, scratch(), nil)
		late := turn.Err() != nil && ctx.Err() == nil
		cancel()
		if err != nil && late {
			return nil, errTurnTimeout
		}
		if err != nil {
			return nil, failed(err, raw, stderrOf(err))
		}
		object, ok := objectFromClaude(raw)
		if !ok {
			return nil, failed(errors.New("the turn did not return an object"), raw)
		}
		return object, nil
	}
	dir, err := os.MkdirTemp("", "clawdline-turn-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	answer := filepath.Join(dir, answerName)
	args := []string{"exec", "--ephemeral", "--ignore-user-config", "--ignore-rules",
		"--skip-git-repo-check", "--sandbox", "read-only", "--disable", "shell_tool",
		"--disable", "unified_exec", "-c", `web_search="disabled"`, "-c", "agents.enabled=false",
		"-c", `approval_policy="never"`, "-c", `model_reasoning_effort="low"`,
		"-m", polishCodexModel, "--color", "never", "-C", dir, "-o", answer, "-"}
	env := []string{}
	if p.Home != "" && os.Getenv("CODEX_HOME") == "" {
		env = append(env, "CODEX_HOME="+filepath.Join(p.Home, ".codex"))
	}
	turn, cancel := context.WithTimeout(ctx, timeout)
	out, runErr := p.runner()(turn, executable, args, system+"\n\n"+input, dir, env)
	late := turn.Err() != nil && ctx.Err() == nil
	cancel()
	if runErr != nil && late {
		return nil, errTurnTimeout
	}
	if runErr != nil {
		return nil, failed(runErr, out, stderrOf(runErr))
	}
	raw, err := os.ReadFile(answer)
	if err != nil {
		return nil, err
	}
	object, ok := objectFromText(raw)
	if !ok {
		return nil, errors.New("the turn did not return an object")
	}
	return object, nil
}
