package projectassistanthost

import (
	"context"
	"encoding/json"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// hostCharters delivers each Project's team charter from the Host's own
// store: every prompt reads the current version with the Bot's own
// playbooks, and every sent prompt records which delivery version it carried
// for the charter's readers.
type hostCharters struct {
	store *featurestore.Store
	clock func() time.Time
}

func (c hostCharters) CurrentCharter(ctx context.Context, project pa.ProjectScope) (int64, string, error) {
	v, err := c.store.Charter(ctx, project.MachineID, project.ProjectID)
	return v.Version, v.Text, err
}

// DeliveredCharter is the charter one Bot hears. A Bot's private background
// helper works for its owner and hears the owner's playbooks.
func (c hostCharters) DeliveredCharter(ctx context.Context, bot pa.Bot) (int64, string, error) {
	v, err := c.store.Charter(ctx, bot.Project.MachineID, bot.Project.ProjectID)
	if err != nil || v.Version <= 0 {
		return 0, "", err
	}
	reader := bot.Key.BotID
	if bot.Hidden {
		owner, err := c.store.BackgroundOwner(ctx, bot.Key)
		if err != nil {
			return 0, "", err
		}
		if owner.BotID != "" {
			reader = owner.BotID
		}
	}
	// The workflow shows each Bot by the display name it has now.
	var names map[string]string
	if v.Workflow != nil {
		bots, err := c.store.CharterBots(ctx, bot.Project.MachineID)
		if err != nil {
			return 0, "", err
		}
		names = map[string]string{}
		for _, b := range bots {
			names[b.BotID] = b.Name
		}
	}
	return v.DeliveryVersion(reader), v.DeliveredTextNamed(reader, func(id string) string { return names[id] }), nil
}

// CurrentCharterWorkflow is a Project's current workflow as JSON for a lead
// proposing a change to it; "null" when it has none.
func (c hostCharters) CurrentCharterWorkflow(ctx context.Context, project pa.ProjectScope) (int64, string, error) {
	v, err := c.store.Charter(ctx, project.MachineID, project.ProjectID)
	if err != nil {
		return 0, "", err
	}
	if v.Workflow == nil {
		return v.Version, "null", nil
	}
	b, err := json.Marshal(v.Workflow)
	return v.Version, string(b), err
}

func (c hostCharters) CharterDelivered(ctx context.Context, bot pa.Bot, turnID string, version int64) error {
	now := time.Now()
	if c.clock != nil {
		now = c.clock()
	}
	return c.store.RecordCharterRead(ctx, bot.Project.MachineID, bot.Project.ProjectID, bot.Key.BotID, version, turnID, now)
}

// CharterChecked notes that the Bot was asked to compare this version.
func (c hostCharters) CharterChecked(ctx context.Context, bot pa.Bot, turnID string, version int64) error {
	now := time.Now()
	if c.clock != nil {
		now = c.clock()
	}
	return c.store.RecordCharterCheck(ctx, bot.Project.MachineID, bot.Project.ProjectID, bot.Key.BotID, version, turnID, now)
}

// bindCharterConflictTool lets every Bot whose Project has a team charter
// report its own sentences that the charter contradicts, in any turn: the
// charter can first reach a Bot in a request from another Bot. A Bot's
// background helper reports for the Bot it works for. A report changes
// nothing until the person chooses on the charter page.
func (s *toolScope) bindCharterConflictTool(tools *NativeWork) {
	h, q, turnID, taskOwner, life, bound, journal := s.h, s.q, s.turnID, s.taskOwner, s.life, s.bound, s.journal
	if s.turn.Origin == pa.CompactOrigin {
		return
	}
	reporter := q.Bot.Key
	if taskOwner.BotID != "" {
		reporter = taskOwner
	}
	project := q.Bot.Project
	current := func(ctx context.Context) (int64, error) {
		v, err := h.store.Charter(ctx, project.MachineID, project.ProjectID)
		if err == nil && v.Version <= 0 {
			err = failure(pa.NotFound, "charter")
		}
		return v.Version, err
	}
	tools.conflictsAvailable = func(ctx context.Context) error {
		ctx, done, err := bound(ctx)
		if err != nil {
			return err
		}
		defer done()
		_, err = current(ctx)
		return err
	}
	tools.reportConflict = func(ctx context.Context, r app.CharterConflictReport) (pa.CharterConflict, error) {
		ctx, done, err := bound(ctx)
		if err != nil {
			return pa.CharterConflict{}, err
		}
		stop := context.AfterFunc(life.sealed, done)
		defer func() { stop(); done() }()
		if _, err = current(ctx); err != nil {
			return pa.CharterConflict{}, err
		}
		c := pa.CharterConflict{ProjectID: project.ProjectID, BotID: reporter.BotID, TurnID: turnID, Version: r.CharterVersion, Source: pa.CharterConflictSource(r.Source),
			Path: r.Path, MemoryEntryID: r.MemoryEntryID, BotSentence: r.BotSentence, CharterSentence: r.CharterSentence, Section: r.Section, Suggested: r.Suggested, CreatedAt: h.o.Clock()}
		if err = pa.ValidateCharterConflict(c); err != nil {
			return pa.CharterConflict{}, err
		}
		if err = journal(ctx, "reportCharterConflict"); err != nil {
			return pa.CharterConflict{}, err
		}
		return h.store.ReportCharterConflict(ctx, project.MachineID, c)
	}
}

// bindCharterTool lets the lead Bot propose a charter in a direct turn from
// the person, which includes a change request sent from a charter page. The
// role is checked again on every call; the proposal waits for the person.
func (s *toolScope) bindCharterTool(tools *NativeWork) {
	h, q, turn, turnID, taskOwner, life, bound, journal := s.h, s.q, s.turn, s.turnID, s.taskOwner, s.life, s.bound, s.journal
	if taskOwner.BotID != "" || !pa.DirectUserTurn(turn) {
		return
	}
	lead := func(ctx context.Context) error {
		bot, err := h.store.Bot(ctx, q.Bot.Key)
		if err != nil {
			return err
		}
		if !bot.Lead {
			return failure(pa.ScopeDenied, "lead_role")
		}
		return nil
	}
	tools.charterAvailable = func(ctx context.Context) error {
		ctx, done, err := bound(ctx)
		if err != nil {
			return err
		}
		defer done()
		return lead(ctx)
	}
	tools.proposeCharter = func(ctx context.Context, p app.CharterEditProposal) (pa.CharterProposal, error) {
		ctx, done, err := bound(ctx)
		if err != nil {
			return pa.CharterProposal{}, err
		}
		stop := context.AfterFunc(life.sealed, done)
		defer func() { stop(); done() }()
		if err = lead(ctx); err != nil {
			return pa.CharterProposal{}, err
		}
		if err = pa.ValidateCharterProject(p.Project); err != nil {
			return pa.CharterProposal{}, err
		}
		// The Project must be one of this machine's; the text names it, it
		// does not grant anything.
		selection := pa.ProjectSelection{MachineID: q.Bot.Key.MachineID, ProjectID: p.Project}
		resolution, err := h.o.Projects.Resolve(ctx, selection)
		if err == nil {
			err = pa.ValidateTarget(selection, resolution)
		}
		if err != nil {
			return pa.CharterProposal{}, err
		}
		if err = journal(ctx, "proposeCharter"); err != nil {
			return pa.CharterProposal{}, err
		}
		return h.store.ProposeCharter(ctx, q.Bot.Key.MachineID, pa.CharterProposal{ProjectID: p.Project, BaseVersion: p.BaseVersion,
			Text: p.NewText, Summary: p.Summary, BotID: q.Bot.Key.BotID, TurnID: turnID, CreatedAt: h.o.Clock()})
	}
	tools.proposeWorkflow = func(ctx context.Context, p app.CharterWorkflowProposal) (pa.CharterProposal, error) {
		ctx, done, err := bound(ctx)
		if err != nil {
			return pa.CharterProposal{}, err
		}
		stop := context.AfterFunc(life.sealed, done)
		defer func() { stop(); done() }()
		if err = lead(ctx); err != nil {
			return pa.CharterProposal{}, err
		}
		if err = pa.ValidateCharterProject(p.Project); err != nil {
			return pa.CharterProposal{}, err
		}
		selection := pa.ProjectSelection{MachineID: q.Bot.Key.MachineID, ProjectID: p.Project}
		resolution, err := h.o.Projects.Resolve(ctx, selection)
		if err == nil {
			err = pa.ValidateTarget(selection, resolution)
		}
		if err != nil {
			return pa.CharterProposal{}, err
		}
		if err = journal(ctx, "proposeCharterWorkflow"); err != nil {
			return pa.CharterProposal{}, err
		}
		flow := p.Workflow
		return h.store.ProposeCharter(ctx, q.Bot.Key.MachineID, pa.CharterProposal{ProjectID: p.Project, BaseVersion: p.BaseVersion, Kind: pa.CharterProposalWorkflow,
			Workflow: &flow, Summary: p.Summary, BotID: q.Bot.Key.BotID, TurnID: turnID, CreatedAt: h.o.Clock()})
	}
}
