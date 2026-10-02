package app

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Did handing over at milestones pay (docs/token-ledger.md, "Did handing
// over pay"). The unit is one finished Board item, and its bill is what its
// owner Sessions re-read: each Session's own cache reads, shared equally
// among the finished items it owned in the range. A long Root that finished
// ten items carries a tenth of its cache reads on each. The groups are the
// items a milestone handoff carried, the items an ordinary handoff carried,
// and the items one Root saw through alone.
//
// The answer never turns handing over on. It says recommend_default only
// when both groups have enough items, the milestone group re-read at least
// 30% less per item, and none of the guardrails slipped; otherwise it says
// why, and the handoff stays the Session's own choice.

const (
	// handoffComparableLimit is the fewest items each compared group needs
	// before a saving is computed. Below it the answer is
	// insufficient_evidence and the saving is null.
	handoffComparableLimit = 20
	// handoffItemLimit is how many finished items one answer reads, the
	// newest first; past it the answer says truncated.
	handoffItemLimit = 300
	// handoffSavingTarget is the saving per item the milestone group must
	// reach before it is recommended as the default.
	handoffSavingTarget = 0.30
	// handoffElapsedTolerance is how much longer, as a ratio of medians, the
	// milestone group's items may take from first owner to done.
	handoffElapsedTolerance = 1.10
	// handoffSettleTime is how long after a handoff its carried tasks are
	// expected to be acknowledged and landed by the sender.
	handoffSettleTime = 24 * time.Hour
)

// The groups, in the order they are listed.
const (
	HandoffGroupSingle    = "single_root"
	HandoffGroupMilestone = "milestone_handoff"
	HandoffGroupPlain     = "plain_handoff"
)

// The verdicts.
const (
	HandoffVerdictInsufficient = "insufficient_evidence"
	HandoffVerdictBelowTarget  = "below_target"
	HandoffVerdictGuardrail    = "guardrail_failed"
	HandoffVerdictRecommend    = "recommend_default"
)

// HandoffUnit is one finished item as the comparison counts it.
type HandoffUnit struct {
	ItemID string
	Group  string
	// CacheRead and Calls are the owner Sessions' own, each Session's shared
	// equally among the finished items it owned in the range.
	CacheRead float64
	Calls     float64
	// Elapsed is from the first owner's assignment to done.
	Elapsed  time.Duration
	Reopened bool
	// Excluded is why the unit is in no group: unowned, or unread.
	Excluded string
}

// HandoffGroup is one group's medians and guardrails.
type HandoffGroup struct {
	Name           string   `json:"name"`
	Items          int      `json:"items"`
	TooFew         bool     `json:"too_few"`
	CacheReadItem  *float64 `json:"cache_read_per_item"`
	CallsItem      *float64 `json:"calls_per_item"`
	ElapsedHours   *float64 `json:"elapsed_hours"`
	ReopenedShare  *float64 `json:"reopened_share"`
	ReopenedCount  int      `json:"reopened"`
	CacheReadTotal float64  `json:"cache_read_total"`
}

// HandoffComparison is the answer.
type HandoffComparison struct {
	Since     time.Time      `json:"since"`
	Until     time.Time      `json:"until"`
	Groups    []HandoffGroup `json:"groups"`
	Excluded  map[string]int `json:"excluded"`
	Truncated bool           `json:"truncated,omitempty"`
	// Handoffs counts the milestone handoffs opened in the range, those that
	// failed to open, and the tasks they carried that the sender had still
	// not settled a day later.
	MilestoneHandoffs int `json:"milestone_handoffs"`
	FailedHandoffs    int `json:"failed_handoffs"`
	CarriedUnsettled  int `json:"carried_unsettled"`
	// Saving is 1 - milestone/single cache reads per item, when both groups
	// have enough items.
	Saving      *float64 `json:"saving"`
	Target      float64  `json:"target"`
	MinItems    int      `json:"min_items"`
	Verdict     string   `json:"verdict"`
	Reasons     []string `json:"reasons"`
	NotMeasured []string `json:"not_measured"`
}

// handoffNotMeasured is what the acceptance asks about and the daemon has no
// record of: say so rather than show a zero.
var handoffNotMeasured = []string{
	"re-asks: questions the receiver put to the person that the summary should have answered are not recorded apart from other questions",
	"re-reads: which evidence files a receiver opened again is not in the ledger, only its total cache reads",
}

// CompareHandoffSince is CompareHandoff for a `since` as the route takes it.
func (u *UsageLedger) CompareHandoffSince(ctx context.Context, raw string) (HandoffComparison, error) {
	since, err := ParseCompareSince(raw, u.now())
	if err != nil {
		return HandoffComparison{}, err
	}
	return u.CompareHandoff(ctx, since)
}

// CompareHandoff reads the items finished since since and the handoffs
// opened since then, and folds them.
func (u *UsageLedger) CompareHandoff(ctx context.Context, since time.Time) (HandoffComparison, error) {
	until := u.now()
	items, more, err := u.Store.WorkV2DoneSince(ctx, since, handoffItemLimit)
	if err != nil {
		return HandoffComparison{}, err
	}
	handoffs, err := u.recentHandoffs(ctx)
	if err != nil {
		return HandoffComparison{}, err
	}
	type owned struct {
		item      work.ItemV2
		sessions  []string
		group     string
		firstFrom time.Time
		excluded  string
	}
	var rows []owned
	share := map[string]int{}
	for _, item := range items {
		assignments, err := u.Store.WorkV2Assignments(ctx, item.ID)
		if err != nil {
			return HandoffComparison{}, err
		}
		o := owned{item: item, group: HandoffGroupSingle}
		var unresolved []string
		seen := map[string]bool{}
		for _, a := range assignments {
			if a.SessionID == "" && a.RootAssignment == "" {
				continue
			}
			if o.firstFrom.IsZero() || a.CreatedAt.Before(o.firstFrom) {
				o.firstFrom = a.CreatedAt
			}
			if id, ok := strings.CutPrefix(a.HumanActor, "handoff:"); ok {
				if h, found := handoffs[id]; found && h.Milestone {
					o.group = HandoffGroupMilestone
				} else if o.group != HandoffGroupMilestone {
					o.group = HandoffGroupPlain
				}
			}
			switch {
			case a.SessionID != "" && !seen[a.SessionID]:
				seen[a.SessionID] = true
				o.sessions = append(o.sessions, a.SessionID)
			case a.SessionID == "":
				unresolved = append(unresolved, a.RootAssignment)
			}
		}
		if len(unresolved) > 0 {
			opened, err := u.Store.UsageRowsForRootAssignments(ctx, unresolved)
			if err != nil {
				return HandoffComparison{}, err
			}
			for _, r := range ownRows(opened) {
				if !seen[r.Conversation] {
					seen[r.Conversation] = true
					o.sessions = append(o.sessions, r.Conversation)
				}
			}
		}
		if len(o.sessions) == 0 {
			o.excluded = "unowned"
		}
		for _, s := range o.sessions {
			share[s]++
		}
		rows = append(rows, o)
	}

	var all []string
	for s := range share {
		all = append(all, s)
	}
	sort.Strings(all)
	usage, err := u.Store.UsageRowsForConversations(ctx, all)
	if err != nil {
		return HandoffComparison{}, err
	}
	bySession := map[string]SessionUsage{}
	for _, s := range all {
		var own []store.UsageRow
		for _, r := range ownRows(usage) {
			if r.Conversation == s {
				own = append(own, r)
			}
		}
		bySession[s] = FoldSession(s, own, nil)
	}

	var units []HandoffUnit
	for _, o := range rows {
		unit := HandoffUnit{ItemID: o.item.ID, Group: o.group, Reopened: o.item.Cycle > 1, Excluded: o.excluded}
		if !o.firstFrom.IsZero() && !o.item.ClosedAt.IsZero() {
			unit.Elapsed = o.item.ClosedAt.Sub(o.firstFrom)
		}
		for _, s := range o.sessions {
			su := bySession[s]
			if !su.counted() {
				unit.Excluded = "unread"
				break
			}
			unit.CacheRead += su.Totals.Measured.CacheRead / float64(share[s])
			unit.Calls += float64(su.Calls) / float64(share[s])
		}
		units = append(units, unit)
	}

	out := FoldHandoffComparison(units)
	out.Since, out.Until, out.Truncated = since, until, more
	for _, h := range handoffs {
		if !h.Milestone || time.Unix(h.CreatedAt, 0).Before(since) {
			continue
		}
		out.MilestoneHandoffs++
		if h.State == orchestrator.HandoffSpawnFailed {
			out.FailedHandoffs++
		}
	}
	out.CarriedUnsettled, err = u.carriedUnsettled(ctx, handoffs, since, until)
	if err != nil {
		return HandoffComparison{}, err
	}
	decideHandoff(&out)
	return out, nil
}

// recentHandoffs is the newest handoffs, by id, whenever they were opened: an
// item finished in the range may have been carried by an older one, and must
// not be miscounted as plain for want of its record.
func (u *UsageLedger) recentHandoffs(ctx context.Context) (map[string]orchestrator.Handoff, error) {
	rows, err := u.Store.ListOpened(ctx, store.TableHandoffs, handoffItemLimit)
	if err != nil {
		return nil, err
	}
	out := map[string]orchestrator.Handoff{}
	for _, row := range rows {
		var h orchestrator.Handoff
		if json.Unmarshal(row.Record, &h) != nil {
			continue
		}
		if h.ID == "" {
			h.ID = row.ID
		}
		if h.State == "" {
			h.State = row.State
		}
		h.CreatedAt = row.CreatedAt.Unix()
		out[h.ID] = h
	}
	return out, nil
}

// carriedUnsettled counts the tasks milestone handoffs carried that, a day
// after the handoff, are still running or still owe a landing: the receipts
// the sender was meant to keep and settle before it closed.
func (u *UsageLedger) carriedUnsettled(ctx context.Context, handoffs map[string]orchestrator.Handoff, since, now time.Time) (int, error) {
	var ids []string
	for _, h := range handoffs {
		created := time.Unix(h.CreatedAt, 0)
		if h.Milestone && !created.Before(since) && now.Sub(created) >= handoffSettleTime {
			ids = append(ids, h.Carried...)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	records, err := u.Store.BrokerTaskRecords(ctx, ids)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		row, ok := records[id]
		if !ok {
			continue
		}
		r, err := orchestrator.Decode(row.Record)
		if err != nil {
			continue
		}
		if !r.State.Terminal() || (r.Landing != nil && r.Landing.State == orchestrator.LandingPending) {
			n++
		}
	}
	return n, nil
}

// FoldHandoffComparison groups the units and takes each group's medians.
// It is pure; CompareHandoff reads, this counts.
func FoldHandoffComparison(units []HandoffUnit) HandoffComparison {
	out := HandoffComparison{Excluded: map[string]int{}, Target: handoffSavingTarget, MinItems: handoffComparableLimit,
		Reasons: []string{}, NotMeasured: handoffNotMeasured}
	by := map[string][]HandoffUnit{}
	for _, unit := range units {
		if unit.Excluded != "" {
			out.Excluded[unit.Excluded]++
			continue
		}
		by[unit.Group] = append(by[unit.Group], unit)
	}
	for _, name := range []string{HandoffGroupSingle, HandoffGroupMilestone, HandoffGroupPlain} {
		list := by[name]
		g := HandoffGroup{Name: name, Items: len(list), TooFew: len(list) < handoffComparableLimit}
		var cache, calls, hours []float64
		for _, unit := range list {
			cache = append(cache, unit.CacheRead)
			calls = append(calls, unit.Calls)
			g.CacheReadTotal += unit.CacheRead
			if unit.Elapsed > 0 {
				hours = append(hours, unit.Elapsed.Hours())
			}
			if unit.Reopened {
				g.ReopenedCount++
			}
		}
		if len(list) > 0 {
			g.CacheReadItem = floatPtr(medianFloat(cache))
			g.CallsItem = floatPtr(medianFloat(calls))
			g.ReopenedShare = floatPtr(float64(g.ReopenedCount) / float64(len(list)))
		}
		if len(hours) > 0 {
			g.ElapsedHours = floatPtr(medianFloat(hours))
		}
		out.Groups = append(out.Groups, g)
	}
	decideHandoff(&out)
	return out
}

// decideHandoff sets the saving and the verdict from the groups and the
// handoff guardrails. It is called again once those are counted.
func decideHandoff(out *HandoffComparison) {
	var single, milestone HandoffGroup
	for _, g := range out.Groups {
		switch g.Name {
		case HandoffGroupSingle:
			single = g
		case HandoffGroupMilestone:
			milestone = g
		}
	}
	out.Saving, out.Reasons = nil, []string{}
	if single.TooFew || milestone.TooFew {
		out.Verdict = HandoffVerdictInsufficient
		out.Reasons = append(out.Reasons, "fewer than 20 comparable items in a group: "+
			single.Name+" "+strconv.Itoa(single.Items)+", "+milestone.Name+" "+strconv.Itoa(milestone.Items))
		return
	}
	if single.CacheReadItem == nil || *single.CacheReadItem <= 0 || milestone.CacheReadItem == nil {
		out.Verdict = HandoffVerdictInsufficient
		out.Reasons = append(out.Reasons, "no cache reads were measured for the single-Root group")
		return
	}
	saving := 1 - *milestone.CacheReadItem / *single.CacheReadItem
	out.Saving = &saving
	var slipped []string
	if milestone.ReopenedShare != nil && single.ReopenedShare != nil && *milestone.ReopenedShare > *single.ReopenedShare {
		slipped = append(slipped, "more milestone items were reopened")
	}
	if milestone.ElapsedHours != nil && single.ElapsedHours != nil && *milestone.ElapsedHours > *single.ElapsedHours*handoffElapsedTolerance {
		slipped = append(slipped, "milestone items took more than 10% longer to finish")
	}
	if out.FailedHandoffs > 0 {
		slipped = append(slipped, strconv.Itoa(out.FailedHandoffs)+" milestone handoffs failed to open")
	}
	if out.CarriedUnsettled > 0 {
		slipped = append(slipped, strconv.Itoa(out.CarriedUnsettled)+" carried tasks were still unsettled a day after their handoff")
	}
	switch {
	case len(slipped) > 0:
		out.Verdict = HandoffVerdictGuardrail
		out.Reasons = append(out.Reasons, slipped...)
	case saving < handoffSavingTarget:
		out.Verdict = HandoffVerdictBelowTarget
		out.Reasons = append(out.Reasons, "the saving per item is under 30%")
	default:
		out.Verdict = HandoffVerdictRecommend
	}
}

func floatPtr(v float64) *float64 { return &v }
