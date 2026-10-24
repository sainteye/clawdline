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
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The indirection is installed before inventory captures its sources. Its
// default is a complete empty owned source; reading it never launches a CLI.
type ownedAntigravityTerminal struct{ server *Server }

func (p ownedAntigravityTerminal) Name() string { return "owned" }
func (p ownedAntigravityTerminal) Inventory(ctx context.Context) (session.Inventory, error) {
	if h := p.server.antigravitySessions.Load(); h != nil {
		return h.Inventory(ctx)
	}
	return session.Inventory{Complete: true, Provenance: "owned", Sessions: []session.Session{}}, nil
}
func (p ownedAntigravityTerminal) Open(ctx context.Context, r ports.OpenRequest) (session.Session, error) {
	if h := p.server.antigravitySessions.Load(); h != nil {
		return h.Open(ctx, r)
	}
	return session.Session{}, ownedAntigravityUnavailable()
}

// Prepare role files and shared memory through the same Starter as the other
// providers. No model input is sent while opening the blank conversation.
func (s *Server) openOwnedAntigravity(ctx context.Context, starter app.Starter, place projects.Place, persona string) (session.Session, error) {
	prepared, err := starter.PrepareOwnedContext(ctx, place, persona)
	if err != nil {
		return session.Session{}, err
	}
	h := s.antigravitySessions.Load()
	if h == nil {
		if prepared.LaunchID != "" {
			_ = s.store.FailSquadLaunch(ctx, prepared.LaunchID)
		}
		return session.Session{}, ownedAntigravityUnavailable()
	}
	row, err := h.OpenWithContext(ctx, ports.OpenRequest{Cwd: place.Path, Name: place.Label}, ownedAntigravityContext(place, prepared))
	if err != nil {
		if prepared.LaunchID != "" {
			_ = s.store.FailSquadLaunch(ctx, prepared.LaunchID)
		}
		return session.Session{}, err
	}
	if prepared.LaunchID != "" {
		// Opening has already succeeded. A pending receipt is recovered by
		// the existing inventory observer; never invite a duplicate open.
		if err := squadfiles.RecordTerminal(prepared.Files, row.ID); err != nil {
			log.Print("gemini: squad terminal receipt pending")
		}
		if err := s.store.RecordSquadTerminal(ctx, prepared.LaunchID, row.ID); err != nil {
			log.Print("gemini: squad terminal binding pending")
		} else if err := s.store.BindSquadConversation(ctx, prepared.LaunchID, row.ConversationID); err != nil {
			log.Print("gemini: squad conversation binding pending")
		}
	}
	return row, nil
}
func ownedAntigravityContext(place projects.Place, prepared app.OwnedContext) antigravitysession.LaunchContext {
	resource := prepared.Context.PromptPath
	if resource == "" {
		// An inline resource can identify the existing Project directory. It
		// neither creates a Project file nor asks the provider to read one.
		resource = place.Path
	}
	return antigravitysession.LaunchContext{
		URI: (&url.URL{Scheme: "file", Path: resource}).String(), Text: prepared.Context.Instructions(),
		Persona: prepared.Persona, SquadLaunchID: prepared.LaunchID, SquadCapabilityPath: prepared.Files.CapabilityPath,
	}
}

func (p ownedAntigravityTerminal) Send(ctx context.Context, s session.Session, text string) error {
	return p.SendProtocol(ctx, s, text, nil)
}
func (p ownedAntigravityTerminal) SendProtocol(ctx context.Context, s session.Session, text string, before func() error) error {
	if h := p.server.antigravitySessions.Load(); h != nil {
		return h.SendProtocol(ctx, s, text, before)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) OwnsProtocol(s session.Session) bool {
	h := p.server.antigravitySessions.Load()
	return h != nil && h.OwnsProtocol(s)
}
func (p ownedAntigravityTerminal) SelectProtocol(ctx context.Context, s session.Session, digit int, expect string, before func() error) error {
	if h := p.server.antigravitySessions.Load(); h != nil {
		return h.SelectProtocol(ctx, s, digit, expect, before)
	}
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Interrupt(context.Context, session.Session) error {
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Reveal(context.Context, session.Session, bool) error {
	return ownedAntigravityUnavailable()
}
func (p ownedAntigravityTerminal) Screen(context.Context, session.Session, int) (string, bool) {
	return "", false
}
func (p ownedAntigravityTerminal) Close(ctx context.Context, s session.Session) error {
	if h := p.server.antigravitySessions.Load(); h != nil {
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
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil, ownedAntigravityUnavailable()
	}
	if st, err := os.Stat(cfg.Executable); err != nil || st.IsDir() || st.Mode()&0111 == 0 {
		return nil, ownedAntigravityUnavailable()
	}
	h, err := antigravitysession.New(ctx, antigravitysession.Options{Executable: cfg.Executable, AccountDir: cfg.AccountDir, Args: cfg.Args, StateDir: filepath.Join(s.cfg.Dir, "antigravity-sessions")})
	if err != nil {
		return nil, err
	}
	if !s.antigravitySessions.CompareAndSwap(nil, h) {
		_ = h.Shutdown()
		return nil, ownedAntigravityUnavailable()
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

func (s *Server) availableAssistants() []contract.StartAssistant {
	rows := installedAssistants()
	if h := s.antigravitySessions.Load(); h != nil {
		for n := range rows {
			if rows[n].ID != "antigravity" {
				continue
			}
			installed := true
			rows[n].Installed = &installed
			rows[n].IntegrationState = contract.StartIntegrationStateActive
			rows[n].AuthState = contract.StartAuthStateUnknown
			count, _ := h.Stats()
			if count > 0 {
				rows[n].AuthState = contract.StartAuthStateReady
			}
			rows[n].Capabilities = &contract.StartCapabilities{Roles: true, Start: true, Resume: true, Send: count > 0, Menus: count > 0, Transcript: count > 0}
		}
	}
	return rows
}
