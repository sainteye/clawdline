package botnetwork

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

type assistantTestHost struct {
	target          wire.BotEndpoint
	checks, effects int
	onCheck         func(int)
	onSubmit        func()
}

func (h *assistantTestHost) CheckAssistant(_ context.Context, target wire.BotEndpoint, _ bool) (app.AssistantTargetState, error) {
	h.checks++
	if h.onCheck != nil {
		h.onCheck(h.checks)
	}
	if target != h.target {
		return app.AssistantTargetState{}, wire.AssistantStale
	}
	return app.AssistantTargetState{FeatureEnabled: true, SourceWritable: true, TargetCurrent: true, TargetReachable: true}, nil
}
func (h *assistantTestHost) DirectoryAssistant(_ context.Context, target wire.BotEndpoint, _ []string) ([]app.Entry, error) {
	other := target
	other.AccountID = "other-account"
	return []app.Entry{{Endpoint: target, Name: "Synthetic Bot"}, {Endpoint: other, Name: "Not in scope"}}, nil
}
func (h *assistantTestHost) SubmitAssistant(_ context.Context, call wire.AssistantCall, p wire.VerifiedAssistant, text string, body []byte) (string, error) {
	if p.PrincipalID() == "" || call.Target != h.target || text != "synthetic work" {
		return "", wire.AssistantDenied
	}
	if call.BodyDigest != app.Digest(body) {
		return "", wire.AssistantInvalid
	}
	h.effects++
	if h.onSubmit != nil {
		h.onSubmit()
	}
	return "native-turn-1", nil
}
func (h *assistantTestHost) ResultAssistant(_ context.Context, r app.AssistantReceipt) (app.AssistantReceipt, error) {
	if r.TurnID != "" {
		r.State = "reply_stored"
		r.Reply = "synthetic native reply"
	}
	return r, nil
}

type assistantFixture struct {
	store   *AssistantStore
	host    *assistantTestHost
	key     ed25519.PrivateKey
	grant   wire.AssistantGrant
	policy  app.AssistantPolicy
	call    wire.AssistantCall
	body    []byte
	service *app.AssistantService
	server  *httptest.Server
}

func newAssistantFixture(t *testing.T) *assistantFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewAssistantStore(ctx, db, "synthetic-account", "machine-source")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0).UTC()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, 32))
	target := wire.BotEndpoint{AccountID: "synthetic-account", MachineID: "machine-source", ProjectID: "project-one", BotID: "bot-one", ConversationID: "conversation-one", Generation: 0}
	grant := wire.AssistantGrant{ID: "consent-one", Epoch: 1, KeyID: "gateway-key-one", PublicKey: key.Public().(ed25519.PublicKey), PrincipalID: "assistant-one", AccountID: target.AccountID, MachineID: target.MachineID, ProjectID: target.ProjectID, BotIDs: []string{target.BotID}, AllowList: true, AllowMessage: true, AllowRead: true, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	policy := app.AssistantPolicy{AccountID: target.AccountID, MachineID: target.MachineID, Version: 1, GlobalOptIn: true, GatewayKeyID: grant.KeyID, GatewayPublicKey: grant.PublicKey}
	if err = store.PutAssistantPolicy(ctx, policy, 0); err != nil {
		t.Fatal(err)
	}
	if err = store.PutAssistantGrant(ctx, grant, 0); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"text":"synthetic work"}`)
	call := wire.AssistantCall{Version: wire.BotWireVersion, RequestID: "request-one", Kind: "message", Target: target, GrantID: grant.ID, GrantEpoch: grant.Epoch, KeyID: grant.KeyID, BodyDigest: app.Digest(body), IssuedAt: now.Add(-time.Second).Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	call, err = wire.SignAssistantCall(call, key)
	if err != nil {
		t.Fatal(err)
	}
	host := &assistantTestHost{target: target}
	service, err := app.NewAssistant(app.AssistantOptions{AccountID: target.AccountID, MachineID: target.MachineID, Repository: store, Host: host, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(AssistantHandler{Service: service})
	t.Cleanup(server.Close)
	return &assistantFixture{store: store, host: host, key: key, grant: grant, policy: policy, call: call, body: body, service: service, server: server}
}
func (f *assistantFixture) post(t *testing.T, call wire.AssistantCall, body []byte) (int, app.AssistantResponse, string) {
	t.Helper()
	input, _ := json.Marshal(AssistantRequest{Call: call, Body: string(body)})
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/bot-assistant/call", bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	// A viewer bearer has no authority on this route; the dedicated signature is
	// independently required whether this unrelated header is present or absent.
	req.Header.Set("Authorization", "Bearer x")
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result app.AssistantResponse
	if response.StatusCode == http.StatusOK {
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, result, string(data)
}
func (f *assistantFixture) sign(t *testing.T, call wire.AssistantCall, body []byte) wire.AssistantCall {
	t.Helper()
	call.BodyDigest = app.Digest(body)
	signed, err := wire.SignAssistantCall(call, f.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestAssistantHTTPUsesStoredDedicatedAuthorityAndDurableCorrelatedReceipts(t *testing.T) {
	f := newAssistantFixture(t)
	status, out, data := f.post(t, f.call, f.body)
	if status != http.StatusOK || out.Receipt == nil || out.Receipt.State != "accepted" || out.Receipt.PrincipalID != "assistant-one" {
		t.Fatalf("accepted: %d %+v %s", status, out, data)
	}
	status, _, data = f.post(t, f.call, f.body)
	if status != http.StatusOK || f.host.effects != 1 {
		t.Fatalf("replay executed: %d %d %s", status, f.host.effects, data)
	}
	changedBody := []byte(`{"text":"different work"}`)
	changed := f.sign(t, f.call, changedBody)
	status, _, data = f.post(t, changed, changedBody)
	if status != http.StatusForbidden || !strings.Contains(data, string(pa.IdempotencyConflict)) || f.host.effects != 1 {
		t.Fatalf("changed ID payload: %d %s effects=%d", status, data, f.host.effects)
	}
	read := f.call
	read.RequestID = "read-one"
	read.CorrelationID = f.call.RequestID
	read.Kind = "read"
	read = f.sign(t, read, []byte(`{}`))
	status, out, data = f.post(t, read, []byte(`{}`))
	if status != http.StatusOK || out.Receipt == nil || out.Receipt.Reply != "synthetic native reply" || !out.Receipt.Read {
		t.Fatalf("correlated read: %d %+v %s", status, out, data)
	}
	if strings.Contains(data, "observed") || strings.Contains(data, "acknowledged") {
		t.Fatal("source forged assistant observation")
	}
	list := f.call
	list.RequestID = "list-one"
	list.Kind = "directory"
	list = f.sign(t, list, []byte(`{}`))
	status, out, data = f.post(t, list, []byte(`{}`))
	if status != http.StatusOK || len(out.Bots) != 1 || out.Bots[0].Endpoint != f.call.Target {
		t.Fatalf("scoped directory: %d %+v %s", status, out, data)
	}
	grants, receipts, err := f.store.AssistantUsage(context.Background())
	if err != nil || grants != 1 || receipts != 1 {
		t.Fatalf("actual usage: %d %d %v", grants, receipts, err)
	}
	// A newly opened store object reads retained consent and receipts, without
	// creating a new execution or treating the retained claim as replay permission.
	reopened, err := NewAssistantStore(context.Background(), f.store.db, f.store.account, f.store.machine)
	if err != nil {
		t.Fatal(err)
	}
	principal, _, err := f.service.Verify(context.Background(), f.call, f.body)
	if err != nil {
		t.Fatal(err)
	}
	r, fresh, err := reopened.ClaimAssistant(context.Background(), f.call, principal)
	if err != nil || fresh || r.State != "reply_stored" {
		t.Fatalf("restart replay: %+v %v %v", r, fresh, err)
	}
	var zero wire.VerifiedAssistant
	if _, _, err = reopened.ClaimAssistant(context.Background(), f.call, zero); err != wire.AssistantDenied {
		t.Fatalf("forged JSON principal: %v", err)
	}
}

func TestAssistantHTTPRevocationBeforeEffectAndPublicationWithholdsReply(t *testing.T) {
	for _, stage := range []string{"before-effect", "after-effect", "policy-off-before-effect", "policy-off-after-effect"} {
		t.Run(stage, func(t *testing.T) {
			f := newAssistantFixture(t)
			wantError := wire.AssistantRevoked
			revoke := func() {
				if strings.HasPrefix(stage, "policy-off") {
					policy := f.policy
					policy.Version++
					policy.GlobalOptIn = false
					if err := f.store.PutAssistantPolicy(context.Background(), policy, f.policy.Version); err != nil {
						t.Fatal(err)
					}
					return
				}
				g := f.grant
				g.Epoch++
				g.Revoked = true
				if err := f.store.PutAssistantGrant(context.Background(), g, f.grant.Epoch); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(stage, "policy-off") {
				wantError = wire.AssistantDenied
			}
			if strings.HasSuffix(stage, "before-effect") {
				f.host.onCheck = func(n int) {
					if n == 2 {
						revoke()
					}
				}
			} else {
				f.host.onSubmit = revoke
			}
			status, _, data := f.post(t, f.call, f.body)
			if status != http.StatusForbidden || !strings.Contains(data, string(wantError)) {
				t.Fatalf("revocation result: %d %s", status, data)
			}
			want := 0
			if strings.HasSuffix(stage, "after-effect") {
				want = 1
			}
			if f.host.effects != want {
				t.Fatalf("effects=%d want=%d", f.host.effects, want)
			}
			_, _, _ = f.post(t, f.call, f.body)
			if f.host.effects != want {
				t.Fatal("revoked exact replay executed again")
			}
		})
	}
}

func TestAssistantHTTPUnavailableAndHumanPolicyVersionAreIndependent(t *testing.T) {
	f := newAssistantFixture(t)
	policy := f.policy
	policy.Version++
	policy.GlobalOptIn = false
	if err := f.store.PutAssistantPolicy(context.Background(), policy, 1); err != nil {
		t.Fatal(err)
	}
	status, _, data := f.post(t, f.call, f.body)
	if status != http.StatusForbidden || !strings.Contains(data, string(wire.AssistantDenied)) {
		t.Fatalf("global off: %d %s", status, data)
	}
	if err := f.store.PutAssistantPolicy(context.Background(), policy, 1); pa.Code(err) != pa.StaleVersion {
		t.Fatalf("stale human management: %v", err)
	}
	policy.Version++
	policy.GlobalOptIn = true
	policy.GatewayKeyID = ""
	policy.GatewayPublicKey = nil
	if err := f.store.PutAssistantPolicy(context.Background(), policy, 2); err != nil {
		t.Fatal(err)
	}
	status, _, data = f.post(t, f.call, f.body)
	if status != http.StatusServiceUnavailable || !strings.Contains(data, string(wire.AssistantUnavailable)) || f.host.effects != 0 {
		t.Fatalf("unconfigured gateway: %d %s effects=%d", status, data, f.host.effects)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/bot-assistant/call", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer x")
	recorder := httptest.NewRecorder()
	AssistantHandler{}.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), string(wire.AssistantUnavailable)) {
		t.Fatalf("nil service: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAssistantHTTPVerifiesConsentBeforeInspectingTheHost(t *testing.T) {
	f := newAssistantFixture(t)
	unsigned := f.call
	unsigned.Signature = ""
	status, _, data := f.post(t, unsigned, f.body)
	if status != http.StatusForbidden || !strings.Contains(data, string(wire.AssistantUnverified)) || f.host.checks != 0 {
		t.Fatalf("unsigned call reached target checks: %d %s checks=%d", status, data, f.host.checks)
	}
	outside := f.call
	outside.Target.ProjectID = "another-project"
	status, _, data = f.post(t, f.sign(t, outside, f.body), f.body)
	if status != http.StatusForbidden || !strings.Contains(data, string(wire.AssistantDenied)) || f.host.checks != 0 {
		t.Fatalf("out-of-scope call reached target checks: %d %s checks=%d", status, data, f.host.checks)
	}
}
