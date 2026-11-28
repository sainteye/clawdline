package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func saveHookRoutine(t *testing.T, f *paServerFixture, server *httptest.Server, trigger contract.ProjectAssistantRoutineTrigger) (contract.ProjectAssistantRoutineDefinition, contract.ProjectAssistantRoutineListResponse, string) {
	t.Helper()
	page := routineHumanPage(t, f, server, "", "", 100)
	q := contract.ProjectAssistantRoutineSaveRequest{Command: f.command(pa.EditOperation, "save-hook"), Fence: page.Fence, Name: "Synthetic hook", Instruction: "Report the synthetic event", Triggers: []contract.ProjectAssistantRoutineTrigger{trigger}, TimeoutSeconds: 3600, Enabled: true}
	status, raw := routineHumanTCP(t, f, server, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines", f.local, q)
	if status != 200 {
		t.Fatalf("save %d %s", status, raw)
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	d := page.Definitions[0]
	qHook := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, "read-hook"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw = routineHumanTCP(t, f, server, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines/"+d.ID+"/hook", f.local, qHook)
	if status != 200 {
		t.Fatalf("hook %d %s", status, raw)
	}
	var hook contract.ProjectAssistantRoutineHookResponse
	if err := json.Unmarshal(raw, &hook); err != nil || len(hook.Key) != 2*pa.MaxRoutineWebhookSecretBytes || hook.URL != "/hooks/routines/"+d.ID {
		t.Fatalf("hook response %s %v", raw, err)
	}
	return d, page, hook.Key
}

func TestRoutineHookCloudBridgeKeepsVersionKeyAndWriteAuthority(t *testing.T) {
	f, _, server := newRoutineHumanFixture(t)
	d, page, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	f.pin(t, "hook-writer", adaptercloud.ScopeOperate, false)
	f.pin(t, "hook-reader", adaptercloud.ScopeReadOnly, false)
	bridge := cloudops.Bridge{MachineID: f.identity.MachineID, Router: f.cloudRouter(), AllowCommands: func() bool { return true }}
	for _, sender := range []string{"hook-writer", "hook-reader"} {
		q := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, sender), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
		raw, _ := json.Marshal(q)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		body["type"], body["session"], body["request"] = "project-assistants.routine-hook", cloudops.MachineReplySession, sender
		raw, _ = json.Marshal(body)
		answer := bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + f.identity.MachineID, Class: cloudops.ClassCtl, Sender: sender, Sequence: 1, Plaintext: raw})
		if sender == "hook-reader" {
			if answer.OK() || bytes.Contains(answer.Payload, []byte(key)) {
				t.Fatalf("Cloud reader saw hook key: %+v %s", answer, answer.Payload)
			}
			continue
		}
		var response struct {
			Body contract.ProjectAssistantRoutineHookResponse `json:"body"`
		}
		if err := json.Unmarshal(answer.Payload, &response); err != nil || !answer.OK() || response.Body.ContractVersion != pa.ContractVersion || response.Body.Key != key || response.Body.URL != "/hooks/routines/"+d.ID {
			t.Fatalf("hook reply lost through actual Cloud bridge: %+v %s %v", answer, answer.Payload, err)
		}
	}
}

func TestRoutineHookTCPForgetProjectAndSourceRefusals(t *testing.T) {
	for _, change := range []string{"forget", "forget-empty", "project", "source"} {
		t.Run(change, func(t *testing.T) {
			f, _, server := newRoutineHumanFixture(t)
			d, _, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
			if strings.HasPrefix(change, "forget") {
				body := `{"text":"synthetic event"}`
				if change == "forget-empty" {
					body = `{}`
				}
				status, raw := hookTCP(t, server, d.ID, key, "forgotten-delivery", body, false, "")
				if status != 200 {
					t.Fatalf("delivery %d %s", status, raw)
				}
				bot := f.get(t, f.local)
				paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/memory/forget", f.local, contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation, "forget-event"), Project: bot.Bot.Project, ExpectedRevision: bot.Conversation.MemoryRevision, SourceIds: []string{"synthetic-event-source"}}))
				until := time.Now().Add(4 * time.Second)
				for {
					_, runs, err := store.New(f.s.store.ProjectAssistantDatabase()).Routines(context.Background(), f.key)
					if err != nil {
						t.Fatal(err)
					}
					if len(runs) == 1 && runs[0].State == pa.TurnCancelled {
						break
					}
					if time.Now().After(until) {
						t.Fatal("forgotten event remained runnable", runs)
					}
					time.Sleep(10 * time.Millisecond)
				}
				return
			}
			if change == "project" {
				if err := os.Rename(f.project, f.project+"-moved"); err != nil {
					t.Fatal(err)
				}
			} else {
				paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
			}
			status, raw := hookTCP(t, server, d.ID, key, "refused-delivery", `{}`, false, "")
			if status == 200 {
				t.Fatalf("%s accepted delivery %s", change, raw)
			}
			_, runs, err := store.New(f.s.store.ProjectAssistantDatabase()).Routines(context.Background(), f.key)
			if err != nil || len(runs) != 0 {
				t.Fatal("refusal leaked a run", runs, err)
			}
		})
	}
}

type routineHookUnknownProvider struct {
	base  routineHumanProvider
	calls atomic.Int64
}

type routineHookRetryProvider struct {
	base  routineHumanProvider
	calls atomic.Int64
}

func (p *routineHookRetryProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	return p.base.Capabilities(ctx, b)
}
func (p *routineHookRetryProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	session, err := p.base.Open(ctx, q, sink)
	if err == nil && q.Binding.SessionSlot != "" && p.calls.Add(1) == 1 {
		return session, &app.PreSendRefusal{Failure: &app.ProviderFailure{Reason: app.ProviderRateLimited, Cause: &pa.Failure{Code: pa.CapacityFull, Field: "qualified_synthetic_refusal"}}}
	}
	return session, err
}

func TestRoutineHookTCPQualifiedRetryKeepsFirstDelivery(t *testing.T) {
	f := newPAServerFixture(t, true)
	p := &routineHookRetryProvider{}
	var now atomic.Int64
	base := time.Now().UTC()
	now.Store(base.UnixMilli())
	f.s.projectAssistants.clock = func() time.Time { return time.UnixMilli(now.Load()) }
	f.s.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	server := httptest.NewServer(f.h)
	t.Cleanup(server.Close)
	paServerOK(t, f.send(t, f.local, "retry-main"))
	routineHumanIdle(t, f)
	d, _, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	status, first := hookTCP(t, server, d.ID, key, "retry-delivery", `{}`, false, "")
	if status != 200 {
		t.Fatalf("delivery %d %s", status, first)
	}
	until := time.Now().Add(4 * time.Second)
	var original contract.ProjectAssistantRoutineRun
	for {
		page := routineHumanPage(t, f, server, d.ID, "", 100)
		if len(page.Runs) == 1 && page.Runs[0].RetryCount == 1 && page.Runs[0].State == "queued" {
			original = page.Runs[0]
			break
		}
		if time.Now().After(until) {
			t.Fatal("qualified refusal lost its wait", page.Runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if original.NotBefore == "" || p.calls.Load() != 1 {
		t.Fatal("retry wait missing", original, p.calls.Load())
	}
	now.Store(base.Add(2 * time.Hour).UnixMilli())
	until = time.Now().Add(4 * time.Second)
	for {
		page := routineHumanPage(t, f, server, d.ID, "", 100)
		if len(page.Runs) == 1 && page.Runs[0].State == "succeeded" {
			if page.Runs[0].ID != original.ID || page.Runs[0].TurnID != original.TurnID {
				t.Fatal("retry changed run identity", original, page.Runs)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatal("safe retry did not wake", page.Runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, again := hookTCP(t, server, d.ID, key, "retry-delivery", `{}`, false, "")
	if status != 200 || !bytes.Equal(first, again) || p.calls.Load() != 2 {
		t.Fatal("retry delivery duplicated execution", status, string(again), p.calls.Load())
	}
}

func (p *routineHookUnknownProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	return p.base.Capabilities(ctx, b)
}
func (p *routineHookUnknownProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if q.Binding.SessionSlot == "" {
		return p.base.Open(ctx, q, sink)
	}
	p.calls.Add(1)
	return &routineHookUnknownSession{}, nil
}

type routineHookUnknownSession struct{}

func (*routineHookUnknownSession) SessionID() string { return "synthetic-unknown" }
func (s *routineHookUnknownSession) Send(context.Context, app.PromptRequest) (app.PendingPrompt, error) {
	return s, nil
}
func (*routineHookUnknownSession) Wait(context.Context) (app.PromptResult, error) {
	return app.PromptResult{}, &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "injected_after_send"}
}
func (*routineHookUnknownSession) Answer(context.Context, string, string) error { return nil }
func (*routineHookUnknownSession) Cancel(context.Context) error                 { return nil }
func (*routineHookUnknownSession) Close(context.Context) error                  { return nil }

func TestRoutineHookTCPUnknownDeliveryDoesNotReplay(t *testing.T) {
	f := newPAServerFixture(t, true)
	p := &routineHookUnknownProvider{}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	server := httptest.NewServer(f.h)
	t.Cleanup(server.Close)
	paServerOK(t, f.send(t, f.local, "unknown-main"))
	routineHumanIdle(t, f)
	d, _, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	status, first := hookTCP(t, server, d.ID, key, "unknown-delivery", `{}`, false, "")
	if status != 200 {
		t.Fatalf("admit %d %s", status, first)
	}
	until := time.Now().Add(4 * time.Second)
	for {
		page := routineHumanPage(t, f, server, d.ID, "", 100)
		if len(page.Runs) == 1 && page.Runs[0].State == "unknown" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("unknown outcome lost", page.Runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, again := hookTCP(t, server, d.ID, key, "unknown-delivery", `{}`, false, "")
	if status != 200 || !bytes.Equal(first, again) {
		t.Fatalf("unknown delivery lost identity: %d %s", status, again)
	}
	status, _ = hookTCP(t, server, d.ID, key, "replacement-delivery", `{}`, false, "")
	if status != 409 || p.calls.Load() != 1 {
		t.Fatal("unknown run gained another execution", status, p.calls.Load())
	}
}

func hookTCP(t *testing.T, server *httptest.Server, id, secret, delivery, body string, signed bool, event string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest("POST", server.URL+"/hooks/routines/"+id, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if signed {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(body))
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	} else {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	if delivery != "" {
		r.Header.Set("X-GitHub-Delivery", delivery)
	}
	if event != "" {
		r.Header.Set("X-GitHub-Event", event)
	}
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("hook response is cacheable")
	}
	return res.StatusCode, raw
}

func TestRoutineHookTCPAdmissionAndAtomicDelivery(t *testing.T) {
	f, _, server := newRoutineHumanFixture(t)
	source, event := "github", "issues.opened"
	d, page, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "event", Source: source, Event: event, Filters: []contract.ProjectAssistantRoutineEventFilter{{Key: "repo", Value: "synthetic/repo"}}})
	if d.NextDue != "" || len(d.NextRuns) != 1 || d.NextRuns[0] != "" {
		t.Fatal("hook acquired a clock", d)
	}
	unknownStatus, unknown := hookTCP(t, server, "unknown", key, "", `{}`, false, "")
	wrongStatus, wrong := hookTCP(t, server, d.ID, "wrong", "", `{}`, false, "")
	if unknownStatus != 401 || wrongStatus != unknownStatus || !bytes.Equal(unknown, wrong) {
		t.Fatalf("credential oracle: %d %s / %d %s", unknownStatus, unknown, wrongStatus, wrong)
	}
	for _, tc := range []struct {
		body, event string
		signed      bool
	}{{`{}`, "ping", true}, {`{"action":"closed"}`, "issues", true}, {`{"source":"slack","event":"issues","action":"opened"}`, "", false}} {
		status, raw := hookTCP(t, server, d.ID, key, "ignored", tc.body, tc.signed, tc.event)
		if status != 200 || !bytes.Contains(raw, []byte(`"ignored":true`)) {
			t.Fatalf("ignored %d %s", status, raw)
		}
	}
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := db.Exec(`CREATE TRIGGER reject_hook_receipt BEFORE INSERT ON project_assistant_receipts WHEN NEW.command_key LIKE 'routine-delivery-%' BEGIN SELECT RAISE(ABORT,'injected delivery commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	status, raw := hookTCP(t, server, d.ID, key, "delivery-one", `{"action":"opened","repository":{"full_name":"synthetic/repo"}}`, true, "issues")
	if status != 409 || !bytes.Contains(raw, []byte("webhook_outcome_unknown")) {
		t.Fatalf("injection %d %s", status, raw)
	}
	st := store.New(db)
	_, runs, err := st.Routines(context.Background(), f.key)
	if err != nil || len(runs) != 0 {
		t.Fatal("failed commit leaked queue", runs, err)
	}
	if _, err = db.Exec(`DROP TRIGGER reject_hook_receipt`); err != nil {
		t.Fatal(err)
	}
	status, first := hookTCP(t, server, d.ID, key, "delivery-one", `{"action":"opened","repository":{"full_name":"synthetic/repo"}}`, true, "issues")
	if status != 200 {
		t.Fatalf("admission %d %s", status, first)
	}
	status, again := hookTCP(t, server, d.ID, key, "delivery-one", `{"action":"changed"}`, false, "issues")
	if status != 200 || !bytes.Equal(first, again) {
		t.Fatalf("redelivery changed: %d %s / %s", status, again, first)
	}
	status, raw = hookTCP(t, server, d.ID, key, "delivery-two", `{"action":"opened","repository":{"full_name":"synthetic/repo"}}`, false, "issues")
	if status != 409 || !bytes.Contains(raw, []byte("webhook_busy")) {
		t.Fatalf("busy %d %s", status, raw)
	}
	_, runs, err = st.Routines(context.Background(), f.key)
	if err != nil || len(runs) != 1 || runs[0].DeliveryID != "delivery-one" {
		t.Fatal("dedupe failed", runs, err)
	}
	archive, err := st.ExportArchive(context.Background(), f.key)
	encoded, _ := json.Marshal(archive)
	if err != nil || bytes.Contains(encoded, []byte(key)) {
		t.Fatal("export exposed hook secret", err)
	}
	// Read-only devices cannot retrieve secrets, even though routine metadata is readable.
	hook := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, "reader-hook"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw = routineHumanTCP(t, f, server, "POST", "/v1/project-assistants/"+f.key.BotID+"/routines/"+d.ID+"/hook", f.readonly, hook)
	if status != 403 || bytes.Contains(raw, []byte(key)) {
		t.Fatalf("reader key leak %d %s", status, raw)
	}
}

func TestRoutineHookTCPConcurrentRedeliveryReturnsOneRun(t *testing.T) {
	f, _, server := newRoutineHumanFixture(t)
	d, _, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	type response struct {
		status int
		body   []byte
		err    error
	}
	answers := make(chan response, 12)
	start := make(chan struct{})
	for range 12 {
		go func() {
			<-start
			r, _ := http.NewRequest("POST", server.URL+"/hooks/routines/"+d.ID, strings.NewReader(`{}`))
			r.Header.Set("Authorization", "Bearer "+key)
			r.Header.Set("X-Delivery-Id", "concurrent-delivery")
			res, err := server.Client().Do(r)
			if err != nil {
				answers <- response{err: err}
				return
			}
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			answers <- response{status: res.StatusCode, body: body, err: err}
		}()
	}
	close(start)
	var first []byte
	for range 12 {
		answer := <-answers
		if answer.err != nil || answer.status != 200 {
			t.Errorf("concurrent redelivery %d %s %v", answer.status, answer.body, answer.err)
			continue
		}
		if first == nil {
			first = answer.body
		} else if !bytes.Equal(first, answer.body) {
			t.Errorf("concurrent delivery changed run identity: %s / %s", first, answer.body)
		}
	}
	_, runs, err := store.New(f.s.store.ProjectAssistantDatabase()).Routines(context.Background(), f.key)
	if err != nil || len(runs) != 1 {
		t.Fatal("concurrent delivery duplicated queue", runs, err)
	}
}

func TestRoutineHookTCPPauseRotateDeleteAndBounds(t *testing.T) {
	f, _, server := newRoutineHumanFixture(t)
	d, page, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	base := "/v1/project-assistants/" + f.key.BotID + "/routines/" + d.ID
	q := contract.ProjectAssistantRoutineHookRequest{Command: f.command(pa.EditOperation, "rotate-hook"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version, Rotate: true}
	status, raw := routineHumanTCP(t, f, server, "POST", base+"/hook", f.lan, q)
	if status != 200 {
		t.Fatalf("paired device rotation %d %s", status, raw)
	}
	var rotated contract.ProjectAssistantRoutineHookResponse
	_ = json.Unmarshal(raw, &rotated)
	if rotated.Key == key || len(rotated.Key) != len(key) {
		t.Fatal("rotation retained old key")
	}
	status, raw = routineHumanTCP(t, f, server, "POST", base+"/hook", f.lan, q)
	var replay contract.ProjectAssistantRoutineHookResponse
	_ = json.Unmarshal(raw, &replay)
	if status != 200 || replay.Key != rotated.Key {
		t.Fatal("rotation replay rotated twice", status)
	}
	if status, _ = hookTCP(t, server, d.ID, key, "", `{}`, false, ""); status != 401 {
		t.Fatal("old key accepted", status)
	}
	if status, _ = hookTCP(t, server, d.ID, rotated.Key, "", strings.Repeat("x", pa.MaxRoutineWebhookBodyBytes+1), false, ""); status != 413 {
		t.Fatal("oversize accepted", status)
	}
	if status, _ = hookTCP(t, server, d.ID, rotated.Key, "", `[]`, false, ""); status != 400 {
		t.Fatal("non-object accepted", status)
	}
	// JSON quoting/HTML escaping may expand storage beyond the original body.
	// The upstream ceiling applies to original delivery bytes, not that encoding.
	if status, raw = hookTCP(t, server, d.ID, rotated.Key, "expanded-json", `{"issue":{"title":"`+strings.Repeat("<", pa.MaxRoutineWebhookBodyBytes/3)+`"}}`, true, "issues"); status != 200 {
		t.Fatalf("valid bounded body rejected after normalization %d %s", status, raw)
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	pause := contract.ProjectAssistantRoutineEnabledRequest{Command: f.command(pa.EditOperation, "pause-hook"), Fence: page.Fence, ID: d.ID, ExpectedRevision: page.Definitions[0].Version, Enabled: false}
	status, raw = routineHumanTCP(t, f, server, "POST", base+"/enabled", f.local, pause)
	if status != 200 {
		t.Fatalf("pause %d %s", status, raw)
	}
	status, raw = hookTCP(t, server, d.ID, rotated.Key, "", `{}`, false, "")
	if status != 409 || !bytes.Contains(raw, []byte("webhook_paused")) {
		t.Fatalf("paused %d %s", status, raw)
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	del := contract.ProjectAssistantRoutineActionRequest{Command: f.command(pa.EditOperation, "delete-hook"), Fence: page.Fence, ID: d.ID, ExpectedRevision: page.Definitions[0].Version}
	status, raw = routineHumanTCP(t, f, server, "DELETE", base, f.local, del)
	if status != 200 {
		t.Fatalf("delete %d %s", status, raw)
	}
	if status, _ = hookTCP(t, server, d.ID, rotated.Key, "", `{}`, false, ""); status != 401 {
		t.Fatal("deleted key survived", status)
	}
	var count int
	if err := f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*) FROM project_assistant_routine_hooks`).Scan(&count); err != nil || count != 0 {
		t.Fatal("secret survived delete", count, err)
	}
}

func TestRoutineHookTCPLifecycleKeepsMainRunMain(t *testing.T) {
	f, p, server := newRoutineHumanFixture(t)
	paServerOK(t, f.send(t, f.local, "hook-main-before"))
	routineHumanIdle(t, f)
	d, _, key := saveHookRoutine(t, f, server, contract.ProjectAssistantRoutineTrigger{Type: "webhook"})
	status, raw := hookTCP(t, server, d.ID, key, "main-run-main", `{"text":"synthetic event","id":9007199254740993}`, false, "")
	if status != 200 {
		t.Fatalf("delivery %d %s", status, raw)
	}
	until := time.Now().Add(5 * time.Second)
	for {
		page := routineHumanPage(t, f, server, d.ID, "", 100)
		if len(page.Runs) == 1 && page.Runs[0].State == "succeeded" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("hook did not run", page.Runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Reclaiming published run bookkeeping must not forget delivery identity.
	if _, err := f.s.store.ProjectAssistantDatabase().Exec(`DELETE FROM project_assistant_routine_runs WHERE published=1`); err != nil {
		t.Fatal(err)
	}
	status, replay := hookTCP(t, server, d.ID, key, "main-run-main", `{"text":"synthetic event","id":9007199254740993}`, false, "")
	if status != 200 || !bytes.Equal(raw, replay) {
		t.Fatalf("run pruning forgot delivery: %d %s", status, replay)
	}
	paServerOK(t, f.send(t, f.local, "hook-main-after"))
	routineHumanIdle(t, f)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.opens) != 3 || p.opens[0].Binding.SessionSlot != "" || p.opens[1].Binding.SessionSlot == "" || p.opens[2].Binding.SessionSlot != "" || !p.opens[2].Resume {
		t.Fatalf("lost isolation: %+v", p.opens)
	}
	if !strings.Contains(p.sends[1], "untrusted quoted JSON data") || !strings.Contains(p.sends[1], "synthetic event") || !strings.Contains(p.sends[1], "9007199254740993") {
		t.Fatal("event missing from prompt", p.sends)
	}
}
