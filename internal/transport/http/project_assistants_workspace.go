package http

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (p projectAssistantProjects) ProvisionPersonalWorkspace(ctx context.Context, target pa.ProjectSelection) (pa.ProjectResolution, error) {
	if err := ctx.Err(); err != nil {
		return pa.ProjectResolution{}, err
	}
	identity, err := p.server.projectAssistantIdentity()
	if err != nil {
		return pa.ProjectResolution{}, err
	}
	if target.MachineID != identity.MachineID || !pa.IsPersonalWorkspaceID(target.ProjectID) {
		return pa.ProjectResolution{}, paHTTPFailure(pa.ScopeDenied, "personal_workspace")
	}
	return p.personalWorkspace(target, true)
}

func (p projectAssistantProjects) personalWorkspace(target pa.ProjectSelection, create bool) (pa.ProjectResolution, error) {
	// Use a flat, source-owned namespace. Root operations cannot follow
	// symlinks outside the daemon state tree, and each component must itself
	// be a directory before exposing its path to the provider.
	base, err := filepath.EvalSymlinks(p.server.cfg.Dir)
	if err != nil {
		return pa.ProjectResolution{}, errors.Join(paHTTPFailure(pa.NotFound, "personal_workspace"), err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return pa.ProjectResolution{}, errors.Join(paHTTPFailure(pa.NotFound, "personal_workspace"), err)
	}
	defer root.Close()
	const parent = app.PersonalWorkspaceDirectory
	relative := filepath.Join(parent, target.ProjectID)
	for _, path := range []string{parent, relative} {
		if create {
			if err = root.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return pa.ProjectResolution{}, errors.Join(paHTTPFailure(pa.NotFound, "personal_workspace"), err)
			}
		}
		info, err := root.Lstat(path)
		if err != nil {
			return pa.ProjectResolution{}, errors.Join(paHTTPFailure(pa.NotFound, "personal_workspace"), err)
		}
		if !info.IsDir() {
			return pa.ProjectResolution{}, paHTTPFailure(pa.ScopeDenied, "personal_workspace")
		}
	}
	return pa.ProjectResolution{Scope: pa.PersonalScope(target), Directory: filepath.Join(base, relative), Current: true, Available: true}, nil
}
