package http

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravityproject"
	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// antigravityProjectRoot requires today's catalog plus actual Git registration
// for linked worktrees. Neither a .git pointer nor a path prefix is authority.
func (s *Server) antigravityProjectRoot(ctx context.Context, workspace string) (string, error) {
	root, ok := projects.CanonicalProjectKey(workspace)
	if !ok {
		return "", antigravityproject.Unverified
	}
	known := false
	for _, p := range s.workV2Projects(ctx) {
		if p.Path == root {
			known = true
			break
		}
	}
	if !known {
		return "", antigravityproject.Unverified
	}
	if err := projects.VerifyProjectWorkspace(ctx, root, workspace); err != nil {
		return "", antigravityproject.Unverified
	}
	return root, nil
}

// StartAntigravityProjectConsent owns one immutable helper pin per boot, not
// per preview or Project. It starts no worker/provider and publishes no native
// behavior row or policy input. A missing helper permits revocation only.
func (s *Server) StartAntigravityProjectConsent(ctx context.Context) (func(), error) {
	s.antigravityProjectMu.Lock()
	defer s.antigravityProjectMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.antigravityProjectOwner.Load() != nil {
		return nil, antigravityproject.Changed
	}
	root, err := filepath.EvalSymlinks(s.cfg.Dir)
	if err != nil {
		return nil, antigravityproject.Invalid
	}
	helper, err := os.Executable()
	if err != nil {
		return nil, antigravityproject.HelperUnavailable
	}
	// Keep the known absolute path even if its image was removed. Durable
	// revocation must remain available; cleanup may then report unknown.
	if canonical, e := filepath.EvalSymlinks(helper); e == nil {
		helper = canonical
	}
	// The nil image is an explicit unavailable state. Never fall back to a
	// different helper or call a provider to decide whether this one works.
	image, _ := antigravity.NewPinnedExecutable(ctx, helper)
	var owner *antigravityproject.Manager
	owner, err = antigravityproject.New(ctx, antigravityproject.Config{Store: s.store, Helper: helper, ConfigRoot: root, Image: image,
		Resolve: func(requestCtx context.Context, workspace string) (string, error) {
			if ctx.Err() != nil || s.antigravityProjectOwner.Load() != owner {
				return "", antigravityproject.Unverified
			}
			return s.antigravityProjectRoot(requestCtx, workspace)
		}})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.antigravityProjectOwner.Store(owner)
	var once sync.Once
	return func() { once.Do(func() { s.antigravityProjectOwner.CompareAndSwap(owner, nil) }) }, nil
}
