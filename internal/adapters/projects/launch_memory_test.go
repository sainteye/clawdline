package projects

import (
	"reflect"
	"strings"
	"testing"
)

const inventedMemory = "This Project's shared memory index is below.\n\n# Project memory index\n\n## feedback\n\n- it's-invented — a lesson nobody learned\n"

// A Project with no memory launches with exactly the arguments it had before
// shared memory existed, for both assistants, with and without a role.
func TestALaunchWithoutMemoryIsUnchanged(t *testing.T) {
	squad := "/private/state/squad/launches/abcdefghijklmnopqrstuv/prompt.md"
	for _, tc := range []struct {
		req  LaunchRequest
		want []string
	}{
		{LaunchRequest{ProjectRoot: "/p", Assistant: AssistantClaude}, nil},
		{LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex}, nil},
		{LaunchRequest{ProjectRoot: "/p", Assistant: AssistantClaude, Model: "opus", SquadPromptPath: squad},
			[]string{"--model", "opus", "--append-system-prompt-file", "'" + squad + "'"}},
		{LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex, Model: "opus"}, []string{"--model", "opus"}},
	} {
		got, err := Admit(tc.req)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Arguments, tc.want) {
			t.Errorf("%s %+v: %q, want %q", tc.req.Assistant, tc.req, got.Arguments, tc.want)
		}
	}
}

// Claude Code is given the memory as one inline system-prompt argument after
// everything it had, and keeps its role file: a second
// --append-system-prompt-file would have replaced the first.
func TestClaudeIsGivenMemoryBesideItsRoleFile(t *testing.T) {
	squad := "/private/state/squad/launches/abcdefghijklmnopqrstuv/prompt.md"
	base := LaunchRequest{ProjectRoot: "/p", Assistant: AssistantClaude, Model: "opus", SquadPromptPath: squad}
	without, err := Admit(base)
	if err != nil {
		t.Fatal(err)
	}
	with := base
	with.Memory = inventedMemory
	got, err := Admit(with)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]string{}, without.Arguments...), "--append-system-prompt", ShellQuoted(inventedMemory))
	if !reflect.DeepEqual(got.Arguments, want) {
		t.Fatalf("arguments %q\nwant %q", got.Arguments, want)
	}
	if strings.Count(strings.Join(got.Arguments, " "), "--append-system-prompt-file") != 1 {
		t.Fatalf("not exactly one role file: %q", got.Arguments)
	}
}

// Codex is given the memory inside its one developer_instructions value,
// after the role and before the Note instruction, so nothing has to be read
// from a file; a Codex with no role gets a developer_instructions holding the
// memory and the Note instruction only.
func TestCodexIsGivenMemoryInItsDeveloperInstructions(t *testing.T) {
	squad := "/private/state/squad/launches/abcdefghijklmnopqrstuv/prompt.md"
	plain, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex, Memory: inventedMemory})
	if err != nil {
		t.Fatal(err)
	}
	if want := codexDeveloperArgs(inventedMemory, codexNoteInstruction); !reflect.DeepEqual(plain.Arguments, want) {
		t.Fatalf("plain codex %q\nwant %q", plain.Arguments, want)
	}
	role, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantCodex, SquadPromptPath: squad, Memory: inventedMemory})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(role.Arguments, " ")
	if strings.Count(line, "developer_instructions=") != 1 || strings.Contains(line, "--append-system-prompt") {
		t.Fatalf("codex arguments %q", role.Arguments)
	}
	value := role.Arguments[len(role.Arguments)-1]
	at := func(s string) int { return strings.Index(value, s) }
	if at(squad) < 0 || at(squad) > at("lesson nobody learned") || at("lesson nobody learned") > at("clawdline guide note") {
		t.Fatalf("role, memory and Note instruction are not in that order: %s", value)
	}
}

func TestALaunchRefusesMemoryPastWhatItCarries(t *testing.T) {
	for _, assistant := range []string{AssistantClaude, AssistantCodex} {
		_, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: assistant, Memory: strings.Repeat("m", MemoryLaunchLimit+1)})
		if err == nil {
			t.Fatalf("%s admitted %d bytes of memory", assistant, MemoryLaunchLimit+1)
		}
	}
	if args := MemoryArgs(AssistantCodex, inventedMemory); args != nil {
		t.Fatalf("Codex was given a Claude flag: %q", args)
	}
}
