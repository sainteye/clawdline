package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// A one-off rehearsal of the 26 proposals of 2026-09-20 against a store of its
// own, in the shapes measured from the live one: 6 rule proposals whose line
// landed, 17 rule proposals on I1 alone whose line is still owed, 3 leftovers
// whose subject nobody has dispatched anything for.
func TestFixtureTheTwentySix(t *testing.T) {
	p, _, st, clock := newParticipation(t)
	ctx := context.Background()
	rows := []struct{ id, kind string }{}
	n := 0
	mk := func(kind string, count int) {
		for i := 0; i < count; i++ {
			n++
			line := newWorkID()
			task := taskID(n)
			sent(t, st, task, line, "custom", theRoot, clock.at)
			owes(t, st, task, line, theRoot, clock.at)
			id := fmt.Sprintf("p-%02d", n)
			row := work.Proposal{ID: id, WorkID: line, TaskID: task, Session: theRoot,
				Source: work.SourceRule, Project: "/p", Title: "line " + task[len(task)-2:],
				Signals: []work.Signal{work.SignalCrossSession}, AskReason: work.AskByRule,
				Channel: work.ChannelToConfirm, State: work.ProposalPending, CreatedAt: clock.at,
				ExpiresAt: clock.at.Add(p.Proposals.Expiry)}
			if kind == "leftover" {
				// The subject is a line nobody dispatched anything for; the
				// task is only provenance (PT-9).
				row.WorkID, row.Source, row.Signals = newWorkID(), work.SourceSession,
					[]work.Signal{work.SignalLeftover}
				row.AskReason = work.AskHumanAbsent
			}
			insert(t, p, row)
			rows = append(rows, struct{ id, kind string }{id, kind})
			if kind == "landed" {
				lands(t, st, task, line, theRoot, clock.at.Add(time.Hour))
			}
		}
	}
	mk("landed", 6)
	mk("owed", 17)
	mk("leftover", 3)
	page, _ := p.ProposalList(ctx, work.ProposalPending, "", "")
	fmt.Printf("\nbefore the sweep: pending=%d\n", page.Counts[work.ProposalPending])
	clock.at = clock.at.Add(2 * time.Hour)
	if err := p.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	after := map[string]map[string]int{}
	for _, r := range rows {
		got, err := p.Proposal(ctx, r.id)
		if err != nil {
			t.Fatal(err)
		}
		key := string(got.State)
		if got.WithdrawnReason != "" {
			key += " / " + got.WithdrawnReason
		}
		if after[r.kind] == nil {
			after[r.kind] = map[string]int{}
		}
		after[r.kind][key]++
	}
	for _, kind := range []string{"landed", "owed", "leftover"} {
		fmt.Printf("  %-9s -> %v\n", kind, after[kind])
	}
	page, _ = p.ProposalList(ctx, work.ProposalPending, "", "")
	gone, _ := p.ProposalList(ctx, work.ProposalWithdrawn, "", "")
	fmt.Printf("after the sweep:  pending=%d withdrawn=%d (deleted=0)\n\n",
		page.Counts[work.ProposalPending], gone.Counts[work.ProposalWithdrawn])
	if page.Counts[work.ProposalPending] != 3 || gone.Counts[work.ProposalWithdrawn] != 23 {
		t.Fatalf("26 -> %d pending, %d withdrawn", page.Counts[work.ProposalPending],
			gone.Counts[work.ProposalWithdrawn])
	}
}

// insert writes one proposal as the store held it before this change.
func insert(t *testing.T, p *Participation, row work.Proposal) {
	t.Helper()
	if err := p.Board.Store.WriteWork(context.Background(), func(tx *store.WorkTx) error {
		return tx.PutProposal(row, nil, 0)
	}); err != nil {
		t.Fatal(err)
	}
}
