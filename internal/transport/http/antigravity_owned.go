package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The indirection is installed before inventory captures its sources. Its
// default is a complete empty owned source; reading it never launches a CLI.
type ownedAntigravityTerminal struct{ server *Server }

func (p ownedAntigravityTerminal) Name() string { return "owned" }
func (p ownedAntigravityTerminal) Inventory(ctx context.Context) (session.Inventory, error) {
	inv := session.Inventory{Complete: true, Provenance: "owned", Sessions: []session.Session{}}
	for _, h := range p.server.ownedHosts() {
		rows, err := h.Inventory(ctx)
		if err != nil {
			return inv, err
		}
		inv.Complete = inv.Complete && rows.Complete
		inv.Sessions = append(inv.Sessions, rows.Sessions...)
	}
	return inv, nil
}
func (p ownedAntigravityTerminal) Open(ctx context.Context, r ports.OpenRequest) (session.Session, error) {
	if h := p.server.antigravitySessions.Load(); h != nil {
		return h.Open(ctx, r)
	}
	return session.Session{}, ownedAntigravityUnavailable()
}

// Prepare role files and shared memory through the same Starter as the other
// providers. No model input is sent while opening the blank conversation.
func (s *Server) openOwnedAntigravity(ctx context.Context, starter app.Starter, place projects.Place, persona, model string, addDirs []string) (session.Session, error) {
	return s.openOwnedAssistant(ctx, starter, place, persona, model, addDirs, projects.AssistantAntigravity)
}

func (s *Server) openOwnedAssistant(ctx context.Context, starter app.Starter, place projects.Place, persona, model string, addDirs []string, assistant string) (session.Session, error) {
	prepared, err := starter.PrepareOwnedContextFor(ctx, place, persona, assistant)
	if err != nil {
		return session.Session{}, err
	}
	h := s.ownedHost(session.Assistant(assistant))
	if h == nil {
		prepared.Workflow.Fail(ctx)
		if prepared.LaunchID != "" {
			_ = s.store.FailSquadLaunch(ctx, prepared.LaunchID)
		}
		return session.Session{}, ownedAntigravityUnavailable()
	}
	launchCtx := ownedContextFor(place, prepared, assistant)
	if assistant == projects.AssistantGrok && s.grokFullPermission() {
		// Frozen into this Session's launch context: a later change to the
		// setting applies to new Sessions, not to one already running.
		launchCtx.PermissionMode = "full"
	}
	if len(addDirs) > 0 {
		var extra strings.Builder
		extra.WriteString("\n\nAdditional accessible directories:\n")
		for _, d := range addDirs {
			extra.WriteString("- " + d + "\n")
		}
		launchCtx.Text += extra.String()
	}
	row, err := h.OpenWithModel(ctx, ports.OpenRequest{Cwd: place.Path, Name: place.Label}, launchCtx, model)
	if err != nil {
		prepared.Workflow.Fail(ctx)
		if prepared.LaunchID != "" {
			_ = s.store.FailSquadLaunch(ctx, prepared.LaunchID)
		}
		return session.Session{}, err
	}
	if prepared.LaunchID != "" {
		// Opening has already succeeded. A pending receipt is recovered by
		// the existing inventory observer; never invite a duplicate open.
		if err := squadfiles.RecordTerminal(prepared.Files, row.ID); err != nil {
			log.Print(ownedLabel(assistant) + ": squad terminal receipt pending")
		}
		if err := s.store.RecordSquadTerminal(ctx, prepared.LaunchID, row.ID); err != nil {
			log.Print(ownedLabel(assistant) + ": squad terminal binding pending")
		} else if err := s.store.BindSquadConversation(ctx, prepared.LaunchID, row.ConversationID); err != nil {
			log.Print(ownedLabel(assistant) + ": squad conversation binding pending")
		}
	}
	if err := prepared.Workflow.Bind(ctx, row.ID, row.ConversationID); err != nil {
		log.Print(ownedLabel(assistant) + ": workflow binding pending recovery")
	}
	return row, nil
}

// ownedInstructions adds the turn-end request to every owned conversation's
// launch context; terminal Gemini Sessions have no use for it.
func ownedInstructions(text string) string {
	if text == "" {
		return antigravitysession.TurnEndInstruction
	}
	return text + "\n\n" + antigravitysession.TurnEndInstruction
}

func ownedAntigravityContext(place projects.Place, prepared app.OwnedContext) antigravitysession.LaunchContext {
	return ownedContextFor(place, prepared, projects.AssistantAntigravity)
}

func ownedContextFor(place projects.Place, prepared app.OwnedContext, assistant string) antigravitysession.LaunchContext {
	resource := prepared.Context.PromptPath
	if resource == "" {
		// An inline resource can identify the existing Project directory. It
		// neither creates a Project file nor asks the provider to read one.
		resource = place.Path
	}
	text, reminder := prepared.Context.Instructions(), ""
	if assistant == projects.AssistantAntigravity {
		text = ownedInstructions(text)
		reminder = prepared.Context.TurnReminder()
	}
	return antigravitysession.LaunchContext{
		URI: (&url.URL{Scheme: "file", Path: resource}).String(), Text: text, Reminder: reminder,
		Persona: prepared.Persona, SquadLaunchID: prepared.LaunchID, SquadCapabilityPath: prepared.Files.CapabilityPath,
		WorkflowLaunchID: prepared.Workflow.ID,
	}
}

func (p ownedAntigravityTerminal) Send(ctx context.Context, s session.Session, text string) error {
	return p.SendProtocol(ctx, s, text, nil)
}
func (p ownedAntigravityTerminal) SendProtocol(ctx context.Context, s session.Session, text string, before func() error) error {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.SendProtocol(ctx, s, text, before)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) SendProtocolPictures(ctx context.Context, s session.Session, text string, pictures []string, before func() error) error {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.SendProtocolPictures(ctx, s, text, pictures, before)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) OwnsProtocol(s session.Session) bool {
	h := p.server.ownedHost(s.Assistant)
	return h != nil && h.OwnsProtocol(s)
}
func (p ownedAntigravityTerminal) SelectProtocol(ctx context.Context, s session.Session, digit int, expect string, before func() error) error {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.SelectProtocol(ctx, s, digit, expect, before)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Interrupt(ctx context.Context, s session.Session) error {
	return p.InterruptProtocol(ctx, s, nil)
}
func (p ownedAntigravityTerminal) InterruptProtocol(ctx context.Context, s session.Session, before func() error) error {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.InterruptProtocol(ctx, s, before)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Reveal(context.Context, session.Session, bool) error {
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) NativeTerminalIdentity(ctx context.Context, s session.Session) (string, error) {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.NativeTerminalIdentity(ctx, s)
	}
	return "", ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Keystroke(ctx context.Context, s session.Session, b []byte) error {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.Keystroke(ctx, s, b)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Screen(ctx context.Context, s session.Session, cols int) (string, bool) {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.Screen(ctx, s, cols)
	}
	return "", false
}
func (p ownedAntigravityTerminal) Close(ctx context.Context, s session.Session) error {
	if h := p.server.ownedHost(s.Assistant); h != nil {
		return h.Close(ctx, s)
	}
	return ownedAntigravityUnavailable()
}
func ownedAntigravityUnavailable() error {
	return &antigravitysession.Error{Code: "unavailable", Delivery: "not_written"}
}

// StartAntigravitySessions enables an explicitly configured local preview. It
// neither signs in nor starts a process. The daemon owns shutdown and joins all
// of its prompts before returning. No global account or Cloud entitlement is added.
func (s *Server) StartAntigravitySessions(ctx context.Context) (func(), error) {
	f, err := os.Open(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"))
	if errors.Is(err, os.ErrNotExist) {
		return func() {}, nil
	}
	if err != nil {
		return nil, ownedAntigravityUnavailable()
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, antigravitysession.MaxFrameBytes+1))
	if err != nil || len(raw) > antigravitysession.MaxFrameBytes {
		return nil, ownedAntigravityUnavailable()
	}
	var cfg struct {
		Executable string   `json:"executable"`
		AccountDir string   `json:"accountDir"`
		Args       []string `json:"args"`
		RawTrace   bool     `json:"rawTrace"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil, ownedAntigravityUnavailable()
	}
	// Keep the exact configured profile: replacing a missing account with a
	// different home would hide its conversations and change the login identity.
	if st, err := os.Stat(cfg.AccountDir); err != nil || !st.IsDir() {
		return nil, ownedAntigravityUnavailable()
	}
	if err := s.resolveOwnedAntigravityExecutable(ctx, &cfg.Executable, &cfg.Args); err != nil {
		return nil, err
	}
	h, err := antigravitysession.New(ctx, antigravitysession.Options{Executable: cfg.Executable, AccountDir: cfg.AccountDir, Args: cfg.Args, StateDir: filepath.Join(s.cfg.Dir, "antigravity-sessions"), NewSessionMode: "yolo", RawTrace: cfg.RawTrace, TurnComplete: s.automaticOwnedTitle, PromptAccepted: s.automaticOwnedTitle})
	if err != nil {
		return nil, err
	}
	if !s.antigravitySessions.CompareAndSwap(nil, h) {
		_ = h.Shutdown()
		return nil, ownedAntigravityUnavailable()
	}
	if n := h.ContinueInterrupted(ctx); n > 0 {
		log.Printf("antigravity: continuing %d conversation(s) a restart interrupted", n)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := h.Shutdown(); err != nil {
				log.Print("antigravity: owned_session_cleanup_failed")
			}
			s.antigravitySessions.CompareAndSwap(h, nil)
		})
	}, nil
}

// Older pilots pinned an extracted server inside a disposable evaluation tree.
// Recover only that known default, using the installed CLI's public ACP entry
// point. Custom executables, arguments and profiles are never silently replaced.
func (s *Server) resolveOwnedAntigravityExecutable(ctx context.Context, executable *string, args *[]string) error {
	st, err := os.Stat(*executable)
	if errors.Is(err, os.ErrNotExist) && filepath.IsAbs(*executable) && filepath.Base(*executable) == "agy_acp_server.par" && len(*args) == 0 {
		installation := s.antigravityInstallation
		if installation == nil {
			installation = func(ctx context.Context) antigravity.Installation { return antigravityResolver().Snapshot(ctx) }
		}
		info := installation(ctx)
		if !info.InstalledKnown || !info.Installed || !info.Compatible || !filepath.IsAbs(info.Path) {
			return ownedAntigravityUnavailable()
		}
		*executable = info.Path
		*args = []string{"acp", "--app-data-dir", "antigravity-acp"}
		st, err = os.Stat(*executable)
	}
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
		return ownedAntigravityUnavailable()
	}
	return nil
}

func (s *Server) availableAssistants() []contract.StartAssistant {
	rows := installedAssistants()
	for _, assistant := range []session.Assistant{session.AssistantAntigravity, session.AssistantGrok} {
		if h := s.ownedHost(assistant); h != nil {
			for n := range rows {
				if rows[n].ID != string(assistant) {
					continue
				}
				if assistant == session.AssistantGrok && (rows[n].Installed == nil || !*rows[n].Installed || rows[n].Version == "") {
					continue
				}
				installed := true
				rows[n].Installed = &installed
				rows[n].IntegrationState = contract.StartIntegrationStateActive
				rows[n].AuthState = contract.StartAuthStateUnknown
				count, _ := h.Stats()
				if count > 0 && assistant == session.AssistantAntigravity {
					rows[n].AuthState = contract.StartAuthStateReady
				}
				rows[n].Capabilities = &contract.StartCapabilities{Roles: true, Queue: true, Start: true, Resume: true, Send: count > 0, Menus: count > 0, Transcript: count > 0}
			}
		}
	}
	return rows
}

// The broker has already published its immutable role snapshot with the task's
// claims. Reuse it and configure the native worker before its one first input.
func (s *Server) openOwnedAntigravityChild(ctx context.Context, r orchestrator.Record, cwd, launchID string, files squadfiles.Files, workflow workflowlaunch.Prepared) (orchestrator.OwnedChild, error) {
	h := s.ownedHost(session.Assistant(r.Assistant))
	if h == nil {
		return orchestrator.OwnedChild{}, ownedAntigravityUnavailable()
	}
	starter := app.Starter{Memory: s.memoryLaunchText, Language: func(assistant string) string {
		if s.broker == nil {
			return ""
		}
		return s.broker.SessionLanguage(assistant)
	}, GeminiStepExplanations: func() bool {
		values, err := nextconfig.Open(s.cfg.Dir).Read()
		if err != nil {
			return true
		}
		return values.GeminiStepExplanations()
	}}
	place := projects.Place{Path: cwd, Label: r.Title}
	prepared, err := starter.OwnedContextFromPublishedFor(ctx, place, r.Persona, launchID, files, r.Assistant)
	if err != nil {
		return orchestrator.OwnedChild{}, err
	}
	prepared, err = prepared.WithWorkflow(workflow)
	if err != nil {
		return orchestrator.OwnedChild{}, err
	}
	launch := ownedContextFor(place, prepared, r.Assistant)
	if r.Assistant == projects.AssistantGrok {
		launch.PermissionMode = r.PermissionMode
	}
	row, err := h.OpenWithContext(ctx, ports.OpenRequest{Cwd: cwd, Name: r.Title}, launch)
	if err != nil {
		return orchestrator.OwnedChild{}, err
	}
	result := orchestrator.OwnedChild{Row: row}
	if err := h.ConfigureDispatch(ctx, row, r.Model, r.ReasoningEffort, r.PermissionMode); err != nil {
		return result, err
	}
	facts, err := h.Facts(row)
	result.Model, result.ReasoningEffort = facts.Model, facts.Effort
	return result, err
}

func (s *Server) stopOwnedAntigravityChild(ctx context.Context, id, native, cwd string) error {
	for _, assistant := range []session.Assistant{session.AssistantAntigravity, session.AssistantGrok} {
		h := s.ownedHost(assistant)
		if h == nil {
			continue
		}
		err := h.Stop(ctx, session.Session{ID: id, Backend: session.BackendOwned, Assistant: assistant, Binding: session.BindingProtocol, ConversationID: native, CWD: cwd})
		if err == nil {
			return nil
		}
		var local *antigravitysession.Error
		if !errors.As(err, &local) || (local.Code != "not_found" && local.Code != "identity_changed") {
			return err
		}
	}
	return ownedAntigravityUnavailable()
}

// grokFullPermission reads the person's machine setting. An unreadable
// settings file keeps the supervised default rather than granting more.
func (s *Server) grokFullPermission() bool {
	values, err := s.settingsFile().Read()
	return err == nil && values.GrokFullPermission()
}
