package squadpack

// ExportSelection names each private scope the person explicitly selected.
// A zero selection is the safe default and exports shareable definitions only.
type ExportSelection struct {
	PrivateScopes  []string
	ConfirmPrivate bool
}

func privateScopeKey(p Private) string {
	if p.Scope == "global" {
		return "global"
	}
	return p.Project
}

// Export gathers only requested settings before Build. A caller must still
// check the authenticated person's read/write authority for each scope.
func Export(m Manifest, files map[string][]byte, selection ExportSelection) ([]byte, error) {
	selected := map[string]bool{}
	for _, scope := range selection.PrivateScopes {
		if scope == "" || selected[scope] {
			return nil, refuse(ErrPrivateScope)
		}
		selected[scope] = true
	}
	if len(selected) > 0 && !selection.ConfirmPrivate {
		return nil, refuse(ErrPrivateConfirmation)
	}
	public := make(map[string][]byte, len(files))
	privatePaths := map[string]bool{}
	for _, p := range m.Private {
		privatePaths[p.Path] = true
	}
	for path, body := range files {
		if !privatePaths[path] {
			public[path] = body
		}
	}
	kept := make([]Private, 0, len(selected))
	for _, p := range m.Private {
		if selected[privateScopeKey(p)] {
			kept = append(kept, p)
			public[p.Path] = files[p.Path]
			delete(selected, privateScopeKey(p))
		}
	}
	if len(selected) > 0 {
		return nil, refuse(ErrPrivateScope)
	}
	m.Private = kept
	return Build(m, public)
}
