package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// Dispatcher creates a task, opens a session for it and records the intent.
type Dispatcher struct {
	Store    *store.Store
	Tasks    taskdir.Root
	Terminal ports.TerminalHost
}

// Dispatch accepts a task and starts it.
//
// The order matters and is the whole design: the intent is recorded durably
// before the effect is attempted, so a crash between the two leaves a task that
// is known and unstarted rather than one that is running and unrecorded. The
// receipt says the intent committed. It does not say the assistant received
// anything — that is a later fact with its own evidence.
func (d Dispatcher) Dispatch(ctx context.Context, t task.Task, command string) (store.Receipt, string, error) {
	if err := t.Validate(); err != nil {
		return store.Receipt{}, "", err
	}
	// Arbitrate before anything is created. Two roots dispatching a correction
	// of the same delivery six seconds apart is not hypothetical: it happened,
	// both were isolated, and nothing refused either of them.
	live, err := d.Store.LiveTasks(ctx)
	if err != nil {
		return store.Receipt{}, "", err
	}
	for _, other := range live {
		if shared := task.Overlaps(t.Claims, other.Claims); len(shared) > 0 {
			return store.Receipt{}, "", task.Refusal{
				Code: "workspace_busy",
				Detail: fmt.Sprintf("task %s (%s, started %s ago) already claims %v",
					other.ID, other.Assistant,
					time.Since(other.CreatedAt).Round(time.Second), shared),
			}
		}
	}

	t.State = task.StateQueued
	t.CreatedAt = time.Now()

	dir, err := d.Tasks.Create(t)
	if err != nil {
		return store.Receipt{}, "", err
	}

	payload, _ := json.Marshal(t)
	obligation := &task.Obligation{
		ID:       "task:" + t.ID,
		Kind:     task.KindReview,
		Subject:  t.ID,
		Mover:    task.Mover{Kind: task.MoverTask, ID: t.ID},
		OpenedAt: t.CreatedAt,
		Evidence: task.EvidenceObserved,
		Note:     "dispatched, no result yet",
	}
	receipt, err := d.Store.Commit(ctx, "dispatch:"+t.ID,
		[]store.Event{{Kind: "task.queued", Subject: t.ID, Payload: payload}},
		[]store.Change{{Open: obligation}, {Task: &t}})
	if err != nil {
		return store.Receipt{}, "", err
	}
	if receipt.Replayed {
		// The same dispatch arriving twice starts nothing twice.
		return receipt, dir, nil
	}

	opened, err := d.Terminal.Open(ctx, ports.OpenRequest{
		Name:    "clawdline-" + t.ID,
		Cwd:     t.ProjectDir,
		Command: command,
		Env:     map[string]string{"CLAWDLINE_TASK_DIR": dir},
	})
	if err != nil {
		_, _ = d.Store.Commit(ctx, "spawn-failed:"+t.ID,
			[]store.Event{{Kind: "task.spawn_failed", Subject: t.ID}}, nil)
		return receipt, dir, err
	}

	brief := fmt.Sprintf("Your task directory is %s. Read task.json, do the work, then write result.json.", dir)
	if err := d.Terminal.Send(ctx, opened, brief); err != nil {
		return receipt, dir, err
	}
	// Typing proves bytes reached a tty and nothing more, so this is recorded
	// as an attempt. `briefed` belongs to evidence from the assistant's own
	// record, which arrives later.
	_, _ = d.Store.Commit(ctx, "spawned:"+t.ID,
		[]store.Event{{Kind: "task.spawn_attempted", Subject: t.ID,
			Payload: json.RawMessage(fmt.Sprintf(`{"session":%q}`, opened.ID))}}, nil)
	return receipt, dir, nil
}

// Settle records a finished task, if it has finished.
func (d Dispatcher) Settle(ctx context.Context, id string) (task.State, bool, error) {
	result, ok := d.Tasks.Result(id)
	if !ok {
		return "", false, nil
	}
	state := task.Settle(result)
	payload, _ := json.Marshal(result)
	_, err := d.Store.Commit(ctx, "settle:"+id,
		[]store.Event{{Kind: "task." + string(state), Subject: id, Payload: payload}},
		[]store.Change{{Close: "task:" + id}, {SetTask: [2]string{id, string(state)}}})
	return state, true, err
}
