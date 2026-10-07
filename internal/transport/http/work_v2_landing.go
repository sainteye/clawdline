package http

import (
	"context"
	"errors"
	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/work"
	"net/http"
	"strings"
)

func verifyWorkV2DirectLanding(ctx context.Context, g workV2GitReader, item app.WorkV2View,
	session, repo string, ask *workV2LandingRequest) (*app.VerifiedLandingV2, *app.WorkError) {
	if ask == nil {
		return nil, nil
	}
	ownedDirectly := false
	for _, a := range item.Assignments {
		ownsSession := a.State == "active" && a.SessionID == session
		directSession := a.Mode == "existing_session"
		resolvedRoot := a.Mode == "new_session" && strings.TrimSpace(a.RootAssignment) != ""
		if ownsSession && (directSession || resolvedRoot) {
			ownedDirectly = true
			break
		}
	}
	if !ownedDirectly {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "direct_landing_not_applicable",
			Message: "Direct landing evidence belongs to an active existing-Session assignment or a resolved Root Assignment."}
	}
	commit, target, remote := strings.TrimSpace(ask.Commit), strings.TrimSpace(ask.Target), strings.TrimSpace(ask.Remote)
	if commit == "" || !g.ValidBranchName(ctx, target) || remote == "" || strings.Contains(remote, "/") ||
		!g.ValidBranchName(ctx, remote) {
		return nil, &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_landing_evidence",
			Message: "Landing evidence needs a commit, a valid local target branch, and one remote name."}
	}
	resolved, err := g.ResolveCommit(ctx, repo, commit)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_commit_unresolved",
			Message: "The landing commit does not resolve in the item's Project. Work that landed in another Project names it with landing.project."}
	}
	localRef := "refs/heads/" + target
	localHead, err := g.ResolveCommit(ctx, repo, localRef)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_target_unresolved",
			Message: "The local target branch does not resolve in the item's Project."}
	}
	onLocal, err := g.IsAncestor(ctx, repo, resolved, localHead)
	if err != nil || !onLocal {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_not_on_target",
			Message: "The landing commit is not contained by the local target branch."}
	}
	remoteRef := "refs/remotes/" + remote + "/" + target
	remoteHead, err := g.ResolveCommit(ctx, repo, remoteRef)
	if err != nil {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_remote_unresolved",
			Message: "The remote-tracking target does not resolve; fetch or push it before recording landing."}
	}
	onRemote, err := g.IsAncestor(ctx, repo, resolved, remoteHead)
	if err != nil || !onRemote {
		return nil, &app.WorkError{Status: http.StatusConflict, Code: "landing_not_published",
			Message: "The landing commit is not contained by the remote-tracking target."}
	}
	landing := &app.VerifiedLandingV2{Commit: resolved, Target: target, TargetCommit: localHead,
		Remote: remote, RemoteCommit: remoteHead}
	if repo != item.Item.ProjectPath {
		landing.Repository = repo
	}
	return landing, nil
}

// workV2FinishGit is what finishing reads of git: the landing proof's
// questions, and which remote the target branch tracks.
type workV2FinishGit interface {
	workV2GitReader
	UpstreamRemote(context.Context, string, string) (string, error)
}

// finishLandingAsk spells the landing a root did not type, from what the
// daemon already holds, so that `item finish` after a merge needs no commit,
// target or remote. Whatever the root did type wins. The result is still only
// an ask: verifyWorkV2DirectLanding proves it against git, and Finish's gate
// still checks the commit against the authorized candidate, so nothing here
// can let a step through that typing the same values would not.
//
//   - commit: the candidate the current PASS or override authorized, on an
//     item with a verification gate; otherwise the one commit the bound
//     tasks' landings name.
//   - target: the one branch the bound tasks' landings name.
//   - remote: the remote that target branch tracks.
//   - repository: the one repository the landings name, when it is not the
//     item's own Project.
//
// It answers nil when no direct landing is needed: the item is already past
// merging, or it has no gate and nothing was typed, so its bound tasks'
// landings (which Finish reads itself) are the evidence. Two different
// answers for one field are refused rather than chosen between.
func finishLandingAsk(ctx context.Context, g workV2FinishGit, facts app.FinishFactsV2,
	ask *workV2LandingRequest) (*workV2LandingRequest, string, *app.WorkError) {
	switch facts.Item.Phase {
	case work.PhaseImplementing, work.PhaseVerifying, work.PhaseMerging:
	default:
		return nil, "", nil
	}
	gated := facts.Item.VerifyGate
	if !gated && ask == nil {
		return nil, "", nil
	}
	out := workV2LandingRequest{}
	if ask != nil {
		out = *ask
	}
	commits, targets, repos := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range facts.Landings {
		if l.Commit != "" {
			commits[l.Commit] = true
		}
		if l.Target != "" {
			targets[l.Target] = true
		}
		if l.Repository != "" {
			repos[l.Repository] = true
		}
	}
	only := func(set map[string]bool) (string, int) {
		if len(set) == 1 {
			for v := range set {
				return v, 1
			}
		}
		return "", len(set)
	}
	conflict := func(code, message string) *app.WorkError {
		return &app.WorkError{Status: http.StatusConflict, Code: code, Message: message}
	}
	if strings.TrimSpace(out.Commit) == "" {
		if gated {
			if facts.Candidate == "" {
				return nil, "", conflict("verification_authorization_required",
					"This cycle requires an independent PASS or reasoned override for its exact candidate and acceptance criteria before merging.")
			}
			out.Commit = facts.Candidate
		} else {
			commit, n := only(commits)
			switch n {
			case 0:
				return nil, "", conflict("landing_required", app.LandingRequiredMessage)
			case 1:
				out.Commit = commit
			default:
				return nil, "", conflict("landing_ambiguous",
					"The tasks bound to this item landed different commits; name the one with --commit.")
			}
		}
	}
	if strings.TrimSpace(out.Target) == "" {
		target, n := only(targets)
		switch n {
		case 0:
			return nil, "", conflict("landing_target_unknown",
				"No landed task bound to this item names its target branch; name it with --target.")
		case 1:
			out.Target = target
		default:
			return nil, "", conflict("landing_ambiguous",
				"The tasks bound to this item landed on different branches; name the one with --target.")
		}
	}
	repo := ""
	if strings.TrimSpace(out.Project) == "" {
		if r, n := only(repos); n == 1 && r != facts.Item.ProjectPath {
			repo = r
		} else if n > 1 {
			return nil, "", conflict("landing_ambiguous",
				"The tasks bound to this item landed in different repositories; name the Project with --landing-project.")
		}
	}
	if strings.TrimSpace(out.Remote) == "" {
		where := repo
		if where == "" {
			where = facts.Item.ProjectPath
		}
		remote, err := g.UpstreamRemote(ctx, where, out.Target)
		if err != nil {
			return nil, "", &app.WorkError{Status: http.StatusServiceUnavailable, Code: "landing_remote_unreadable",
				Message: "Git could not say which remote " + out.Target + " tracks; name it with --remote."}
		}
		if remote == "" {
			return nil, "", conflict("landing_remote_unknown",
				"The branch "+out.Target+" tracks no remote; name the remote whose copy also holds the commit with --remote.")
		}
		out.Remote = remote
	}
	return &out, repo, nil
}

// noLandingWithLanding refuses --no-landing-reason sent beside a landing:
// two answers to whether the work has code, before git is asked anything.
func noLandingWithLanding(reason string, landing *workV2LandingRequest) *app.WorkError {
	if strings.TrimSpace(reason) == "" || landing == nil {
		return nil
	}
	if strings.TrimSpace(landing.Commit) == "" && strings.TrimSpace(landing.Target) == "" &&
		strings.TrimSpace(landing.Remote) == "" && strings.TrimSpace(landing.Project) == "" {
		return nil
	}
	return &app.WorkError{Status: http.StatusUnprocessableEntity, Code: "invalid_landing_evidence",
		Message: "Give either a landing (--commit) or --no-landing-reason, not both."}
}

// detectItemLandings asks the broker's detector, now, about every task bound
// to the item whose landing is still pending, and answers the item's finish
// facts as they stand after it — so a branch merged a moment ago counts
// without waiting for the beat's next look. A look that fails changes
// nothing; what it found is kept for the refusal the step may still give.
func (s *Server) detectItemLandings(ctx context.Context, id string) (app.FinishFactsV2, []orchestrator.LandingDetection, error) {
	facts, err := s.workV2().FinishFacts(ctx, id)
	if err != nil || len(facts.Pending) == 0 {
		return facts, nil, err
	}
	found := s.broker.DetectLandingsFor(ctx, facts.Pending)
	for _, d := range found {
		if d.Landed {
			facts, err = s.workV2().FinishFacts(ctx, id)
			break
		}
	}
	return facts, found, err
}

// withDetections adds what the detector found to a refusal for want of a
// landing, so the root reads why a merge it believes it made did not count.
func withDetections(err error, found []orchestrator.LandingDetection) error {
	var we *app.WorkError
	if !errors.As(err, &we) || (we.Code != "landing_required" && we.Code != "invalid_transition") {
		return err
	}
	var said []string
	for _, d := range found {
		if !d.Landed && d.Reason != "" {
			said = append(said, "task "+d.Task+": "+d.Reason)
		}
	}
	if len(said) == 0 {
		return err
	}
	out := *we
	out.Message = strings.TrimSpace(we.Message) + " The broker looked just now: " + strings.Join(said, "; ") + "."
	return &out
}

// recordedLandingsWire is the one projection of an item's landings, for the
// item read and the landings route alike.
func recordedLandingsWire(in []app.ItemLandingV2) []contract.RecordedLanding {
	if in == nil {
		return nil
	}
	out := make([]contract.RecordedLanding, 0, len(in))
	for _, l := range in {
		row := contract.RecordedLanding{ID: l.ID, Source: contract.RecordedLandingSource(l.Source), TaskID: l.Task,
			State: l.State, Repository: l.Repository, Target: l.Target, Commit: l.Commit, TargetCommit: l.TargetCommit,
			Remote: l.Remote, RemoteCommit: l.RemoteCommit}
		if !l.RecordedAt.IsZero() {
			row.RecordedAt = l.RecordedAt.Unix()
		}
		out = append(out, row)
	}
	return out
}

// agentFinishItem is POST /v1/work/v2/agent/items/<id>/finish: `item finish`.
func (s *Server) agentFinishItem(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ExpectedVersion    int64                 `json:"expected_version"`
		SessionID          string                `json:"session_id"`
		Verification       string                `json:"verification"`
		Landing            *workV2LandingRequest `json:"landing"`
		NoLandingReason    string                `json:"no_landing_reason"`
		Deployment         string                `json:"deployment"`
		NoDeploymentReason string                `json:"no_deployment_reason"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, body.SessionID, raw)
	if !ok {
		return
	}
	refuse := func(err error) {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
	}
	if refusal := noLandingWithLanding(body.NoLandingReason, body.Landing); refusal != nil {
		refuse(refusal)
		return
	}
	catalog := s.workV2Projects(r.Context())
	item, err := s.workV2().Item(r.Context(), id)
	if err != nil {
		refuse(err)
		return
	}
	facts, detections, err := s.detectItemLandings(r.Context(), id)
	if err != nil {
		refuse(err)
		return
	}
	git := gitadapter.New()
	var ask *workV2LandingRequest
	landedRepo := ""
	if strings.TrimSpace(body.NoLandingReason) == "" {
		var askErr *app.WorkError
		ask, landedRepo, askErr = finishLandingAsk(r.Context(), git, facts, body.Landing)
		if askErr != nil {
			refuse(withDetections(askErr, detections))
			return
		}
	}
	repo := item.Item.ProjectPath
	if landedRepo != "" {
		repo = landedRepo
	}
	if ask != nil && strings.TrimSpace(ask.Project) != "" {
		other, ok := catalog[strings.TrimSpace(ask.Project)]
		if !ok {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			writeRefusal(w, http.StatusUnprocessableEntity, "landing_project_not_found",
				"landing.project names no Project in the current catalog.")
			return
		}
		repo = other.Path
	}
	landing, landingErr := verifyWorkV2DirectLanding(r.Context(), git, item, body.SessionID, repo, ask)
	if landingErr != nil {
		refuse(landingErr)
		return
	}
	var answer []byte
	changed, err := s.workV2().Finish(r.Context(), id, app.FinishWorkV2{ExpectedVersion: app.AgentExpectedVersion(body.ExpectedVersion),
		SessionID: body.SessionID, Verification: body.Verification, Landing: landing,
		NoLandingReason: body.NoLandingReason, Deployment: body.Deployment,
		NoDeploymentReason: body.NoDeploymentReason, Actor: body.SessionID,
		Effects: []store.Effect{workV2CompletionEffect(item, body.SessionID, s.productLanguage())}},
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(s.workV2ItemOf(catalog, v))
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
	if err != nil {
		refuse(withDetections(err, detections))
		return
	}
	s.broker.RunEffects(r.Context(), changed.EffectIDs)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}
