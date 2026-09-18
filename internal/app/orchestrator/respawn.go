package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// Retrying a tab that never opened, without making the root write the task
// out again. The Swift app's `POST /v1/orchestrator/tasks/:id/respawn`,
// carried over whole (docs/broker-design.md D3, #20).
//
// `spawn_failed` was 34 of 206 dispatches on 2026-08-28, and until this route
// existed the protocol's answer was that the root must write the whole
// task.json out again under a fresh id: thirty-four rewrites by the most
// context-loaded session in the tree, each one a chance to drop a field. So the
// file is copied, not rewritten — task.json is the only place `instructions`
// was ever written down — with a fresh id and a fresh secret, because the old
// id is finished and re-sending it opens nothing.

// RespawnLimit is how many retries may descend from one original dispatch. Two
// gets past a terminal that would not open, and is few enough that a tab
// failing for a real reason stops being retried in a loop.
const RespawnLimit = 2

// Respawned is what a respawn answers: the new task's dispatch, the secret it
// was given, and where it came from.
type Respawned struct {
	Dispatched
	Secret   string
	From     string
	Original string
}

// Respawn retries one spawn_failed task.
func (b *Broker) Respawn(ctx context.Context, id, supplied string) (Respawned, error) {
	if !IsTaskID(id) {
		return Respawned{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"task_id must be a lowercase UUID.")
	}
	origin, _, err := b.Record(ctx, id)
	if err != nil {
		return Respawned{}, err
	}
	// Only the one terminal state that means "nothing ran". A failure is an
	// answer, a timeout had a session that read the briefing, and a cancel was
	// somebody's decision — copying any of those into a new tab would be
	// re-running work, not retrying a dispatch.
	if origin.State != StateSpawnFailed {
		return Respawned{}, refuseWith(http.StatusConflict, "not_respawnable",
			fmt.Sprintf("Only a spawn_failed task may be respawned; task %s is %s.", id, origin.State),
			map[string]any{"state": string(origin.State)})
	}
	all, err := b.records(ctx)
	if err != nil {
		return Respawned{}, err
	}
	original, descendants := respawnFamily(all, id)
	if descendants >= RespawnLimit {
		return Respawned{}, refuseWith(http.StatusConflict, "respawn_exhausted",
			fmt.Sprintf("Task %s has already been respawned %d times; the limit is %d. "+
				"Dispatch a new task, or find out why the tab will not open.", original, descendants, RespawnLimit),
			map[string]any{"original_task": original, "respawns": descendants, "limit": RespawnLimit})
	}
	if supplied != "" && !IsTaskSecret(supplied) {
		return Respawned{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"secret must be 64 hex characters.")
	}
	body, err := os.ReadFile(filepath.Join(b.Tasks.Path(id), "task.json"))
	var brief map[string]any
	if err == nil {
		err = json.Unmarshal(body, &brief)
	}
	if err != nil || brief == nil {
		return Respawned{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"The original task.json is gone, so there is nothing to respawn from; write a new one.")
	}
	secret := supplied
	if secret == "" {
		secret = NewSecret()
	}
	fresh := uuidLike()
	brief["task_id"] = fresh
	if err := b.writeBriefOnce(fresh, brief); err != nil {
		return Respawned{}, refuse(http.StatusInternalServerError, "internal",
			"Could not write the respawned task file.")
	}
	out, err := b.Dispatch(ctx, DispatchRequest{
		TaskID: fresh, Secret: secret,
		Respawn:  &RespawnOrigin{TaskID: id, Generation: origin.RespawnGeneration + 1},
		Schedule: scheduleOf(origin),
	})
	if err != nil {
		// A refused respawn leaves its directory behind for the ordinary
		// sweep, exactly as a root's own abandoned attempt does.
		return Respawned{}, err
	}
	// Recorded after the dispatch, not before: written first, the line would
	// record retries that never happened — an over_capacity refusal opens no
	// tab — and an audit bigger than the thing it audits is worse than none.
	payload, _ := json.Marshal(map[string]any{
		"task": fresh, "from": id, "original": original, "generation": origin.RespawnGeneration + 1,
	})
	_ = b.Store.Append(ctx, store.Event{Kind: "task.respawn", Subject: fresh, Payload: payload})
	return Respawned{Dispatched: out, Secret: secret, From: id, Original: original}, nil
}

// writeBriefOnce puts a task.json the broker made — a respawn's copy, a
// schedule's template (scheduled.go) — in a new task's directory, with the same
// modes an ordinary brief gets: 0700 directory, 0600 file, the mode passed to
// the create rather than applied after it, and never over a file already there.
func (b *Broker) writeBriefOnce(id string, brief map[string]any) error {
	dir := b.Tasks.Path(id)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(brief, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "task.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// respawnFamily is the first task in id's respawn chain, and how many tasks
// this store holds that descend from it.
//
// The cap is on the family, not on any one chain: at most two respawns descend
// from one original. A depth cannot enforce that, because a respawn writes
// nothing back to the task it retried — a spawn_failed original stays at
// generation zero however many times it is respawned, so counting its own
// depth would let the same original be retried for ever. That is also the
// shape the caller falls into, since the id a root has in hand is the one that
// failed: `curl …/$FAILED_ID/respawn`, again, and again.
//
// Both walks stop at a task this store does not hold and at a cycle it should
// never contain, so a forgotten ancestor shortens the chain rather than
// hanging the walk.
func respawnFamily(all []Record, id string) (string, int) {
	parents := map[string]string{}
	for _, r := range all {
		parents[r.ID] = r.RespawnOf
	}
	originOf := func(id string) string {
		current := id
		seen := map[string]bool{current: true}
		for {
			previous := parents[current]
			if previous == "" || seen[previous] {
				return current
			}
			if _, held := parents[previous]; !held {
				return current
			}
			seen[previous] = true
			current = previous
		}
	}
	original := originOf(id)
	descendants := 0
	for other := range parents {
		if other != original && originOf(other) == original {
			descendants++
		}
	}
	return original, descendants
}
