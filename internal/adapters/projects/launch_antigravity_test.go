package projects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func antigravityRequest() LaunchRequest {
	return LaunchRequest{ProjectRoot: "/project", Assistant: AssistantAntigravity,
		Executable: "/validated/standalone agy", PermissionMode: "plan"}
}

func TestAntigravityExactNewAndResumeArguments(t *testing.T) {
	if !KnownAssistant(AssistantAntigravity) || KnownAssistant("agy") || KnownAssistant("gemini") {
		t.Fatal("provider vocabulary")
	}
	for _, resume := range []string{"", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"} {
		for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
			for _, mode := range []string{"plan", "accept-edits", "auto"} {
				req := antigravityRequest()
				req.Model, req.Resume, req.ReasoningEffort, req.PermissionMode = "gemini-3-pro", resume, effort, mode
				got, err := Admit(req)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{}
				if resume != "" {
					want = append(want, "--conversation", resume)
				}
				want = append(want, "--model", req.Model)
				if effort != "" {
					want = append(want, "--effort", effort)
				}
				want = append(want, "--mode", mode)
				if !reflect.DeepEqual(got.Arguments, want) {
					t.Fatalf("%v want %v", got.Arguments, want)
				}
				line := got.ShellCommand()
				if !strings.Contains(line, "'/validated/standalone agy'") || strings.Contains(line, "PATH=") || strings.Contains(line, "--continue") || strings.Contains(line, "skip-permissions") {
					t.Fatal(line)
				}
			}
		}
	}
}

func TestAntigravityRejectsUnmappedPermissionsAndProviderOnlyFields(t *testing.T) {
	for _, mode := range []string{"", "ask", "edits", "full", "--dangerously-skip-permissions", "plan;echo wrong"} {
		req := antigravityRequest()
		req.PermissionMode = mode
		if _, err := Admit(req); !errors.Is(err, ErrProviderPermissionUnsupported) {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
	for _, field := range []string{"language", "persona", "personaDir", "squad", "memory"} {
		req := antigravityRequest()
		switch field {
		case "language":
			req.Language = "zh-Hant-TW"
		case "persona":
			req.Persona = "backend"
		case "personaDir":
			req.PersonaDir = "/p"
		case "squad":
			req.SquadPromptPath = "/p"
		case "memory":
			req.Memory = "read memory"
		}
		if _, err := Admit(req); !errors.Is(err, ErrProviderOptionsUnsupported) {
			t.Fatalf("%s: %v", field, err)
		}
	}
	for _, executable := range []string{"", "agy", "/tmp/agy\nwrong"} {
		req := antigravityRequest()
		req.Executable = executable
		if _, err := Admit(req); !errors.Is(err, ErrProviderExecutableUnavailable) {
			t.Fatalf("path %q: %v", executable, err)
		}
	}
	for _, mutate := range []func(*LaunchRequest){
		func(r *LaunchRequest) { r.ReasoningEffort = "ultra" },
		func(r *LaunchRequest) { r.Resume = "latest" },
		func(r *LaunchRequest) { r.Model = "-unsafe" },
	} {
		req := antigravityRequest()
		mutate(&req)
		if _, err := Admit(req); !errors.Is(err, ErrInvalidLaunch) {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	for _, assistant := range []string{AssistantClaude, AssistantCodex} {
		req := antigravityRequest()
		req.Assistant = assistant
		if _, err := Admit(req); !errors.Is(err, ErrProviderOptionsUnsupported) {
			t.Fatalf("%s: %v", assistant, err)
		}
	}
}

func TestAntigravityShellExecutesTheAbsoluteQuotedExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launch")
	}
	path := filepath.Join(t.TempDir(), "agy ' standalone")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	request := antigravityRequest()
	request.Executable = path
	launch, err := Admit(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := exec.CommandContext(context.Background(), "/bin/sh", "-c", launch.ShellCommand()).CombinedOutput()
	if err != nil || string(got) != "--mode\nplan\n" {
		t.Fatalf("%q %v", got, err)
	}
}
