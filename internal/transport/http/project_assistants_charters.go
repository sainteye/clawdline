package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Team charters are one Markdown document per Project of this machine, kept
// as immutable versions (docs/bots.md, "Team charter"). Like the tower floors
// the routes stand outside /v1/project-assistants/ and check the named
// machine's Bots authority for every read and write.
const chartersPath = "/v1/project-assistant-charters"

func (s *Server) projectAssistantChartersRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var (
		answer any
		err    error
	)
	switch route := r.Method + " " + r.URL.Path; route {
	case "GET " + chartersPath:
		answer, err = s.chartersList(r)
	case "GET " + chartersPath + "/charter":
		answer, err = s.charterRead(r)
	case "PUT " + chartersPath + "/charter":
		answer, err = s.charterSave(w, r)
	case "POST " + chartersPath + "/restore":
		answer, err = s.charterRestore(w, r)
	case "POST " + chartersPath + "/remove":
		answer, err = s.charterRemove(w, r)
	case "PUT " + chartersPath + "/playbook":
		answer, err = s.charterPlaybookSave(w, r)
	case "POST " + chartersPath + "/playbook-remove":
		answer, err = s.charterPlaybookRemove(w, r)
	case "PUT " + chartersPath + "/workflow":
		answer, err = s.charterWorkflowSave(w, r)
	case "GET " + chartersPath + "/readers":
		answer, err = s.charterReaders(r)
	case "GET " + chartersPath + "/proposals":
		answer, err = s.charterProposals(r)
	case "POST " + chartersPath + "/proposal-apply":
		answer, err = s.charterApply(w, r)
	case "POST " + chartersPath + "/proposal-dismiss":
		answer, err = s.charterDismiss(w, r)
	case "POST " + chartersPath + "/change-request":
		answer, err = s.charterChangeRequest(w, r)
	case "GET " + chartersPath + "/conflicts":
		answer, err = s.charterConflicts(r)
	case "POST " + chartersPath + "/conflict-resolve":
		answer, err = s.charterConflictResolve(w, r)
	default:
		err = paHTTPFailure(pa.Invalid, "route")
	}
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	writeJSON(w, answer)
}

func charterLimits() contract.ProjectAssistantCharterLimits {
	return contract.ProjectAssistantCharterLimits{TextBytes: pa.MaxCharterTextBytes, Versions: pa.MaxCharterVersions, Proposals: pa.MaxCharterProposals,
		ReasonBytes: pa.MaxCharterReasonBytes, RequestBytes: pa.MaxCharterRequestBytes, PlaybookTextBytes: pa.MaxCharterPlaybookTextBytes, Playbooks: pa.MaxCharterPlaybooks,
		PlaybookTitleBytes: pa.MaxCharterPlaybookTitleBytes, PlaybookBots: pa.MaxCharterPlaybookBots, DocumentBytes: pa.MaxCharterDocumentBytes,
		WorkflowStates: pa.MaxCharterWorkflowStates, WorkflowTransitions: pa.MaxCharterWorkflowTransitions, WorkflowTitleBytes: pa.MaxCharterWorkflowTitleBytes,
		WorkflowNoteBytes: pa.MaxCharterWorkflowNoteBytes, WorkflowIDBytes: pa.MaxCharterWorkflowIDBytes}
}

func charterTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func wireCharterVersion(v pa.CharterVersion, text bool) contract.ProjectAssistantCharterVersion {
	out := contract.ProjectAssistantCharterVersion{Version: v.Version, Author: contract.ProjectAssistantCharterAuthor(v.Author), Reason: v.Reason, CreatedAt: charterTime(v.CreatedAt)}
	if text {
		out.Text = v.Text
		out.Playbooks = wirePlaybooks(v.Playbooks)
		out.Workflow = wireWorkflow(v.Workflow)
	}
	return out
}

// charterRead names one Project of one machine and holds the reply to the
// authority it was read under.
func (s *Server) charterReadScope(r *http.Request, fields ...string) (string, string, func() error, error) {
	query, err := paHTTPQuery(r, append([]string{"machine_id", "project_id"}, fields...)...)
	if err != nil {
		return "", "", nil, err
	}
	machine, project := query.Get("machine_id"), query.Get("project_id")
	if err = pa.ValidateCharterProject(project); err != nil {
		return "", "", nil, err
	}
	before, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err != nil {
		return "", "", nil, err
	}
	after := func() error {
		now, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
		if err == nil {
			err = sameMachineAuthority(before, now)
		}
		return err
	}
	return machine, project, after, nil
}

func (s *Server) chartersList(r *http.Request) (contract.ProjectAssistantChartersResponse, error) {
	query, err := paHTTPQuery(r, "machine_id")
	if err != nil {
		return contract.ProjectAssistantChartersResponse{}, err
	}
	machine := query.Get("machine_id")
	before, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err != nil {
		return contract.ProjectAssistantChartersResponse{}, err
	}
	store := s.projectAssistantFloorsStore()
	charters, err := store.Charters(r.Context(), machine)
	if err != nil {
		return contract.ProjectAssistantChartersResponse{}, err
	}
	bots, err := store.CharterBots(r.Context(), machine)
	if err != nil {
		return contract.ProjectAssistantChartersResponse{}, err
	}
	conflicts, err := store.OpenCharterConflicts(r.Context(), machine)
	if err != nil {
		return contract.ProjectAssistantChartersResponse{}, err
	}
	out := contract.ProjectAssistantChartersResponse{ContractVersion: pa.ContractVersion, MachineID: machine, Charters: []contract.ProjectAssistantCharterSummary{}, ProjectsWithoutCharter: []string{}, Limits: charterLimits()}
	has := map[string]bool{}
	for _, c := range charters {
		has[c.ProjectID] = true
		out.Charters = append(out.Charters, contract.ProjectAssistantCharterSummary{ProjectID: c.ProjectID, Version: c.Version, UpdatedAt: charterTime(c.UpdatedAt), UpdatedBy: contract.ProjectAssistantCharterAuthor(c.UpdatedBy), PendingProposals: int64(c.Pending), OpenConflicts: int64(conflicts[c.ProjectID])})
	}
	out.ProjectsWithoutCharter = append(out.ProjectsWithoutCharter, projectsWithoutCharter(bots, has)...)
	now, err := s.projectAssistantMachineAuthority(r.Context(), machine, false)
	if err == nil {
		err = sameMachineAuthority(before, now)
	}
	return out, err
}

// projectsWithoutCharter names, once each, the Projects whose Bots have no
// charter yet. A personal workspace belongs to one Bot, and a team charter
// is for a Project's team, so one is not offered.
func projectsWithoutCharter(bots []projectassistantstore.CharterBot, has map[string]bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, b := range bots {
		if b.ProjectID != "" && b.ProjectKind != pa.PersonalWorkspace && !has[b.ProjectID] && !seen[b.ProjectID] {
			seen[b.ProjectID] = true
			out = append(out, b.ProjectID)
		}
	}
	return out
}

func (s *Server) charterAnswer(ctx context.Context, machine, project string, selected int64) (contract.ProjectAssistantCharterResponse, error) {
	store := s.projectAssistantFloorsStore()
	current, err := store.Charter(ctx, machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	history, err := store.CharterHistory(ctx, machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	proposals, err := store.CharterProposals(ctx, machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	out := contract.ProjectAssistantCharterResponse{ContractVersion: pa.ContractVersion, MachineID: machine, ProjectID: project, Version: current.Version, Text: current.Text,
		Playbooks: wirePlaybooks(current.Playbooks), Workflow: wireWorkflow(current.Workflow), History: make([]contract.ProjectAssistantCharterVersion, 0, len(history)), PendingProposals: int64(len(proposals)), Limits: charterLimits()}
	for _, v := range history {
		out.History = append(out.History, wireCharterVersion(v, false))
	}
	if selected > 0 {
		v, err := store.CharterAt(ctx, machine, project, selected)
		if err != nil {
			return contract.ProjectAssistantCharterResponse{}, err
		}
		wire := wireCharterVersion(v, true)
		out.Selected = &wire
	}
	return out, nil
}

func (s *Server) charterRead(r *http.Request) (contract.ProjectAssistantCharterResponse, error) {
	machine, project, after, err := s.charterReadScope(r, "version")
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	query, _ := paHTTPQuery(r, "machine_id", "project_id", "version")
	selected, err := paHTTPNumber(query, "version")
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	out, err := s.charterAnswer(r.Context(), machine, project, selected)
	if err != nil {
		return out, err
	}
	return out, after()
}

// charterWrite checks one write's key and authority, runs it, and answers
// only under the authority it was made with. A failure that is not a typed
// refusal may or may not have committed.
func (s *Server) charterWrite(r *http.Request, machine, project, idem string, write func(context.Context) error, answer func(context.Context) (any, error)) (any, error) {
	if idem == "" || r.Header.Get("Idempotency-Key") != idem {
		return nil, paHTTPFailure(pa.Invalid, "idempotency_key")
	}
	if err := pa.ValidateCharterProject(project); err != nil {
		return nil, err
	}
	before, err := s.projectAssistantMachineAuthority(r.Context(), machine, true)
	if err != nil {
		return nil, err
	}
	if err = write(r.Context()); err != nil {
		var f *pa.Failure
		if !errors.As(err, &f) {
			err = errors.Join(paHTTPFailure(pa.OutcomeUnknown, "charter"), err)
		}
		return nil, err
	}
	ctx := context.WithoutCancel(r.Context())
	now, err := s.projectAssistantMachineAuthority(ctx, machine, true)
	if err == nil {
		err = sameMachineAuthority(before, now)
	}
	if err != nil {
		return nil, errors.Join(paHTTPFailure(pa.OutcomeUnknown, "charter_publication"), err)
	}
	return answer(ctx)
}

func (s *Server) charterSave(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTPLimit[contract.ProjectAssistantCharterSaveRequest](w, r, pa.MaxCharterBodyBytes)
	if err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		_, err := s.projectAssistantFloorsStore().SaveCharter(ctx, projectassistantstore.CharterWrite{MachineID: body.MachineID, ProjectID: body.ProjectID,
			ExpectedVersion: body.ExpectedVersion, Text: body.Text, Reason: body.Reason, Author: pa.CharterByPerson, IdempotencyKey: body.IdempotencyKey, At: time.Now()})
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}

func (s *Server) charterRestore(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterRestoreRequest](w, r)
	if err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		_, err := s.projectAssistantFloorsStore().RestoreCharter(ctx, body.MachineID, body.ProjectID, body.ExpectedVersion, body.Version, body.IdempotencyKey, time.Now())
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}

// charterRemove takes a Project's charter away. The Bots' next prompts say
// once that there is no charter; fresh sessions carry none.
func (s *Server) charterRemove(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterRemoveRequest](w, r)
	if err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		_, err := s.projectAssistantFloorsStore().RemoveCharter(ctx, body.MachineID, body.ProjectID, body.ExpectedVersion, body.IdempotencyKey, time.Now())
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}

func (s *Server) charterReaders(r *http.Request) (contract.ProjectAssistantCharterReadersResponse, error) {
	machine, project, after, err := s.charterReadScope(r)
	if err != nil {
		return contract.ProjectAssistantCharterReadersResponse{}, err
	}
	store := s.projectAssistantFloorsStore()
	current, err := store.Charter(r.Context(), machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterReadersResponse{}, err
	}
	bots, err := store.CharterBots(r.Context(), machine)
	if err != nil {
		return contract.ProjectAssistantCharterReadersResponse{}, err
	}
	reads, err := store.CharterReads(r.Context(), machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterReadersResponse{}, err
	}
	out := contract.ProjectAssistantCharterReadersResponse{ContractVersion: pa.ContractVersion, MachineID: machine, ProjectID: project, Version: current.Version, Readers: []contract.ProjectAssistantCharterReader{}}
	for _, b := range bots {
		if b.ProjectID != project {
			continue
		}
		reader := contract.ProjectAssistantCharterReader{BotID: b.BotID, Name: b.Name, Lead: b.Lead, Hidden: b.Hidden, Running: b.Running, Queued: b.Queued,
			DeliveryVersion: current.DeliveryVersion(b.BotID), PlaybookIds: []string{}}
		for _, p := range current.PlaybooksFor(b.BotID) {
			reader.PlaybookIds = append(reader.PlaybookIds, p.ID)
		}
		if read, ok := reads[b.BotID]; ok {
			reader.Version, reader.TurnID, reader.ReadAt = read.Version, read.TurnID, charterTime(read.At)
		}
		out.Readers = append(out.Readers, reader)
	}
	return out, after()
}

func (s *Server) charterProposalsAnswer(ctx context.Context, machine, project string) (contract.ProjectAssistantCharterProposalsResponse, error) {
	store := s.projectAssistantFloorsStore()
	current, err := store.Charter(ctx, machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterProposalsResponse{}, err
	}
	proposals, err := store.CharterProposals(ctx, machine, project)
	if err != nil {
		return contract.ProjectAssistantCharterProposalsResponse{}, err
	}
	out := contract.ProjectAssistantCharterProposalsResponse{ContractVersion: pa.ContractVersion, MachineID: machine, ProjectID: project, Version: current.Version, Proposals: make([]contract.ProjectAssistantCharterProposal, 0, len(proposals))}
	for _, p := range proposals {
		out.Proposals = append(out.Proposals, contract.ProjectAssistantCharterProposal{ID: p.ID, BaseVersion: p.BaseVersion, Kind: string(p.Kind), Text: p.Text, Workflow: wireWorkflow(p.Workflow),
			Summary: p.Summary, BotID: p.BotID, TurnID: p.TurnID, CreatedAt: charterTime(p.CreatedAt), Stale: p.BaseVersion != current.Version})
	}
	return out, nil
}

func (s *Server) charterProposals(r *http.Request) (contract.ProjectAssistantCharterProposalsResponse, error) {
	machine, project, after, err := s.charterReadScope(r)
	if err != nil {
		return contract.ProjectAssistantCharterProposalsResponse{}, err
	}
	out, err := s.charterProposalsAnswer(r.Context(), machine, project)
	if err != nil {
		return out, err
	}
	return out, after()
}

func (s *Server) charterApply(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterApplyRequest](w, r)
	if err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		_, err := s.projectAssistantFloorsStore().ApplyCharterProposal(ctx, body.MachineID, body.ProjectID, body.ProposalID, body.ExpectedVersion, body.IdempotencyKey, time.Now())
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}

func (s *Server) charterDismiss(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterDismissRequest](w, r)
	if err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		return s.projectAssistantFloorsStore().DismissCharterProposal(ctx, body.MachineID, body.ProjectID, body.ProposalID)
	}, func(ctx context.Context) (any, error) {
		return s.charterProposalsAnswer(ctx, body.MachineID, body.ProjectID)
	})
}

// charterChangeRequest sends the person's words to the machine's lead Bot as
// their own message, marked as a change request for this Project's charter.
// The Bots send path owns the receipt: a repeated key answers its receipt.
func (s *Server) charterChangeRequest(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterChangeRequest](w, r)
	if err != nil {
		return nil, err
	}
	if body.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != body.IdempotencyKey {
		return nil, paHTTPFailure(pa.Invalid, "idempotency_key")
	}
	if err = pa.ValidateCharterProject(body.ProjectID); err != nil {
		return nil, err
	}
	if err = pa.ValidateCharterRequest(body.Text); err != nil {
		return nil, err
	}
	target := pa.CharterEditText
	switch body.Target {
	case "", "text":
	case "workflow":
		target = pa.CharterEditWorkflow
	default:
		return nil, paHTTPFailure(pa.Invalid, "target")
	}
	if _, err = s.projectAssistantMachineAuthority(r.Context(), body.MachineID, true); err != nil {
		return nil, err
	}
	store := s.projectAssistantFloorsStore()
	bots, err := store.CharterBots(r.Context(), body.MachineID)
	if err != nil {
		return nil, err
	}
	var lead pa.BotKey
	for _, b := range bots {
		if b.Lead {
			lead = pa.BotKey{MachineID: body.MachineID, BotID: b.BotID}
		}
	}
	if lead.BotID == "" {
		return nil, paHTTPFailure(pa.NotFound, "lead_bot")
	}
	current, err := store.Charter(r.Context(), body.MachineID, body.ProjectID)
	if err != nil {
		return nil, err
	}
	host, err := s.projectAssistantStartedHost(r.Context())
	if err != nil {
		return nil, err
	}
	service, err := host.Service()
	if err != nil {
		return nil, err
	}
	bot, c, _, err := service.Get(r.Context(), lead)
	if err != nil {
		return nil, err
	}
	fence := pa.Fence{Key: lead, Project: bot.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	receipt, err := service.SendCharterEdit(r.Context(), pa.Command{Key: lead, Operation: pa.SendOperation, IdempotencyKey: body.IdempotencyKey}, fence, body.Text,
		pa.CharterEdit{ProjectID: body.ProjectID, Version: current.Version, Target: target})
	if err != nil {
		return nil, err
	}
	return contract.ProjectAssistantCharterChangeResponse{ContractVersion: pa.ContractVersion, MachineID: body.MachineID, ProjectID: body.ProjectID, LeadBotID: lead.BotID, Receipt: paWireReceipt(receipt)}, nil
}

// projectAssistantStartedHost starts the Bots host when nothing has yet, as
// the Bots routes do.
func (s *Server) projectAssistantStartedHost(ctx context.Context) (*projectassistanthost.Host, error) {
	p := &s.projectAssistants
	p.mu.RLock()
	h := p.host
	p.mu.RUnlock()
	if h == nil {
		if err := s.StartProjectAssistants(ctx); err != nil {
			return nil, err
		}
		p.mu.RLock()
		h = p.host
		p.mu.RUnlock()
	}
	if h == nil {
		return nil, paHTTPFailure(pa.Disabled, "host")
	}
	return h, nil
}
