package orchestrator

import (
	"context"
	"fmt"
	"sort"

	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// What this broker's records say a session still owes (docs/design-decisions.md
// D01).
//
// A session's "N things not yet settled", and the refusal to close it, used to
// read a table of their own — `obligations`, written by the older dispatch
// skeleton and by nothing the broker did. So a root whose child had delivered
// into the shared tree, and had not had that delivery landed, read as free to
// close: the one fact that makes it not free lived in the broker's landing
// record, which that table never saw. Here the landing record is read, and
// nothing is copied out of it: each obligation below is a projection of one
// record, recomputed at every read, and it closes the moment the record does.

// ErrOwedIncomplete is a ledger that holds rows it cannot decode. Any one of
// them may be a landing some session owes, so the list without them is not the
// list (DG-7): a caller reads it as unknown, never as "owes nothing".
type ErrOwedIncomplete struct{ Unreadable int }

func (e ErrOwedIncomplete) Error() string {
	return fmt.Sprintf("%d stored task(s) could not be read, so what is owed is not known", e.Unreadable)
}

// Owed is one obligation per task whose landing is still pending, oldest
// first, with the session that owes it named against the reading given.
//
// The mover is the session that must land it, by the terminal it is in now:
// the root's conversation is looked for in `sessions`, and every terminal that
// proves it is named — two terminals claiming one conversation both owe it,
// because closing either on a guess is closing the one that was working. A
// root not in the reading is named by its conversation id, which no terminal
// id can be mistaken for. A task with no root — a schedule's run — owes its
// landing to a person: no session can be asked to record it.
func (b *Broker) Owed(ctx context.Context, sessions []session.Session) ([]task.Obligation, error) {
	records, unreadable, err := b.Records(ctx)
	if err != nil {
		return nil, err
	}
	if len(unreadable) > 0 {
		return nil, ErrOwedIncomplete{Unreadable: len(unreadable)}
	}
	out := []task.Obligation{}
	for _, r := range records {
		if r.Landing == nil || r.Landing.State != LandingPending {
			continue
		}
		note := r.Landing.Note
		if split := b.Obligation(r); split != "" {
			note = string(split) + ": " + note
		}
		base := task.Obligation{
			ID:       "landing:" + r.ID,
			Kind:     task.KindLanding,
			Subject:  r.ID,
			OpenedAt: r.FinishedAt,
			Evidence: task.EvidenceObserved,
			Note:     note,
		}
		if base.OpenedAt.IsZero() {
			base.OpenedAt = r.CreatedAt
		}
		for _, mover := range landingMovers(r, sessions) {
			o := base
			o.Mover = mover
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OpenedAt.Before(out[j].OpenedAt) })
	return out, nil
}

// landingMovers is who has to record one pending landing.
func landingMovers(r Record, sessions []session.Session) []task.Mover {
	if r.Root == nil || r.Root.SessionID == "" {
		return []task.Mover{{Kind: task.MoverPerson}}
	}
	movers := []task.Mover{}
	for _, s := range sessions {
		if !s.IsAssistant() || s.ConversationID != r.Root.SessionID {
			continue
		}
		if r.Root.Assistant != "" && string(s.Assistant) != r.Root.Assistant {
			continue
		}
		movers = append(movers, task.Mover{Kind: task.MoverThisSession, ID: s.ID})
	}
	if len(movers) == 0 {
		movers = append(movers, task.Mover{Kind: task.MoverThisSession, ID: r.Root.SessionID})
	}
	return movers
}

// LiveRecords is every task that has not finished, for a reader that shows
// them — never a row it could not decode, which the task list names apart.
func (b *Broker) LiveRecords(ctx context.Context) ([]Record, error) {
	return b.liveTasks(ctx)
}
