package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Charter conflicts are the Bots' own reports that a sentence of theirs
// disagrees with the team charter (docs/bots.md, "Team charter"). The person
// reads them on the charter page and chooses, for each, to follow the
// charter, change the charter or leave it.

func charterConflictLimits() contract.ProjectAssistantCharterConflictLimits {
	return contract.ProjectAssistantCharterConflictLimits{Open: pa.MaxCharterConflicts, Resolved: pa.MaxCharterConflictsResolved, SentenceBytes: pa.MaxCharterConflictSentenceBytes}
}

func (s *Server) charterConflictsAnswer(ctx context.Context, machine, project string) (contract.ProjectAssistantCharterConflictsResponse, error) {
	store := s.projectAssistantFloorsStore()
	out := contract.ProjectAssistantCharterConflictsResponse{ContractVersion: pa.ContractVersion, MachineID: machine, ProjectID: project, Conflicts: []contract.ProjectAssistantCharterConflict{}, Limits: charterConflictLimits()}
	current, err := store.Charter(ctx, machine, project)
	if err != nil {
		return out, err
	}
	out.Version = current.Version
	conflicts, err := store.CharterConflicts(ctx, machine, project)
	if err != nil {
		return out, err
	}
	bots, err := store.CharterBots(ctx, machine)
	if err != nil {
		return out, err
	}
	checks, err := store.CharterChecks(ctx, machine, project)
	if err != nil {
		return out, err
	}
	// Each Bot compares the charter as it heard it, so its reports and checks
	// are current at its own delivery version.
	reported, err := store.ReportedVersions(ctx, machine, project)
	if err != nil {
		return out, err
	}
	names := map[string]string{}
	for _, b := range bots {
		if b.Hidden {
			continue
		}
		names[b.BotID] = b.Name
		delivered := current.DeliveryVersion(b.BotID)
		if b.ProjectID == project && delivered > 0 && checks[b.BotID] == delivered && !reported[b.BotID][delivered] {
			out.CheckedWithoutReport++
		}
	}
	for _, c := range conflicts {
		resolved := ""
		if !c.ResolvedAt.IsZero() {
			resolved = charterTime(c.ResolvedAt)
		}
		out.Conflicts = append(out.Conflicts, contract.ProjectAssistantCharterConflict{ID: c.ID, BotID: c.BotID, BotName: names[c.BotID], TurnID: c.TurnID, Version: c.Version,
			Outdated: c.Version != current.DeliveryVersion(c.BotID), Source: contract.ProjectAssistantCharterConflictSource(c.Source), Path: c.Path, MemoryEntryID: c.MemoryEntryID,
			BotSentence: c.BotSentence, CharterSentence: c.CharterSentence, Section: c.Section, Suggested: c.Suggested,
			State: contract.ProjectAssistantCharterConflictState(c.State), CreatedAt: charterTime(c.CreatedAt), ResolvedAt: resolved})
	}
	return out, nil
}

func (s *Server) charterConflicts(r *http.Request) (contract.ProjectAssistantCharterConflictsResponse, error) {
	machine, project, after, err := s.charterReadScope(r)
	if err != nil {
		return contract.ProjectAssistantCharterConflictsResponse{}, err
	}
	out, err := s.charterConflictsAnswer(r.Context(), machine, project)
	if err != nil {
		return out, err
	}
	return out, after()
}

// The plans a follow_charter choice can take. The plan is chosen and stored
// before anything changes, so a repeated choice finishes the same one.
const (
	planDismiss      = "dismiss"
	planCharter      = "charter"
	planInstructions = "instructions"
	planMemory       = "memory:"
	planPlaybook     = "playbook"
	planBot          = "bot"
)

func (s *Server) charterConflictResolve(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterConflictResolveRequest](w, r)
	if err != nil {
		return nil, err
	}
	action := pa.CharterConflictAction(body.Action)
	if action != pa.FollowCharter && action != pa.ChangeCharter && action != pa.DismissReport {
		return nil, paHTTPFailure(pa.Invalid, "action")
	}
	if body.ConflictID == "" {
		return nil, paHTTPFailure(pa.Invalid, "conflict_id")
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		return s.resolveCharterConflict(ctx, body.MachineID, body.ProjectID, body.ConflictID, action, body.IdempotencyKey)
	}, func(ctx context.Context) (any, error) {
		return s.charterConflictsAnswer(ctx, body.MachineID, body.ProjectID)
	})
}

func (s *Server) resolveCharterConflict(ctx context.Context, machine, project, id string, action pa.CharterConflictAction, idem string) error {
	store := s.projectAssistantFloorsStore()
	c, err := store.CharterConflict(ctx, machine, project, id)
	if err != nil {
		return err
	}
	var service *app.Service
	plan := planDismiss
	switch action {
	case pa.ChangeCharter:
		plan = planCharter
	case pa.FollowCharter:
		host, err := s.projectAssistantStartedHost(ctx)
		if err != nil {
			return err
		}
		if service, err = host.Service(); err != nil {
			return err
		}
		if plan, err = followCharterPlan(ctx, store, machine, c); err != nil {
			return err
		}
	}
	res, err := store.BeginConflictResolution(ctx, machine, project, id, action, idem, plan)
	if err != nil {
		return err
	}
	state := pa.ConflictDismissed
	switch res.Plan {
	case planCharter:
		state = pa.ConflictHandedToCharter
	case planDismiss:
	default:
		if state, err = followCharter(ctx, store, service, machine, res, idem); err != nil {
			return err
		}
	}
	return store.FinishConflictResolution(ctx, machine, project, id, state, time.Now())
}

// followCharterPlan decides who changes the Bot's sentence. The daemon itself
// replaces it in a short memory fact, the instructions or a charter playbook
// when the sentence appears there exactly once; a topic note, a workspace
// file, or a sentence that is not there exactly once goes to the Bot.
func followCharterPlan(ctx context.Context, store *projectassistantstore.Store, machine string, c pa.CharterConflict) (string, error) {
	key := pa.BotKey{MachineID: machine, BotID: c.BotID}
	switch c.Source {
	case pa.ConflictInInstructions:
		bot, err := store.Bot(ctx, key)
		if err != nil {
			return "", err
		}
		if strings.Count(bot.Instructions.Text, c.BotSentence) == 1 {
			return planInstructions, nil
		}
	case pa.ConflictInMemory:
		entry, ok, err := memoryFactHolding(ctx, store, key, c)
		if err != nil {
			return "", err
		}
		if ok {
			return planMemory + entry.ID, nil
		}
	case pa.ConflictInPlaybook:
		current, err := store.Charter(ctx, machine, c.ProjectID)
		if err != nil {
			return "", err
		}
		if p, ok := current.Playbook(c.Path); ok && strings.Count(p.Text, c.BotSentence) == 1 {
			return planPlaybook, nil
		}
	}
	return planBot, nil
}

// memoryFactHolding finds the one live short fact holding the sentence
// exactly once; ok is false when any topic note, or more or fewer than one
// place, holds it.
func memoryFactHolding(ctx context.Context, store *projectassistantstore.Store, key pa.BotKey, c pa.CharterConflict) (pa.MemoryEntry, bool, error) {
	entries, err := store.MemoryEntries(ctx, key)
	if err != nil {
		return pa.MemoryEntry{}, false, err
	}
	var found pa.MemoryEntry
	n := 0
	for _, e := range entries {
		if e.Forgotten || c.MemoryEntryID != "" && e.ID != c.MemoryEntryID {
			continue
		}
		count := strings.Count(e.Text+"\n"+e.Title+"\n"+e.Summary, c.BotSentence)
		if count == 0 {
			continue
		}
		if e.Kind == pa.TopicMemory {
			return pa.MemoryEntry{}, false, nil
		}
		n += count
		found = e
	}
	return found, n == 1, nil
}

// followCharter runs a follow_charter plan. A daemon write that finds the
// sentence already gone on a repeated choice has already happened; one that
// finds it changed since the plan was chosen hands the report to the Bot.
func followCharter(ctx context.Context, store *projectassistantstore.Store, service *app.Service, machine string, res projectassistantstore.ConflictResolution, idem string) (pa.CharterConflictState, error) {
	if service == nil {
		return "", paHTTPFailure(pa.Disabled, "host")
	}
	c := res.Conflict
	key := pa.BotKey{MachineID: machine, BotID: c.BotID}
	command := func(op pa.Operation) pa.Command {
		return pa.Command{Key: key, Operation: op, IdempotencyKey: "charter-fix-" + idem}
	}
	switch {
	case res.Plan == planInstructions:
		for attempt := 0; attempt < 3; attempt++ {
			bot, err := store.Bot(ctx, key)
			if err != nil {
				return "", err
			}
			text, n := pa.ReplaceExactlyOnce(bot.Instructions.Text, c.BotSentence, c.Suggested)
			if n == 0 && res.Repeated {
				return pa.ConflictFixed, nil
			}
			if n != 1 {
				break
			}
			_, _, err = service.Edit(ctx, command(pa.EditOperation), pa.EditBot{ExpectedVersion: bot.Version, Name: bot.Name, Appearance: bot.Appearance, Instructions: text})
			if err == nil {
				return pa.ConflictFixed, nil
			}
			if pa.Code(err) != pa.StaleVersion {
				return "", err
			}
		}
	case res.Plan == planPlaybook:
		// The playbook is the person's, so the replacement is a new charter
		// version written as theirs.
		for attempt := 0; attempt < 3; attempt++ {
			current, err := store.Charter(ctx, machine, c.ProjectID)
			if err != nil {
				return "", err
			}
			p, ok := current.Playbook(c.Path)
			text, n := pa.ReplaceExactlyOnce(p.Text, c.BotSentence, c.Suggested)
			if (!ok || n == 0) && res.Repeated {
				return pa.ConflictFixed, nil
			}
			if !ok || n != 1 {
				break
			}
			p.Text = text
			_, err = store.SaveCharterPlaybook(ctx, projectassistantstore.CharterPlaybookWrite{MachineID: machine, ProjectID: c.ProjectID, ExpectedVersion: current.Version,
				Playbook: p, Author: pa.CharterByPerson, IdempotencyKey: "charter-fix-" + idem, At: time.Now()})
			if err == nil {
				return pa.ConflictFixed, nil
			}
			if pa.Code(err) != pa.StaleVersion {
				return "", err
			}
		}
	case strings.HasPrefix(res.Plan, planMemory):
		id := strings.TrimPrefix(res.Plan, planMemory)
		entry, ok, err := memoryFactHolding(ctx, store, key, pa.CharterConflict{MemoryEntryID: id, BotSentence: c.BotSentence})
		if err != nil {
			return "", err
		}
		if !ok && res.Repeated {
			return pa.ConflictFixed, nil
		}
		if ok {
			bot, err := store.Bot(ctx, key)
			if err != nil {
				return "", err
			}
			revision, err := store.Current(ctx, key)
			if err != nil {
				return "", err
			}
			text, _ := pa.ReplaceExactlyOnce(entry.Text, c.BotSentence, c.Suggested)
			if strings.TrimSpace(text) == "" {
				_, err = service.Forget(ctx, app.ForgetRequest{Command: command(pa.ForgetOperation), Project: bot.Project, ExpectedRevision: revision.Revision, SourceIDs: []string{entry.ID}})
			} else {
				_, err = service.SaveMemory(ctx, app.MemoryWrite{Command: command(pa.MemorySaveOperation), ExpectedRevision: revision.Revision,
					Entry: pa.MemoryEntry{ID: entry.ID, Key: key, Project: bot.Project, Text: text}})
			}
			if err == nil {
				return pa.ConflictFixed, nil
			}
			if pa.Code(err) != pa.StaleVersion && pa.Code(err) != pa.ContextStale {
				return "", err
			}
		}
	}
	if err := sendCharterFix(ctx, service, command(pa.SendOperation), c); err != nil {
		return "", err
	}
	return pa.ConflictHandedToBot, nil
}

// sendCharterFix asks the Bot, as the person's message in its main chat, to
// change its own sentence.
func sendCharterFix(ctx context.Context, service *app.Service, command pa.Command, c pa.CharterConflict) error {
	bot, conversation, _, err := service.Get(ctx, command.Key)
	if err != nil {
		return err
	}
	fence := pa.Fence{Key: command.Key, Project: bot.Project, ConversationID: conversation.ID, BindingID: conversation.BindingID, Generation: conversation.Generation}
	_, err = service.SendCharterFix(ctx, command, fence, pa.FixFor(c))
	var f *pa.Failure
	if err != nil && !errors.As(err, &f) {
		err = errors.Join(paHTTPFailure(pa.OutcomeUnknown, "charter_fix"), err)
	}
	return err
}
