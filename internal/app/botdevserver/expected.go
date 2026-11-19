package botdevserver

import (
	"context"
	"sort"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// admitExpected holds ops. A human control names the complete observed resource
// baseline; an intervening completed attempt must not become a fresh execution.
func (r *Registry) admitExpected(ctx context.Context, fingerprint, attempt string, expected *SavedServer) (*registered, SavedServer, bool, error) {
	e, prior, done, err := r.admit(ctx, fingerprint, attempt)
	if err != nil || expected == nil {
		return e, prior, done, err
	}
	if e == nil || e.saved.ID != expected.ID {
		return e, prior, false, failure(pa.StaleVersion, "server_definition")
	}
	current := r.view(e)
	if current.Spec != expected.Spec {
		return e, prior, false, failure(pa.ScopeDenied, "server_definition")
	}
	if current != *expected {
		if current.Attempt.Identity.AttemptID == attempt {
			return e, current, true, nil
		}
		return e, prior, false, failure(pa.StaleVersion, "server_definition")
	}
	return e, prior, done, nil
}

// Snapshot is trusted Host metadata plumbing, not native disclosure. Human
// readers may retain history across Forget; the caller supplies authentication
// and the complete Bot/Project boundary before using this snapshot.
func (r *Registry) Snapshot(key pa.BotKey, project pa.ProjectScope) []SavedServer {
	r.mu.Lock()
	var entries []*registered
	for _, e := range r.rows {
		if e.saved.Spec.Key == key && e.saved.Spec.Project == project {
			entries = append(entries, e)
		}
	}
	r.mu.Unlock()
	rows := make([]SavedServer, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, r.view(e))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}
