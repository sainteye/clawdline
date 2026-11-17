package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type progressHTTPPending struct {
	tools    app.NativeTools
	observed chan<- error
	release  <-chan struct{}
}

func (p progressHTTPPending) Wait(ctx context.Context) (app.PromptResult, error) {
	_, err := p.tools.Memory(ctx)
	p.observed <- err
	if err != nil {
		return app.PromptResult{}, err
	}
	select {
	case <-p.release:
		return app.PromptResult{FinalText: "Scripted answer after observed tool activity"}, nil
	case <-ctx.Done():
		return app.PromptResult{}, ctx.Err()
	}
}

// Real TCP authentication + SQLite + production wire and read projection.
// The optional Node harness renders the real component from this exact HTTP
// source. This is construction evidence, not native/browser/Cloud acceptance.
func TestProjectAssistantFallbackProgressStoredHTTP(t *testing.T) {
	f := newPAServerFixture(t, true)
	observed, release := make(chan error, 1), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(_ context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			s := &fallbackHTTPSession{send: func(app.PromptRequest) (app.PendingPrompt, error) {
				return progressHTTPPending{q.Tools, observed, release}, nil
			}}
			if q.Bot.Agent == pa.ClaudeCode {
				return s, fallbackUsageRefusal()
			}
			return s, nil
		}}, nil
	}
	h := &paSoakHTTP{fixture: f, server: httptest.NewServer(f.h)}
	h.reconnect()
	defer h.server.Close()
	defer h.client.CloseIdleConnections()
	if h.request(t, "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": true}, nil) != 200 {
		t.Fatal("enable failed")
	}
	create := f.create()
	create.Settings.Agent = contract.ProjectAssistantAgentClaude
	create.Settings.FallbackAgents = []contract.ProjectAssistantAgent{contract.ProjectAssistantAgentCodex}
	if h.request(t, "POST", "/v1/project-assistants", create, nil) != 200 {
		t.Fatal("create failed")
	}
	_, admission := h.send(t, "progress-first")
	var observedErr error
	paResilienceAwait(t, func() bool {
		select {
		case observedErr = <-observed:
			return true
		default:
			return false
		}
	})
	if observedErr != nil {
		t.Fatal(observedErr)
	}
	exportPath := "/v1/project-assistants/" + f.key.BotID + "/export?machine_id=" + f.key.MachineID
	var exported contract.ProjectAssistantExport
	if h.request(t, "GET", exportPath, nil, &exported) != 200 {
		t.Fatal("export failed")
	}
	if len(exported.Turns) != 1 || exported.Turns[0].ID != admission.ResourceID {
		t.Fatal("logical input changed")
	}
	active := exported.Turns[0].Attempts[len(exported.Turns[0].Attempts)-1]
	if !active.Opened || !active.SendStarted || !active.SendReturned || !active.NativeActivity || !active.ToolActivity || active.AnswerObserved || active.CleanupConfirmed || active.Stage != "sent" || active.Effect != "unknown" || active.CarryKind != "recap" {
		t.Fatalf("running lifecycle: %+v", active)
	}
	viewPath := paViewURL(f.key, exported.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100)
	var sent contract.ProjectAssistantReadViewResponse
	if h.request(t, "GET", viewPath, nil, &sent) != 200 {
		t.Fatal("snapshot failed")
	}
	if sent.Snapshot == nil || len(sent.Snapshot.PendingTurns) != 1 {
		t.Fatal("running turn missing")
	}
	attempts := sent.Snapshot.PendingTurns[0].Attempts
	if len(attempts) == 0 || attempts[len(attempts)-1] != active {
		t.Fatalf("snapshot lost stored progress: %+v", attempts)
	}
	render := func(phase string) {
		dir := os.Getenv("CLAWDLINE_FALLBACK_PROGRESS_DIR")
		if dir == "" {
			return
		}
		t.Helper()
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "node", "../../../web/console/tools/project-assistants-fallback-progress.e2e.mjs")
		cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+h.server.URL, "BOTS_TOKEN="+f.local, "BOTS_VIEW_PATH="+viewPath, "BOTS_ARTIFACTS="+dir, "BOTS_PHASE="+phase)
		log, err := cmd.CombinedOutput()
		_ = os.WriteFile(filepath.Join(dir, "render-"+phase+".log"), log, 0600)
		if err != nil {
			t.Fatalf("render %s: %v\n%s", phase, err, log)
		}
		t.Log(string(log))
	}
	render("sent")
	close(release)
	saved := fallbackHTTPWait(t, f)
	if saved.Turns[0].State != pa.TurnSucceeded || pa.SafeAttemptClosed(saved.Turns[0]) {
		t.Fatal("successful cleanup authorized replay")
	}
	var closed contract.ProjectAssistantReadViewResponse
	if h.request(t, "GET", viewPath, nil, &closed) != 200 || closed.Snapshot == nil || closed.Snapshot.LastTurn == nil {
		t.Fatal("closed snapshot failed")
	}
	final := closed.Snapshot.LastTurn.Attempts
	if len(final) == 0 || !final[len(final)-1].AnswerObserved || !final[len(final)-1].CleanupConfirmed || !final[len(final)-1].ToolActivity || final[len(final)-1].Effect != "unknown" {
		t.Fatalf("terminal lifecycle: %+v", final)
	}
	render("closed")
	// Round trip the optional fields, rather than asserting generated spelling.
	raw, err := json.Marshal(closed)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip contract.ProjectAssistantReadViewResponse
	if err = json.Unmarshal(raw, &roundTrip); err != nil || roundTrip.Snapshot.LastTurn.Attempts[len(final)-1] != final[len(final)-1] {
		t.Fatal("wire round trip lost facts", err)
	}
	t.Log("opened/send-return/tool/answer/cleanup facts survived real authenticated HTTP and SQLite; external/native/model requests=0")
}
