package taskresources

import (
	"context"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// UseSharedGo attaches a source-owned command before releasing the same fence
// used by plan/apply. A callback cannot start between the last idle observation
// and removal. This never adopts a user-selected GOCACHE.
func (s *Service) UseSharedGo(ctx context.Context, scope Scope, project string, attach func(taskresource.Resource, string) error) error {
	if !s.ready() || !scope.Publish || !filepath.IsAbs(project) {
		return fail("resource_publish_not_allowed")
	}
	owner := taskresource.Owner{Kind: "shared_go", ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(project)).String(), Project: project}
	probe := taskresource.Resource{Owner: owner}
	if !scope.allows(probe) {
		return fail("resource_scope_not_allowed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, next, err := s.Store.TaskResourcesByOwnerPage(ctx, owner.Kind, owner.ID, "")
	if err != nil {
		return err
	}
	if next != "" {
		return fail("resource_shared_cache_ambiguous")
	}
	var selected taskresource.Resource
	for _, r := range rows {
		if r.Owner != owner || r.Class != taskresource.Cache {
			return fail("resource_identity_changed")
		}
		if r.LastOutcome == "removing" {
			// An interrupted trim must not strand a verified shared cache until
			// the next periodic sweep. Reconciliation preserves bytes and keeps
			// another live handle's pending execution fenced.
			if err := s.recoverResourceLocked(ctx, r); err != nil {
				return err
			}
			r, err = s.Store.TaskResource(ctx, r.ID)
			if err != nil {
				return err
			}
		}
		if r.LastOutcome != "" && r.LastOutcome != "kept" {
			return fail("resource_cleanup_in_progress")
		}
		if selected.ID != "" {
			return fail("resource_shared_cache_ambiguous")
		}
		if err := s.FS.Verify(r); err != nil {
			return err
		}
		selected = r
	}
	if selected.ID == "" {
		selected = taskresource.Resource{ID: uuid.NewString(), Generation: uuid.NewString(), Owner: owner, Class: taskresource.Cache, CreatedAt: s.now().Unix()}
		selected.RelativePath = selected.ID
		selected.Identity, err = s.FS.Allocate(selected.ID, selected.Generation)
		if err != nil {
			return err
		}
		if err := s.Store.RegisterTaskResource(ctx, selected); err != nil {
			return err
		}
	}
	path, err := s.FS.GoCachePath(selected)
	if err != nil {
		return err
	}
	// A previous publication belongs to the previous set of consumers. Only
	// the source-owned sealer may publish the expanded set after it finishes.
	if _, err := s.Store.UpdateTaskResource(ctx, selected.ID, "resource.shared_use", s.now(), func(r *taskresource.Resource) error {
		r.Manifest = ""
		return nil
	}); err != nil {
		return err
	}
	s.statsMu.Lock()
	s.stats.Known = false
	s.admissionRevision++
	s.statsMu.Unlock()
	return attach(selected, path)
}
