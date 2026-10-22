package http

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type AntigravityRuntimeOptions struct {
	Admit                 antigravity.HookAdmission
	PreInvocationResponse antigravity.HookResponse
	AfterObservation      antigravity.HookObservation
}

type antigravityRuntimePort struct{ server *Server }

func (p antigravityRuntimePort) VerifyAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	if b := p.server.antigravityBridge.Load(); b != nil {
		return b.VerifyAntigravityProcess(ctx, pid)
	}
	return ports.AntigravityProcess{}, &antigravity.BridgeError{Code: "antigravity_runtime_inactive"}
}
func (p antigravityRuntimePort) AntigravityBinding(ctx context.Context, s session.Session) (ports.AntigravityBinding, error) {
	if b := p.server.antigravityBridge.Load(); b != nil {
		return b.AntigravityBinding(ctx, s)
	}
	return ports.AntigravityBinding{}, &antigravity.BridgeError{Code: "antigravity_runtime_inactive"}
}

// wireAntigravity installs indirections before inventory's method value is
// captured. Start only publishes one bridge; no copied inventory becomes stale.
func (s *Server) wireAntigravity() {
	port := antigravityRuntimePort{s}
	if p, ok := s.inventory.Process.(*process.PS); ok {
		p.Antigravity = port
	}
	if h, ok := s.inventory.Identity.(*transcript.Host); ok {
		h.SetAntigravityBindings(port)
	}
	for _, h := range s.terminals {
		if t, ok := h.(*terminal.Tmux); ok {
			t.Antigravity = func(ctx context.Context, id, tty string) (ports.AntigravityProcess, error) {
				// Before Start there is no native listener or provider reader.
				if s.antigravityBridge.Load() == nil {
					return ports.AntigravityProcess{}, &antigravity.BridgeError{Code: "antigravity_runtime_inactive"}
				}
				return t.AntigravityPaneProcess(ctx, id, tty, port)
			}
		}
	}
}

type normalizedAntigravityReader struct {
	native *antigravity.NativeProcessReader
}

func (r normalizedAntigravityReader) ReadAntigravityProcess(ctx context.Context, pid int) (ports.AntigravityProcess, error) {
	p, err := r.native.ReadAntigravityProcess(ctx, pid)
	if err == nil {
		p.TTY = strings.TrimPrefix(p.TTY, "/dev/")
	}
	return p, err
}

// StartAntigravity owns cancellation, Close and the Serve join. Construction
// and inactive installation checks start no listener or background goroutine.
// stop is concurrent-safe and idempotent. No stale socket is ever unlinked.
func (s *Server) StartAntigravity(ctx context.Context, options AntigravityRuntimeOptions) (func(), error) {
	s.antigravityRuntimeMu.Lock()
	defer s.antigravityRuntimeMu.Unlock()
	if ctx.Err() != nil {
		return nil, &antigravity.BridgeError{Code: "antigravity_runtime_cancelled"}
	}
	if s.antigravityRuntimeStarted {
		return nil, &antigravity.BridgeError{Code: "antigravity_runtime_started"}
	}
	installation := s.antigravityInstallation
	if installation == nil {
		installation = func(ctx context.Context) antigravity.Installation { return antigravityResolver().Snapshot(ctx) }
	}
	info := installation(ctx)
	if !info.InstalledKnown {
		return nil, &antigravity.BridgeError{Code: "antigravity_runtime_installation_unavailable"}
	}
	if !info.Installed {
		return nil, &antigravity.BridgeError{Code: "antigravity_runtime_inactive"}
	}
	if !info.Compatible || info.Version != "1.4.5" {
		return nil, &antigravity.BridgeError{Code: "antigravity_runtime_incompatible"}
	}
	dir, err := antigravity.ProviderStateDir(s.cfg.Dir)
	if err != nil {
		return nil, err
	}
	native := antigravity.NewNativeProcessReader(func(ctx context.Context, pid int, tty string) (string, error) {
		for _, host := range s.terminals {
			resolver, ok := host.(interface {
				ExactAntigravityTerminal(context.Context, int, string) (string, error)
			})
			if !ok {
				continue
			}
			mapped, err := resolver.ExactAntigravityTerminal(ctx, pid, tty)
			// Unreadable supported backends cannot be treated as absence.
			if err != nil {
				return "", err
			}
			if mapped != "" {
				return mapped, nil
			}
		}
		return "", nil
	})
	home, err := s.antigravityRecordHome()
	if err != nil {
		return nil, &antigravity.BridgeError{Code: "antigravity_configuration_invalid"}
	}
	bridge, err := antigravity.NewBridge(home, filepath.Join(dir, "observations.json"), info.Path, normalizedAntigravityReader{native}, transcript.AntigravityPath)
	if err != nil {
		return nil, err
	}
	service, err := antigravity.ListenHookService(antigravity.HookServiceConfig{StateDir: dir, Bridge: bridge, Admit: options.Admit, PreInvocationResponse: options.PreInvocationResponse, AfterObservation: options.AfterObservation})
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.antigravityRuntimeStarted = true
	s.SetAntigravityBridge(bridge)
	go func() {
		defer close(done)
		defer s.antigravityBridge.CompareAndSwap(bridge, nil)
		if err := service.Serve(runCtx); err != nil && runCtx.Err() == nil {
			var typed *antigravity.BridgeError
			if errors.As(err, &typed) {
				log.Printf("antigravity: %s", typed.Code)
			} else {
				log.Print("antigravity: antigravity_hook_failed")
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			service.Close()
			<-done
			s.antigravityRuntimeMu.Lock()
			s.antigravityRuntimeStarted = false
			s.antigravityRuntimeMu.Unlock()
		})
	}, nil
}

// Runtime identity and private proof use the same native record home. Tests
// supply an isolated transcript Host; production never guesses from a payload.
func (s *Server) antigravityRecordHome() (string, error) {
	if host, ok := s.inventory.Identity.(*transcript.Host); ok && host.Home != "" {
		return host.Home, nil
	}
	return os.UserHomeDir()
}
