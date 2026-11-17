package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/resourcefs"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func (b *Broker) prepareSharedGoCache(ctx context.Context, r Record) (Record, error) {
	project := r.Repository
	if project == "" {
		project = r.ProjectDir
	}
	project, ok := projects.CanonicalProjectKey(project)
	if !ok {
		return r, errors.New("resource_project_unknown")
	}
	var next Record
	err := b.Resources.UseSharedGo(ctx, ResourceOwnerScope(), project, func(resource taskresource.Resource, path string) error {
		var err error
		next, err = b.mutate(ctx, r.ID, "task.shared_go_cache_attached", func(x *Record) error {
			if x.Callback == nil || x.State.Terminal() || !x.Callback.StartedAt.IsZero() {
				return errors.New("resource_owner_already_started")
			}
			if x.Callback.GoCacheID != "" && (x.Callback.GoCacheID != resource.ID || x.Callback.GoCachePath != path) {
				return errors.New("resource_identity_changed")
			}
			x.Callback.GoCacheID, x.Callback.GoCachePath, x.Callback.GoCacheProject = resource.ID, path, project
			return nil
		})
		return err
	})
	return next, err
}

// Every consumer is a durable callback record, not a client assertion or a
// heartbeat timeout. A live group or unreadable publication preserves the cache.
func (b *Broker) sharedGoOwner(ctx context.Context, id string) (taskresources.OwnerState, error) {
	out := taskresources.OwnerState{Presence: "unknown", UsePresence: "unknown"}
	resources, next, err := b.Store.TaskResourcesByOwnerPage(ctx, "shared_go", id, "")
	if err != nil || next != "" || len(resources) != 1 {
		return out, errors.New("resource_shared_cache_unknown")
	}
	resource := resources[0]
	out.Owner = resource.Owner
	out.Complete, out.OutputsSaved = true, true
	out.Presence, out.UsePresence = "absent", "absent"
	proofs := sha256.New()
	consumers := 0
	after := ""
	for {
		rows, next, err := b.Store.TaskResourceSourcePage(ctx, after)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			var r Record
			if json.Unmarshal(row.Record, &r) != nil || r.ID != row.ID || !IsTaskID(r.ID) {
				return out, errors.New("resource_source_unknown")
			}
			if r.Callback == nil || r.Callback.GoCacheID != resource.ID {
				continue
			}
			if r.Callback.GoCacheProject != resource.Owner.Project || r.Callback.GoCachePath != filepath.Join(b.Resources.FS.Path(), resource.ID, "go-build") {
				return out, errors.New("resource_shared_use_unverified")
			}
			o, err := b.resourceOwner(ctx, "task", r.ID)
			if err != nil {
				return out, err
			}
			out.Complete = out.Complete && o.Complete
			out.OutputsSaved = out.OutputsSaved && o.OutputsSaved
			out.CompletedAt = max(out.CompletedAt, o.CompletedAt)
			if o.Presence != "absent" {
				out.Presence = o.Presence
			}
			if o.UsePresence != "absent" {
				out.UsePresence = o.UsePresence
			}
			if !out.Complete || !out.OutputsSaved || out.Presence != "absent" || out.UsePresence != "absent" {
				return out, nil
			}
			manifest, err := resourcefs.Manifest(ctx, o.OutputRoot)
			if err != nil {
				return out, err
			}
			proofs.Write([]byte(evidenceDigest(struct{ ID, Evidence, Files string }{r.ID, o.EvidenceDigest, manifest.Digest})))
			consumers++
			if out.OutputRoot == "" {
				out.OutputRoot = o.OutputRoot
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	if consumers == 0 {
		return out, errors.New("resource_shared_use_missing")
	}
	out.EvidenceDigest = hex.EncodeToString(proofs.Sum(nil))
	return out, nil
}
