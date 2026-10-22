package http

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
)

// StartAntigravityLaunchStore owns one private store. The composition root must
// stop hook callbacks before closing it. Construction starts no goroutine and
// never probes a provider or reads an account.
func (s *Server) StartAntigravityLaunchStore(ctx context.Context) (func(), error) {
	s.antigravityLaunchMu.Lock()
	defer s.antigravityLaunchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.antigravityLaunchStore.Load() != nil {
		return nil, &antigravitylaunch.Error{Code: "antigravity_context_started"}
	}
	root, err := filepath.EvalSymlinks(s.cfg.Dir)
	if err != nil || !filepath.IsAbs(root) {
		return nil, &antigravitylaunch.Error{Code: "antigravity_context_configuration_invalid"}
	}
	owner, err := antigravitylaunch.Open(antigravitylaunch.Config{
		StateDir: filepath.Join(root, "antigravity"), SquadLaunchRoot: filepath.Join(root, "squad", "launches"),
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		owner.Close()
		return nil, err
	}
	s.antigravityLaunchStore.Store(owner)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.antigravityLaunchMu.Lock()
			defer s.antigravityLaunchMu.Unlock()
			s.antigravityLaunchStore.CompareAndSwap(owner, nil)
			owner.Close()
		})
	}, nil
}
