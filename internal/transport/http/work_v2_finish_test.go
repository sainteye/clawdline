package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// finishGit is landingGit that also says which remote a branch tracks.
type finishGit struct {
	landingGit
	upstream map[string]string
	broken   bool
}

func (g finishGit) UpstreamRemote(_ context.Context, _, branch string) (string, error) {
	if g.broken {
		return "", errors.New("git would not answer")
	}
	return g.upstream[branch], nil
}

func finishFacts(phase work.Phase, gated bool, candidate string, landings ...app.BoundLandingV2) app.FinishFactsV2 {
	return app.FinishFactsV2{Item: work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectPath: "/project",
		Phase: phase, VerifyGate: gated}, Candidate: candidate, Landings: landings}
}

func TestFinishSpellsTheLandingFromWhatTheDaemonHolds(t *testing.T) {
	g := finishGit{upstream: map[string]string{"main": "origin"}}
	landed := app.BoundLandingV2{Task: "t1", Target: "main", Commit: "landed-commit", Repository: "/project"}

	// A gated item: the commit is the authorized candidate, never the
	// landing's own commit.
	ask, repo, refusal := finishLandingAsk(context.Background(), g, finishFacts(work.PhaseMerging, true, "candidate", landed), nil)
	if refusal != nil || ask == nil || ask.Commit != "candidate" || ask.Target != "main" || ask.Remote != "origin" || repo != "" {
		t.Fatalf("gated ask = %+v %q %v", ask, repo, refusal)
	}
	// What the root typed wins.
	ask, _, refusal = finishLandingAsk(context.Background(), g, finishFacts(work.PhaseVerifying, true, "candidate", landed),
		&workV2LandingRequest{Remote: "upstream"})
	if refusal != nil || ask.Remote != "upstream" || ask.Commit != "candidate" {
		t.Fatalf("typed remote = %+v %v", ask, refusal)
	}
	// An ungated item with nothing typed needs no direct landing: the bound
	// tasks' landings are the evidence, and Finish reads them itself.
	if ask, _, refusal = finishLandingAsk(context.Background(), g, finishFacts(work.PhaseMerging, false, "", landed), nil); ask != nil || refusal != nil {
		t.Fatalf("ungated ask = %+v %v", ask, refusal)
	}
	// Ungated with a partial ask: the commit comes from the landing.
	ask, _, refusal = finishLandingAsk(context.Background(), g, finishFacts(work.PhaseMerging, false, "", landed),
		&workV2LandingRequest{Target: "main"})
	if refusal != nil || ask.Commit != "landed-commit" || ask.Remote != "origin" {
		t.Fatalf("ungated partial ask = %+v %v", ask, refusal)
	}
	// Past merging, nothing is asked of git at all.
	if ask, _, refusal = finishLandingAsk(context.Background(), finishGit{broken: true},
		finishFacts(work.PhaseDeploying, true, "", landed), nil); ask != nil || refusal != nil {
		t.Fatalf("deploying ask = %+v %v", ask, refusal)
	}
	// A landing in another repository is proved there.
	elsewhere := landed
	elsewhere.Repository = "/frontend"
	if _, repo, _ = finishLandingAsk(context.Background(), g, finishFacts(work.PhaseMerging, true, "candidate", elsewhere), nil); repo != "/frontend" {
		t.Fatalf("repository = %q", repo)
	}
}

func TestFinishRefusesALandingItCannotSpellByName(t *testing.T) {
	g := finishGit{upstream: map[string]string{"main": "origin"}}
	main := app.BoundLandingV2{Task: "t1", Target: "main", Commit: "a"}
	other := app.BoundLandingV2{Task: "t2", Target: "release", Commit: "b"}
	for _, c := range []struct {
		name  string
		git   finishGit
		facts app.FinishFactsV2
		code  string
	}{
		{"no PASS", g, finishFacts(work.PhaseMerging, true, "", main), "verification_authorization_required"},
		{"no landed task", g, finishFacts(work.PhaseMerging, true, "candidate"), "landing_target_unknown"},
		{"two targets", g, finishFacts(work.PhaseMerging, true, "candidate", main, other), "landing_ambiguous"},
		{"tracks nothing", finishGit{upstream: map[string]string{}}, finishFacts(work.PhaseMerging, true, "candidate", main), "landing_remote_unknown"},
		{"git silent", finishGit{broken: true}, finishFacts(work.PhaseMerging, true, "candidate", main), "landing_remote_unreadable"},
	} {
		_, _, refusal := finishLandingAsk(context.Background(), c.git, c.facts, nil)
		if refusal == nil || refusal.Code != c.code {
			t.Fatalf("%s: %+v, want %s", c.name, refusal, c.code)
		}
	}
	// Ungated, a partial ask with two landed commits names neither.
	_, _, refusal := finishLandingAsk(context.Background(), g, finishFacts(work.PhaseMerging, false, "", main,
		app.BoundLandingV2{Task: "t3", Target: "main", Commit: "c"}), &workV2LandingRequest{Remote: "origin"})
	if refusal == nil || refusal.Code != "landing_ambiguous" {
		t.Fatalf("two commits: %+v", refusal)
	}
}

// The finish route end to end: an item whose child's branch landed is taken
// from implementing to done with only the two notes, and the same request
// again under a new key changes nothing.
func TestTheFinishRouteClosesALandedItemWithOnlyTheNotes(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err = s.workV2().Advance(context.Background(), v.Item.ID, app.AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
		SessionID: p.s.ConversationID, Next: work.PhaseImplementing, Actor: p.s.ConversationID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	record, _ := json.Marshal(orchestrator.Record{ID: "7a5c0000-0000-4000-8000-0000000000e1", Kind: "custom",
		Title: "the child", WorkID: v.Item.ID, WorkFrom: work.WorkNamed, State: orchestrator.StateSuccess,
		CreatedAt: at, FinishedAt: at, Root: &orchestrator.RootRef{SessionID: p.s.ConversationID, Assistant: "codex"},
		Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", Commit: strings.Repeat("d", 40), At: at}})
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: "7a5c0000-0000-4000-8000-0000000000e1",
		Project: v.Item.ProjectPath, Assistant: "codex", State: "success", CreatedAt: at, SecretHash: "h", Record: record}, nil); err != nil {
		t.Fatal(err)
	}
	finish := func(key string, fields map[string]any) *httptest.ResponseRecorder {
		fields["expected_version"], fields["session_id"] = owned.Item.Version, p.s.ConversationID
		body, _ := json.Marshal(fields)
		req := httptest.NewRequest(http.MethodPost, "/v1/work/v2/agent/items/"+v.Item.ID+"/finish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
			machine: true, verdict: auth.Verdict{Allowed: true},
		}))
		rec := httptest.NewRecorder()
		s.workV2Route(rec, req)
		return rec
	}
	rec := finish("finish-without-verification", map[string]any{"deployment": "rebuilt"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "verification_required") {
		t.Fatalf("without verification: %d %s", rec.Code, rec.Body)
	}
	rec = finish("finish-once", map[string]any{"verification": "go test ./... passed", "deployment": "rebuilt"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"done"`) {
		t.Fatalf("finish: %d %s", rec.Code, rec.Body)
	}
	done, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || done.Item.Phase != work.PhaseDone {
		t.Fatalf("stored: %+v %v", done.Item, err)
	}
	rec = finish("finish-again", map[string]any{"verification": "again", "deployment": "again"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"done"`) {
		t.Fatalf("finish again: %d %s", rec.Code, rec.Body)
	}
	again, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || again.Item.Version != done.Item.Version {
		t.Fatalf("the second finish wrote: %d -> %d %v", done.Item.Version, again.Item.Version, err)
	}
}
