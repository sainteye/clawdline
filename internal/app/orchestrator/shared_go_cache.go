package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

type childGoCache struct {
	ID        string `json:"id"`
	Project   string `json:"project"`
	BuildPath string `json:"build_path"`
}

func (b *Broker) spawnWithGoCache(ctx context.Context, r Record, cwd, secret string, opened func()) Record {
	if next, err := b.prepareChildGoCache(ctx, r, cwd); err == nil {
		r = next
	} else {
		log.Printf("broker: task %s could not attach its Project Go cache: %v", r.ID, err)
	}
	return b.spawn(ctx, r, cwd, secret, opened)
}

func (b *Broker) prepareChildGoCache(ctx context.Context, r Record, cwd string) (Record, error) {
	// Verification and support launches have a closed writable filesystem.
	// Provider-owned and ask-mode Sessions keep their configured permissions.
	if b.Resources == nil || r.Gate != nil || r.Kind == TaskKindSupportAnswer {
		return r, nil
	}
	switch r.Assistant {
	case projects.AssistantCodex:
		if (r.PermissionMode != "edits" && r.PermissionMode != "full") || len(codexGitWritePaths(r, cwd)) == 0 {
			return r, nil
		}
	case projects.AssistantClaude:
		if r.PermissionMode != "full" {
			return r, nil
		}
	default:
		return r, nil
	}
	// Avoid allocating a resource for a project without a Go workspace.
	for dir := cwd; filepath.IsAbs(dir); dir = filepath.Dir(dir) {
		for _, name := range []string{"go.mod", "go.work"} {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
				return b.attachSharedGoCache(ctx, r, true)
			}
		}
		if dir == r.Repository || (r.Worktree != nil && dir == r.Worktree.Path) || dir == filepath.Dir(dir) {
			break
		}
	}
	return r, nil
}

func (b *Broker) prepareSharedGoCache(ctx context.Context, r Record) (Record, error) {
	return b.attachSharedGoCache(ctx, r, true)
}

func (b *Broker) attachSharedGoCache(ctx context.Context, r Record, beforeStart bool) (Record, error) {
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
			if x.Callback == nil && x.State == StateQueued && beforeStart {
				cache := childGoCache{ID: resource.ID, Project: project, BuildPath: path}
				if x.SharedGoCache != nil && *x.SharedGoCache != cache {
					return errors.New("resource_identity_changed")
				}
				x.SharedGoCache = &cache
				return nil
			}
			if x.Callback == nil || x.State.Terminal() || (beforeStart && !x.Callback.StartedAt.IsZero()) {
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

// The durable consumer records identify which processes must have stopped.
// Their output publication is relevant only to their individual resources.
func (b *Broker) sharedGoOwner(ctx context.Context, id string) (taskresources.OwnerState, error) {
	out := taskresources.OwnerState{Presence: "unknown", UsePresence: "unknown"}
	resources, next, err := b.Store.TaskResourcesByOwnerPage(ctx, "shared_go", id, "")
	if err != nil || next != "" || len(resources) != 1 {
		return out, errors.New("resource_shared_cache_unknown")
	}
	resource := resources[0]
	out.Owner = resource.Owner
	// Each owner check, including the recheck at removal, gets fresh readings.
	// Read lazily so callback-only caches never scan child terminals or CWDs.
	read := sync.OnceValue(func() reading { return b.read(ctx) })
	occupied := b.occupancySnapshot(ctx)
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
			use := r.SharedGoCache
			if r.Callback != nil && r.Callback.GoCacheID != "" {
				use = &childGoCache{ID: r.Callback.GoCacheID, Project: r.Callback.GoCacheProject, BuildPath: r.Callback.GoCachePath}
			}
			if use == nil || use.ID != resource.ID {
				continue
			}
			if use.Project != resource.Owner.Project || use.BuildPath != filepath.Join(b.Resources.FS.Path(), resource.ID, "go-build") {
				return out, errors.New("resource_shared_use_unverified")
			}
			r, err := Decode(row.Record)
			if err != nil {
				return out, errors.New("resource_source_unknown")
			}
			if !r.State.Terminal() || r.FinishedAt.IsZero() {
				return out, nil
			}
			if !ownedPath(b.Tasks.Dir, r.Dir, r.ID) {
				return out, errors.New("resource_shared_use_unverified")
			}
			presence, err := b.sharedGoConsumerPresence(r, read, occupied)
			if err != nil {
				return out, err
			}
			if presence != "absent" {
				out.Presence, out.UsePresence = presence, presence
				return out, nil
			}
			out.CompletedAt = max(out.CompletedAt, r.FinishedAt.Unix())
			proofs.Write([]byte(evidenceDigest(struct {
				ID, Terminal, Backend string
				State                 State
				Spawned, Finished     time.Time
				Callback              *Callback
			}{r.ID, r.ChildTerminalID, r.ChildBackend, r.State, r.SpawnedAt, r.FinishedAt, r.Callback})))
			consumers++
		}
		if next == "" {
			break
		}
		after = next
	}
	if consumers == 0 {
		return out, errors.New("resource_shared_use_missing")
	}
	out.Presence, out.UsePresence = "absent", "absent"
	out.Complete, out.OutputsSaved = true, true
	out.EvidenceDigest = hex.EncodeToString(proofs.Sum(nil))
	return out, nil
}

func (b *Broker) sharedGoConsumerPresence(r Record, read func() reading, occupied func(...string) (string, error)) (string, error) {
	if r.Callback != nil {
		return b.callbackResourcePresence(r), nil
	}
	paths := []string{r.Dir}
	if r.Worktree != nil {
		paths = append(paths, r.Worktree.Path)
	}
	return childResourcePresence(r, read(), occupied, paths...)
}

// UseCallbackGoCache lets trusted check tooling attach before its first Go
// command. Scheduling intent does not decide whether compilation data is shared.
// The source callback and canonical Project still own every consumer fact.
func (b *Broker) UseCallbackGoCache(ctx context.Context, id string) (taskresources.Allocation, error) {
	if b.Resources == nil {
		return taskresources.Allocation{}, errors.New("resource_service_unavailable")
	}
	r, _, err := b.Record(ctx, id)
	if err != nil {
		return taskresources.Allocation{}, err
	}
	if r.Callback == nil || r.State.Terminal() || r.Callback.StartedAt.IsZero() {
		return taskresources.Allocation{}, errors.New("resource_owner_not_allocatable")
	}
	if r.Callback.GoCacheID == "" {
		r, err = b.attachSharedGoCache(ctx, r, false)
		if err != nil {
			return taskresources.Allocation{}, err
		}
	}
	resource, err := b.Store.TaskResource(ctx, r.Callback.GoCacheID)
	if err != nil {
		return taskresources.Allocation{}, err
	}
	if err = b.Resources.FS.Verify(resource); err != nil {
		return taskresources.Allocation{}, err
	}
	path := filepath.Join(b.Resources.FS.Path(), resource.ID, "go-build")
	sourceProject := r.Repository
	if sourceProject == "" {
		sourceProject = r.ProjectDir
	}
	project, ok := projects.CanonicalProjectKey(sourceProject)
	if !ok || resource.Owner.Kind != "shared_go" || resource.Owner.Project != project || r.Callback.GoCacheProject != project || r.Callback.GoCachePath != path || (resource.LastOutcome != "" && resource.LastOutcome != "kept") {
		return taskresources.Allocation{}, errors.New("resource_shared_use_unverified")
	}
	return taskresources.Allocation{Resource: resource, Path: path, Artifacts: filepath.Join(r.Dir, "artifacts")}, nil
}
