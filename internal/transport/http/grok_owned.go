package http

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/grok"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

var grokResolver = sync.OnceValue(func() *antigravity.Resolver { return grok.NewResolver(grok.DefaultCandidates(), nil) })

func (s *Server) ownedHost(assistant session.Assistant) *antigravitysession.Host {
	switch assistant {
	case session.AssistantAntigravity:
		return s.antigravitySessions.Load()
	case session.AssistantGrok:
		return s.grokSessions.Load()
	default:
		return nil
	}
}

func (s *Server) ownedHosts() []*antigravitysession.Host {
	var hosts []*antigravitysession.Host
	if h := s.antigravitySessions.Load(); h != nil {
		hosts = append(hosts, h)
	}
	if h := s.grokSessions.Load(); h != nil {
		hosts = append(hosts, h)
	}
	return hosts
}

func grokStartAssistant(info antigravity.Installation) contract.StartAssistant {
	state := contract.StartIntegrationStateMissing
	if info.Installed && info.Reason == "provider_cli_incompatible" {
		state = contract.StartIntegrationStateIncompatible
	}
	var installed *bool
	if info.InstalledKnown {
		installed = &info.Installed
	}
	return contract.StartAssistant{ID: "grok", Label: "Grok", Installed: installed, Version: info.Version,
		Availability: contract.StartAvailabilityUnknown, AuthState: contract.StartAuthStateUnknown,
		IntegrationState: state, Capabilities: &contract.StartCapabilities{}}
}

// StartGrokSessions installs a dormant source. Only a person's open or admitted
// dispatch starts the coding CLI; inventory never authenticates or prompts it.
func (s *Server) StartGrokSessions(ctx context.Context) (func(), error) {
	installation := grokResolver().Snapshot(ctx)
	if !installation.Compatible {
		return func() {}, nil
	}
	h, err := antigravitysession.New(ctx, s.grokHostOptions(installation.Path, grok.AccountDir(), nil))
	if err != nil {
		return nil, err
	}
	if !s.grokSessions.CompareAndSwap(nil, h) {
		h.Shutdown()
		return nil, ownedAntigravityUnavailable()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := h.Shutdown(); err != nil {
				log.Print("grok: owned_session_cleanup_failed")
			}
			s.grokSessions.CompareAndSwap(h, nil)
		})
	}, nil
}

// grokHostOptions is the one place a Grok host is configured, so tests and the
// daemon share its naming callbacks. Without them a Grok Session never asked
// for an automatic title.
func (s *Server) grokHostOptions(executable, accountDir string, args []string) antigravitysession.Options {
	return antigravitysession.Options{Assistant: session.AssistantGrok, Executable: executable, AccountDir: accountDir,
		Args: args, StateDir: filepath.Join(s.cfg.Dir, "grok-sessions"),
		TurnComplete: s.automaticOwnedTitle, PromptAccepted: s.automaticOwnedTitle}
}

func ownedLabel(assistant string) string {
	if assistant == "grok" {
		return "Grok"
	}
	return "Gemini"
}

func ownedAuthRequired(err error) bool {
	var local *antigravityprocess.Error
	return errors.As(err, &local) && local.Code == "auth_required"
}
