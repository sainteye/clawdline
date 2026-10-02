package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The landings recorded for one Board item, read one way for both readers:
// the item (GET /v1/work/v2/items/<id>) and the broker's landings route
// (GET /v1/orchestrator/landings?work_id=<id>). Each is the broker's own
// record — a bound task's landing, or a root landing row — so the two answers
// cannot disagree about what landed (D01).

// Where a recorded landing comes from.
const (
	// LandingSourceTask is a bound broker task's landed or incorporated record.
	LandingSourceTask = "task"
	// LandingSourceRoot is a broker_root_landings row: a landing the owning
	// Session proved with no child task.
	LandingSourceRoot = "root"
	// LandingSourcePhaseEvent is a landing copy an older daemon wrote into the
	// item's phase event before root landings had rows. Read, never rewritten.
	LandingSourcePhaseEvent = "phase_event"
)

// ItemLandingV2 is one recorded landing of an item.
type ItemLandingV2 struct {
	// ID is the record's own id: the task id for a task landing, the row id
	// for a root landing, `event-<seq>` for a legacy copy.
	ID           string
	Source       string
	Task         string
	State        string
	Repository   string
	Target       string
	Commit       string
	TargetCommit string
	Remote       string
	RemoteCommit string
	RecordedAt   time.Time
}

// LandingStateUnknown is a bound task whose record could not be decoded: the
// landing it may hold cannot be told from none, so the list says so in a row
// of its own rather than reading shorter.
const LandingStateUnknown = "unknown"

// itemLandings composes the three sources.
func itemLandings(item work.ItemV2, rows []store.BrokerRow, roots []store.RootLanding,
	legacy []store.LegacyLanding) []ItemLandingV2 {
	out := []ItemLandingV2{}
	for _, row := range rows {
		r, err := orchestrator.Decode(row.Record)
		if err != nil {
			out = append(out, ItemLandingV2{ID: row.ID, Source: LandingSourceTask, Task: row.ID, State: LandingStateUnknown})
			continue
		}
		l := r.Landing
		if l == nil || (l.State != orchestrator.LandingLanded && l.State != orchestrator.LandingIncorporated) {
			continue
		}
		repo := l.Repo
		if repo == "" && r.Worktree != nil {
			repo = r.Worktree.Repository
		}
		if repo == "" {
			repo = item.ProjectPath
		}
		out = append(out, ItemLandingV2{ID: r.ID, Source: LandingSourceTask, Task: r.ID, State: string(l.State),
			Repository: repo, Target: l.Target, Commit: l.Commit, TargetCommit: l.TargetCommit, RecordedAt: l.At})
	}
	for _, l := range roots {
		out = append(out, ItemLandingV2{ID: l.ID, Source: LandingSourceRoot, State: string(orchestrator.LandingLanded),
			Repository: l.Repository, Target: l.Target, Commit: l.Commit, TargetCommit: l.TargetCommit,
			Remote: l.Remote, RemoteCommit: l.RemoteCommit, RecordedAt: l.RecordedAt})
	}
	for _, e := range legacy {
		var change struct {
			Landing *VerifiedLandingV2 `json:"landing"`
		}
		if err := json.Unmarshal([]byte(e.Payload), &change); err != nil || !change.Landing.complete() {
			// The store selected only payloads whose landing is an object; one
			// that does not decode into a whole proof is not a landing.
			continue
		}
		repo := change.Landing.Repository
		if repo == "" {
			repo = item.ProjectPath
		}
		out = append(out, ItemLandingV2{ID: fmt.Sprintf("event-%d", e.Seq), Source: LandingSourcePhaseEvent,
			State: string(orchestrator.LandingLanded), Repository: repo, Target: change.Landing.Target,
			Commit: change.Landing.Commit, TargetCommit: change.Landing.TargetCommit, Remote: change.Landing.Remote,
			RemoteCommit: change.Landing.RemoteCommit, RecordedAt: e.At})
	}
	return out
}

// readItemLandings reads an item's landings outside a write.
func (w *WorkSystemV2) readItemLandings(ctx context.Context, item work.ItemV2) ([]ItemLandingV2, error) {
	rows, err := w.Store.WorkV2Tasks(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	roots, err := w.Store.RootLandings(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	legacy, err := w.Store.LegacyLandings(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	return itemLandings(item, rows, roots, legacy), nil
}

// ItemLandings is every landing recorded for one item: what
// `clawdline landings --work-id` lists. An item that does not exist is
// work_not_found, not an empty list.
func (w *WorkSystemV2) ItemLandings(ctx context.Context, id string) ([]ItemLandingV2, error) {
	item, err := w.Store.WorkV2Item(ctx, id)
	if err != nil {
		return nil, mapWorkV2Error(err)
	}
	out, err := w.readItemLandings(ctx, item)
	return out, mapWorkV2Error(err)
}
