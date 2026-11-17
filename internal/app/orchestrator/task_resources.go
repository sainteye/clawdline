package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/sainteye/clawdline/internal/adapters/resourcefs"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// EnableTaskResources refuses to adopt an existing unmarked directory. Failure
// leaves the service unavailable and new heavy work fenced, not silently unmanaged.
func (b *Broker) EnableTaskResources() error {
	b.ResourceServiceRequired = true
	root, err := resourcefs.Open(filepath.Join(b.Dir, "task-resources-v1"))
	if err != nil {
		return err
	}
	b.Resources = &taskresources.Service{Store: b.Store, FS: root, Clock: b.now, Owner: b.resourceOwner, OtherUsage: b.legacyResourceUsage,
		Uses: func(ctx context.Context, r taskresource.Resource) string {
			return resourcefs.UsePresence(ctx, filepath.Join(root.Path(), r.ID))
		}, FreeSpace: func() (int64, error) {
			r := b.Store.Reading(b.Dir)
			if !r.HasDiskFree {
				return 0, errors.New("resource_disk_space_unknown")
			}
			return r.DiskFree, nil
		}}
	return nil
}

func ResourceOwnerScope() taskresources.Scope {
	return taskresources.Scope{Actor: "daemon", AllProjects: true, Inspect: true, Reclaim: true, Publish: true}
}

func evidenceDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (b *Broker) resourceOwner(ctx context.Context, kind, id string) (taskresources.OwnerState, error) {
	out := taskresources.OwnerState{Presence: "unknown", UsePresence: "unknown"}
	switch kind {
	case "task":
		r, _, err := b.Record(ctx, id)
		if err != nil {
			return out, err
		}
		project := r.Repository
		if project == "" {
			project = r.ProjectDir
		}
		out.Owner = taskresource.Owner{Kind: kind, ID: id, Project: project}
		out.Complete = r.State.Terminal() && !r.FinishedAt.IsZero()
		out.CompletedAt = r.FinishedAt.Unix()
		out.OutputRoot = filepath.Join(b.Tasks.Path(id), "artifacts")
		// Store results survive scratch and raw-log expiry. On-disk child results
		// must still agree with the result accepted by the broker.
		if out.Complete && ownedPath(b.Tasks.Dir, r.Dir, id) {
			if r.Callback != nil {
				out.OutputsSaved = r.Callback.Exit != nil || r.Callback.Signal != "" || r.Verdict != ""
				out.EvidenceDigest = evidenceDigest(struct {
					State    State
					Verdict  string
					Callback *Callback
				}{r.State, r.Verdict, r.Callback})
			} else if r.Gate != nil && r.Result != nil && r.Result.GateVerdict != nil {
				// Gate collectors persist a typed result outside ordinary result.json.
				out.OutputRoot, out.EvidenceDigest, out.OutputsSaved = b.savedGateOutput(r)
			} else if r.Result != nil {
				disk, raw, e := b.Tasks.ReadResult(id)
				stored := *r.Result
				disk.Secret = ""
				stored.Secret = ""
				out.OutputsSaved = e == nil && reflect.DeepEqual(disk, stored)
				out.EvidenceDigest = evidenceDigest(struct {
					Accepted any
					Raw      string
				}{stored, digestGate(raw)})
				for _, name := range stored.Artifacts {
					path := name
					if !filepath.IsAbs(path) {
						path = filepath.Join(r.Dir, path)
					}
					rel, e := filepath.Rel(out.OutputRoot, path)
					if e != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
						out.OutputsSaved = false
						break
					}
					if _, e := os.Lstat(path); e != nil {
						out.OutputsSaved = false
						break
					}
				}
			}
		}
		if !out.Complete {
			return out, nil
		}
		if r.Callback != nil {
			out.Presence = b.callbackResourcePresence(r)
		} else {
			gone, why, _ := b.ownerGone(ctx, b.read(ctx), r, r.Dir)
			if gone {
				out.Presence = "absent"
			} else if why == WhyOwnerPresent {
				out.Presence = "present"
			}
		}
	case "root":
		a, err := b.RootAssignmentByID(ctx, id)
		if err != nil {
			return out, err
		}
		out.Owner = taskresource.Owner{Kind: kind, ID: id, Project: a.ProjectDir}
		out.OutputRoot = filepath.Join(b.AssignmentRoot(), id, "artifacts")
		p := b.rootCleanup(ctx, b.read(ctx), a)
		out.Complete = p.Completion
		out.Presence = p.Owner
		if a.Closure != nil {
			out.CompletedAt = a.Closure.ClosedAt
		}
		// A safe close is completion, not proof that outcomes were saved. Require
		// a durable delivery from that exact conversation during this assignment.
		if out.Complete && a.Closure.ConversationID != "" {
			events, e := b.Store.LatestEvents(ctx, "session.delivered", []string{a.Closure.TerminalID})
			if e == nil {
				if event, ok := events[a.Closure.TerminalID]; ok {
					var receipt struct {
						Conversation string `json:"conversation"`
						At           int64  `json:"at"`
						Summary      string `json:"summary"`
					}
					if json.Unmarshal(event.Payload, &receipt) == nil && receipt.Conversation == a.Closure.ConversationID && receipt.At >= a.Executor.OpenedAt && receipt.At <= a.Closure.ClosedAt && receipt.Summary != "" {
						out.OutputsSaved = true
						out.EvidenceDigest = evidenceDigest(json.RawMessage(event.Payload))
					}
				}
			}
		}
	default:
		return out, errors.New("resource_owner_unknown")
	}
	if out.Presence == "absent" {
		out.UsePresence = "absent"
	}
	return out, nil
}

func (b *Broker) callbackResourcePresence(r Record) string {
	if r.Callback == nil {
		return "unknown"
	}
	if r.Callback.PGID <= 0 {
		if r.State.Terminal() && r.Callback.NotStarted {
			return "absent"
		}
		return "unknown"
	}

	path := (callbackFiles{r.Dir}).lock()
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "unknown"
	}
	held, err := lockHeld(path)
	if err != nil {
		return "unknown"
	}
	if held {
		return "present"
	}
	return callbackGroupPresence(r.Callback.PGID)
}

// Seal only from source-owned publication facts. Retries after a restart never
// replace an existing manifest; changed output requires explicit owner review.
func (b *Broker) sealResourcePage(ctx context.Context, after string) error {
	rows, _, err := b.Store.TaskResourcePage(ctx, after, taskresource.PageRowsLimit, true)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Manifest == "" && r.LastOutcome != "removing" {
			_, _ = b.Resources.Seal(ctx, ResourceOwnerScope(), r.ID)
		}
	}
	return nil
}

func (b *Broker) SweepTaskResources(ctx context.Context, after string) (taskresources.Result, string, error) {
	if b.Resources == nil {
		return taskresources.Result{}, after, errors.New("resource_service_unavailable")
	}
	if err := b.sealResourcePage(ctx, after); err != nil {
		return taskresources.Result{}, after, err
	}
	return b.Resources.Sweep(ctx, after)
}

// Callback resources are allocated before a process can start. Their paths are
// daemon-owned record fields, not environment variables accepted from a client.
func (b *Broker) prepareCallbackResources(ctx context.Context, r Record) (Record, error) {
	if b.Resources == nil {
		if b.ResourceServiceRequired && r.Callback.Intent == CallbackIntentHeavy {
			return r, b.AdmitHeavyResources(ctx)
		}
		return r, nil // read/wait paths remain available; isolated brokers opt in.
	}
	if r.Callback.Intent == CallbackIntentHeavy {
		if err := b.AdmitHeavyResources(ctx); err != nil {
			return r, err
		}
	}
	if err := os.MkdirAll(filepath.Join(r.Dir, "artifacts"), 0o700); err != nil {
		return r, err
	}
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", r.ID, "")
	if err != nil {
		return r, err
	}
	paths := map[taskresource.Class]string{}
	for _, res := range rows {
		if err := b.Resources.FS.Verify(res); err == nil && res.LastOutcome != "removed" {
			paths[res.Class] = filepath.Join(b.Resources.FS.Path(), res.ID)
		}
	}
	for _, class := range []taskresource.Class{taskresource.Scratch, taskresource.Cache} {
		if paths[class] == "" {
			a, err := b.Resources.Allocate(ctx, ResourceOwnerScope(), "task", r.ID, class)
			if err != nil {
				return r, err
			}
			paths[class] = a.Path
		}
	}
	next, err := b.mutate(ctx, r.ID, "task.resources_allocated", func(x *Record) error {
		if x.State.Terminal() || !x.Callback.StartedAt.IsZero() {
			return errors.New("resource_owner_already_started")
		}
		x.Callback.ScratchPath = paths[taskresource.Scratch]
		x.Callback.CachePath = paths[taskresource.Cache]
		return nil
	})
	return next, err
}

// New compile admission is distinct from lease renewal: pressure cannot revoke
// a running command's lease or grant permission to delete its inputs.
func (b *Broker) AdmitHeavyResources(ctx context.Context) error {
	if b.Resources == nil {
		return refuse(507, "resource_service_unavailable", "Managed resource service is unavailable.")
	}
	if err := b.Resources.Admit(ctx); err != nil {
		// Source-owned publication may have finished since the previous sweep.
		// Seal it here before retrying pressure reclamation. This is not Bot authority.
		after := ""
		for {
			_, next, e := b.SweepTaskResources(ctx, after)
			if e != nil {
				break
			}
			if next == "" {
				break
			}
			after = next
		}
		// The legacy reclaimer retains its own Git, owner and preservation
		// procedure; the resource service never adopts those paths.
		legacy, legacyErr := b.Reclaim(ctx, false)
		remaining := legacy.Deferred
		for legacyErr == nil && remaining > 0 && ctx.Err() == nil {
			_, legacyErr = b.Reclaim(ctx, false)
			remaining -= reclaimSweepLimit
		}
		err = b.Resources.Admit(ctx)
		if err == nil {
			return nil
		}
		return refuseWith(507, err.Error(), "Storage limits prevent a new build. Finish or preserve work, then reclaim eligible temporary data and retry.", map[string]any{"policy": taskresource.DefaultPolicy()})
	}
	return nil
}

// Keep these facts when callback resources still depend on process-group and
// lock evidence. Raw output alone can expire independently.
func (b *Broker) callbackResourceMetadataNeeded(ctx context.Context, id string) bool {
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", id, "")
	return err != nil || len(rows) > 0
}

// Use the gate collector's actual durable protocol. A malformed ordinary
// result.json has no gate authority and must not strand all completed caches.
func (b *Broker) savedGateOutput(r Record) (string, string, bool) {
	root := filepath.Join(filepath.Dir(b.Tasks.Dir), "gate-submissions", r.ID)
	receipt, body, err := b.Tasks.ReadGateResult(r.ID)
	if err != nil {
		return root, "", false
	}
	result, err := contract.DecodeWorkGateResult(body)
	if err != nil || receipt.TaskID != r.ID || receipt.Sha256 != digestGate(body) || receipt.Verdict != result.Verdict {
		return root, "", false
	}
	if validateGateResultIdentity(r, result) != nil || !reflect.DeepEqual(result, *r.Result.GateVerdict) || b.validateGateArtifacts(r.ID, result) != nil {
		return root, "", false
	}
	return root, evidenceDigest(struct {
		Accepted any
		Raw      string
	}{r.Result, digestGate(body)}), true
}
