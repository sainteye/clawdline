package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// Terminal operations are synthetic; this test never launches the vendor CLI,
// sends model input, or invokes Apple Events.
type antigravityLaunchTerminal struct {
	personaTerminal
	iterm   bool
	reach   projects.TmuxReach
	opens   int
	openErr error
}

func (f *antigravityLaunchTerminal) ITermRunning(context.Context) (bool, error) { return f.iterm, nil }
func (f *antigravityLaunchTerminal) TmuxReach(context.Context) int              { return int(f.reach) }
func (f *antigravityLaunchTerminal) open(command, id string) (string, error) {
	f.opens++
	f.command = command
	return id, f.openErr
}
func (f *antigravityLaunchTerminal) NewITermTab(_ context.Context, command string) (string, error) {
	return f.open(command, "synthetic-tab")
}
func (f *antigravityLaunchTerminal) NewTmuxWindow(_ context.Context, _, command string) (string, error) {
	return f.open(command, "%70")
}
func (f *antigravityLaunchTerminal) NewTmuxSession(_ context.Context, _, _, command string) (string, error) {
	return f.open(command, "%71")
}

// Refuse the optional viewer before it can touch the launch counter or command.
func (f *antigravityLaunchTerminal) PrepareTmuxViewer(context.Context, string) (string, error) {
	return "", errors.New("synthetic viewer disabled")
}

func TestAntigravityAttachesEveryOpenedTerminalAndKeepsUncertainBinding(t *testing.T) {
	for _, role := range []bool{false, true} {
		for _, tc := range []struct {
			name        string
			choice      projects.TerminalChoice
			iterm       bool
			reach       projects.TmuxReach
			id, backend string
		}{
			{"window", projects.TerminalTmux, false, projects.TmuxRunning, "%70", "tmux"},
			{"detached", projects.TerminalTmux, false, projects.TmuxInstalled, "%71", "tmux"},
			{"native", projects.TerminalITermNative, true, projects.TmuxRunning, "synthetic-tab", "iterm"},
			{"viewer", projects.TerminalITerm, true, projects.TmuxRunning, "%71", "tmux"},
		} {
			for _, failBind := range []bool{false, true} {
				name := tc.name + "/role=" + map[bool]string{true: "yes", false: "no"}[role] + "/pending=" + map[bool]string{true: "yes", false: "no"}[failBind]
				t.Run(name, func(t *testing.T) {
					term := &antigravityLaunchTerminal{iterm: tc.iterm, reach: tc.reach}
					attaches := 0
					s := Starter{Terminal: func() projects.TerminalChoice { return tc.choice }, Launcher: term}
					persona := ""
					if role {
						st, err := store.Open(t.TempDir())
						if err != nil {
							t.Fatal(err)
						}
						defer st.Close()
						s.Squad = st
						s.SquadDir = t.TempDir()
						s.ResolveSquadSnapshot = syntheticAntigravityRole
						persona = "backend"
					}
					s.PrepareAntigravity = func(_ context.Context, r projects.LaunchRequest) (PreparedAntigravity, error) {
						prompt := ""
						if r.SquadPromptPath != "" {
							b, err := os.ReadFile(r.SquadPromptPath)
							if err != nil {
								return PreparedAntigravity{}, err
							}
							prompt = string(b)
						}
						c, err := projects.PrepareAntigravityContext(r.SquadPromptPath, prompt, r.Memory, r.Language)
						r.AntigravityContext = &c
						r.Executable = "/validated/standalone"
						r.PermissionMode = "plan"
						p := preparedAntigravityForTest(r, nil)
						p.AttachTerminal = func(_ context.Context, backend, id string) error {
							attaches++
							if term.opens != 1 || id != tc.id || backend != tc.backend {
								t.Fatalf("binding wrong terminal: opens=%d backend=%s id=%s", term.opens, backend, id)
							}
							if failBind {
								return errors.New("synthetic index write failed")
							}
							return nil
						}
						return p, err
					}
					got, err := s.Start(context.Background(), projects.Place{Path: t.TempDir()}, projects.AssistantAntigravity, "", "", persona)
					if err != nil || got.ID != tc.id || got.Backend != tc.backend || attaches != 1 || term.opens != 1 || got.BindingPending != failBind {
						t.Fatalf("opened outcome: %+v err=%v opens=%d attaches=%d", got, err, term.opens, attaches)
					}
				})
			}
		}
	}
}

func syntheticAntigravityRole(context.Context, string, string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"definition_id": "clawdline.persona.backend", "scope_id": "synthetic-project",
		"definition": map[string]any{"version": "1", "body": "Original synthetic role"},
		"skills":     []any{map[string]any{"id": "synthetic.skill", "version": "1", "enabled": true, "content": "Original synthetic skill"}},
	})
}

func TestAntigravityResumeRestoresBeforePublishingAndRetainsOriginalSkillPaths(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("private context owner requires Unix")
	}
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	squadStore, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer squadStore.Close()
	native, err := antigravitylaunch.Open(antigravitylaunch.Config{StateDir: filepath.Join(root, "antigravity"), SquadLaunchRoot: filepath.Join(root, "squad", "launches")})
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	place := projects.Place{Path: root}
	term := &antigravityLaunchTerminal{reach: projects.TmuxRunning}
	var preparations []antigravitylaunch.Prepared
	resolves, mutableReads := 0, 0
	s := Starter{Squad: squadStore, SquadDir: root, Launcher: term, Terminal: func() projects.TerminalChoice { return projects.TerminalTmux },
		ResolveSquadSnapshot: func(c context.Context, role, path string) (json.RawMessage, error) {
			resolves++
			return syntheticAntigravityRole(c, role, path)
		},
		Memory:   func(string) string { mutableReads++; return "Original synthetic memory" },
		Language: func(string) string { mutableReads++; return "zh-Hant-TW" },
	}
	s.PrepareAntigravity = func(c context.Context, r projects.LaunchRequest) (PreparedAntigravity, error) {
		r.Executable = "/validated/standalone"
		r.PermissionMode = "plan"
		p, err := native.Prepare(c, r)
		if err != nil {
			return PreparedAntigravity{}, err
		}
		preparations = append(preparations, p)
		return PreparedAntigravity{Request: p.Request, PrivateEnv: p.PrivateEnv, AttachTerminal: func(c context.Context, backend, id string) error {
			return native.AttachTerminal(c, p.Record.ID, backend, id)
		}}, nil
	}
	if _, err = s.Start(ctx, place, projects.AssistantAntigravity, "", "", "backend"); err != nil {
		t.Fatal(err)
	}
	first := preparations[0]
	const conversation = "0f1e2d3c-0000-4000-8000-000000000097"
	process := ports.AntigravityProcess{PID: 1234, ParentPID: 123, Birth: "2026-10-22T10:00:00.123456Z", Executable: first.Request.Executable, TTY: "ttysynthetic", TerminalID: "%70", BootID: "synthetic-boot", KernelGeneration: "synthetic-exec-1"}
	if err = native.PinProcess(ctx, first.Record.ID, process); err != nil {
		t.Fatal(err)
	}
	if err = native.RecordBinding(ctx, first.Record.ID, ports.AntigravityBinding{Process: process, ConversationID: conversation, TranscriptPath: "/synthetic/native/log.jsonl", Event: "PreInvocation", Sequence: 1, Verified: true, ObservedAt: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)}, antigravitylaunch.KeepConversation); err != nil {
		t.Fatal(err)
	}
	launches, err := squadStore.PendingSquadLaunches(ctx)
	if err != nil || len(launches) != 1 {
		t.Fatalf("pending: %+v %v", launches, err)
	}
	if err = squadStore.BindSquadConversation(ctx, launches[0].ID, conversation); err != nil {
		t.Fatal(err)
	}
	if err = native.Deactivate(ctx, first.Record.ID); err != nil {
		t.Fatal(err)
	}
	original := *first.Request.AntigravityContext
	s.Memory = func(string) string { t.Fatal("resume read current memory"); return "" }
	s.Language = func(string) string { t.Fatal("resume read current language"); return "" }
	s.ResolveSquadSnapshot = func(context.Context, string, string) (json.RawMessage, error) {
		t.Fatal("resume read current role")
		return nil, nil
	}
	s.RestoreAntigravity = func(c context.Context, path, id string) (projects.AntigravityContext, error) {
		// A refused restore cannot create another Squad launch or its skill paths.
		entries, err := os.ReadDir(filepath.Join(root, "squad", "launches"))
		if err != nil || len(entries) != 1 {
			t.Fatalf("restore came after publication: %d %v", len(entries), err)
		}
		return native.Restore(c, path, id)
	}
	if _, err = s.Start(ctx, place, projects.AssistantAntigravity, "", "0f1e2d3c-0000-4000-8000-000000000096", "backend"); err == nil {
		t.Fatal("unknown native history resumed")
	}
	if _, err = s.Start(ctx, place, projects.AssistantAntigravity, "", conversation, "architect"); err == nil {
		t.Fatal("resume replaced the original role")
	}
	if term.opens != 1 || len(preparations) != 1 {
		t.Fatal("refused restore opened a terminal or prepared another intent")
	}
	if _, err = s.Start(ctx, place, projects.AssistantAntigravity, "", conversation, "backend"); err != nil {
		t.Fatal(err)
	}
	second := preparations[1]
	if second.Record.ID == first.Record.ID || *second.Request.AntigravityContext != original || resolves != 1 || mutableReads != 2 || !strings.Contains(original.Prompt, filepath.Join(root, "squad", "launches", launches[0].ID, "skills")) {
		t.Fatal("resume changed immutable role, memory, language or skill paths")
	}
	if second.Record.TerminalID != "" {
		t.Fatal("prepared value unexpectedly acquired process authority")
	}
	// A second Squad capability belongs to this new terminal, while the injected
	// context continues to name the first launch's skill files.
	entries, err := os.ReadDir(filepath.Join(root, "squad", "launches"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("new capability not isolated: %d %v", len(entries), err)
	}
}

func TestAntigravityPreparationFailureNeverOpensOrAttaches(t *testing.T) {
	for _, kind := range []string{"missing-attach", "changed-memory", "changed-model", "changed-project", "open-failed", "resume-unavailable"} {
		t.Run(kind, func(t *testing.T) {
			term := &antigravityLaunchTerminal{reach: projects.TmuxRunning}
			attached := false
			s := Starter{Launcher: term, Terminal: func() projects.TerminalChoice { return projects.TerminalTmux }}
			s.PrepareAntigravity = func(_ context.Context, r projects.LaunchRequest) (PreparedAntigravity, error) {
				c, _ := projects.PrepareAntigravityContext("", "", r.Memory, r.Language)
				r.AntigravityContext = &c
				r.Executable = "/validated/standalone"
				r.PermissionMode = "plan"
				p := preparedAntigravityForTest(r, nil)
				p.AttachTerminal = func(context.Context, string, string) error { attached = true; return nil }
				switch kind {
				case "missing-attach":
					p.AttachTerminal = nil
				case "changed-memory":
					p.Request.Memory = "mutated"
				case "changed-model":
					p.Request.Model = "gemini-3.8-flash"
				case "changed-project":
					p.Request.ProjectRoot = "/other"
				}
				return p, nil
			}
			resume := ""
			if kind == "open-failed" {
				term.openErr = errors.New("synthetic open refused")
			}
			if kind == "resume-unavailable" {
				resume = "0f1e2d3c-0000-4000-8000-000000000098"
			}
			_, err := s.Start(context.Background(), projects.Place{Path: t.TempDir()}, projects.AssistantAntigravity, "", resume, "")
			wantOpens := 0
			if kind == "open-failed" {
				wantOpens = 1
			}
			if err == nil || attached || term.opens != wantOpens {
				t.Fatalf("failure admitted: %v opens=%d attached=%v", err, term.opens, attached)
			}
		})
	}
}
