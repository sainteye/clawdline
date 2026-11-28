package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The access/resume matrix composes only production pieces after the
// verified/decrypted envelope seam: cloudops.Bridge decodes the real Cloud
// word, cloudtransport.Router dispatches it through the HTTP gate, and the
// Server/Host/Service/SQLite store decide. Local device clients use a real
// 127.0.0.1 listener. The provider is synthetic; no relay, browser, physical
// device or model is contacted. Nothing here is encrypted relay acceptance.

type paMatrixProvider struct {
	mu     sync.Mutex
	sends  []string
	opens  []app.OpenRequest
	caps   func()
	hold   chan struct{}
	signal chan string
}

func (p *paMatrixProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	p.mu.Lock()
	barrier := p.caps
	p.mu.Unlock()
	if barrier != nil {
		barrier()
	}
	c := paServerCaps(b)
	c.Resume = true
	return c, nil
}

func (p *paMatrixProvider) Open(_ context.Context, q app.OpenRequest, _ func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.mu.Lock()
	p.opens = append(p.opens, q)
	p.mu.Unlock()
	return &paMatrixSession{p: p}, nil
}

func (p *paMatrixProvider) setCaps(barrier func()) {
	p.mu.Lock()
	p.caps = barrier
	p.mu.Unlock()
}

func (p *paMatrixProvider) counts() (sends, opens int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sends), len(p.opens)
}

type paMatrixSession struct {
	p    *paMatrixProvider
	text string
	hold chan struct{}
}

func (*paMatrixSession) SessionID() string { return "synthetic-matrix-session" }
func (s *paMatrixSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.p.mu.Lock()
	s.p.sends = append(s.p.sends, q.Text)
	s.hold, s.text = s.p.hold, q.Text
	signal := s.p.signal
	s.p.mu.Unlock()
	if signal != nil {
		signal <- q.Text
	}
	return s, nil
}
func (s *paMatrixSession) Wait(ctx context.Context) (app.PromptResult, error) {
	if s.hold != nil {
		select {
		case <-s.hold:
		case <-ctx.Done():
			return app.PromptResult{}, ctx.Err()
		}
	}
	return app.PromptResult{FinalText: "Synthetic reply to " + s.text}, nil
}
func (*paMatrixSession) Answer(context.Context, string, string) error { return nil }
func (*paMatrixSession) Cancel(context.Context) error                 { return nil }
func (*paMatrixSession) Close(context.Context) error                  { return nil }

type paMatrix struct {
	*paServerFixture
	p      *paMatrixProvider
	server *httptest.Server
	bridge cloudops.Bridge
	pins   map[string]adaptercloud.PinnedDevice
	privs  map[string]ed25519.PrivateKey
	seq    atomic.Uint64
}

func newPAMatrix(t *testing.T) *paMatrix {
	t.Helper()
	f := newPAServerFixture(t, true)
	p := &paMatrixProvider{}
	f.s.projectAssistants.provider = func() (app.Provider, error) { f.providerCalls.Add(1); return p, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	server := httptest.NewServer(f.h)
	t.Cleanup(server.Close)
	if !strings.HasPrefix(server.URL, "http://127.0.0.1:") {
		t.Fatalf("matrix listener is not loopback TCP: %s", server.URL)
	}
	return &paMatrix{paServerFixture: f, p: p, server: server, pins: map[string]adaptercloud.PinnedDevice{}, privs: map[string]ed25519.PrivateKey{},
		bridge: cloudops.Bridge{MachineID: f.key.MachineID, Router: f.cloudRouter(), AllowCommands: func() bool { return true }}}
}

func (m *paMatrix) pinned() *adaptercloud.PinnedStore {
	return adaptercloud.NewPinnedStore(filepath.Join(m.s.cfg.Dir, cloudkeys.DirName))
}

// pair records a completed local pairing through the production pin store.
// Calling it again for the same sender is a fresh re-pair with a new key.
func (m *paMatrix) pair(t *testing.T, sender, scope string, invited bool) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Unix()
	if old, ok := m.pins[sender]; ok && old.PairedAt >= at {
		at = old.PairedAt + 1
	}
	pin := adaptercloud.PinnedDevice{DeviceID: sender, PublicKey: base64.StdEncoding.EncodeToString(pub), Fingerprint: domaincloud.Fingerprint(pub), PairedAt: at, Scope: scope, Invited: invited}
	if err := m.pinned().Pin(m.identity.AccountID, pin); err != nil {
		t.Fatal(err)
	}
	m.pins[sender], m.privs[sender] = pin, priv
}

func (m *paMatrix) grant(t *testing.T, sender string) adaptercloud.AssistantViewerGrant {
	t.Helper()
	key, _ := m.pins[sender].Key()
	g, err := m.pinned().VerifiedProjectAssistantGrant(m.identity.AccountID, sender, key)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// word sends one Cloud word through the production decode and dispatch path,
// starting at the verified sender seam the relay link would hand it.
func (m *paMatrix) word(t *testing.T, sender, word string, fields any) (cloudops.Answer, json.RawMessage) {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	body["type"] = "project-assistants." + word
	body["session"] = cloudops.MachineReplySession
	body["request"] = fmt.Sprintf("matrix-read-%d", m.seq.Add(1))
	if c, ok := body["command"].(map[string]any); ok {
		body["request"] = c["idempotency_key"]
	}
	plain, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var key ed25519.PublicKey
	if pin, ok := m.pins[sender]; ok {
		decoded, _ := base64.StdEncoding.DecodeString(pin.PublicKey)
		key = ed25519.PublicKey(decoded)
	}
	a := m.bridge.Handle(context.Background(), cloudops.Command{Channel: "ctl/" + m.key.MachineID, Class: cloudops.ClassCtl, Sender: sender, VerifiedKey: key, Sequence: m.seq.Add(1), Plaintext: plain})
	var envelope struct {
		Body json.RawMessage `json:"body"`
	}
	_ = json.Unmarshal(a.Payload, &envelope)
	return a, envelope.Body
}

// paMatrixCode is the refusal code a viewer would act on: the Bridge code, the
// published error, or the source's typed Project Assistant failure body.
func paMatrixCode(a cloudops.Answer, inner json.RawMessage) string {
	var source contract.ProjectAssistantErrorResponse
	if json.Unmarshal(inner, &source) == nil && source.Error.Code != "" {
		return string(source.Error.Code)
	}
	var outer struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(a.Payload, &outer) == nil && outer.Error.Code != "" {
		return outer.Error.Code
	}
	return a.Code
}

func (m *paMatrix) tcp(t *testing.T, method, path, token string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, m.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		var envelope struct {
			Command contract.ProjectAssistantCommand `json:"command"`
		}
		_ = json.Unmarshal(raw, &envelope)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", envelope.Command.IdempotencyKey)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	res, err := m.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, data
}

func paMatrixFailure(t *testing.T, raw []byte) string {
	t.Helper()
	var failure contract.ProjectAssistantErrorResponse
	if err := json.Unmarshal(raw, &failure); err != nil {
		t.Fatalf("not a typed failure: %s", raw)
	}
	return string(failure.Error.Code)
}

func (m *paMatrix) fence(t *testing.T) contract.ProjectAssistantFence {
	t.Helper()
	g := m.get(t, m.local)
	return paWireFence(pa.Fence{Key: m.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID, BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation})
}

func (m *paMatrix) sendBody(t *testing.T, id, text string) contract.ProjectAssistantSendRequest {
	return contract.ProjectAssistantSendRequest{Command: m.command(pa.SendOperation, id), Fence: m.fence(t), Text: text}
}

func (m *paMatrix) readBody(t *testing.T, id string, seq int64) contract.ProjectAssistantReadRequest {
	g := m.get(t, m.local)
	return contract.ProjectAssistantReadRequest{Command: m.command(pa.ReadOperation, id), Cursor: contract.ProjectAssistantReadCursor{Key: paWireBotKey(m.key), ConversationID: g.Conversation.ID, ReaderID: "spoofed-reader", MessageSeq: seq}}
}

func (m *paMatrix) export(t *testing.T) app.Export {
	t.Helper()
	x, err := featurestore.New(m.s.store.ProjectAssistantDatabase()).Export(context.Background(), m.key)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func paMatrixReceipt(t *testing.T, raw []byte) contract.ProjectAssistantReceipt {
	t.Helper()
	var r contract.ProjectAssistantReceiptResponse
	if err := json.Unmarshal(raw, &r); err != nil || r.Receipt.ID == "" {
		t.Fatalf("no receipt: %s", raw)
	}
	return r.Receipt
}

func paMatrixCursor(t *testing.T, raw []byte) contract.ProjectAssistantReadCursor {
	t.Helper()
	var r contract.ProjectAssistantReadResponse
	if err := json.Unmarshal(raw, &r); err != nil || r.Cursor.ReaderID == "" {
		t.Fatalf("no read cursor: %s", raw)
	}
	return r.Cursor
}

func (m *paMatrix) getPath() string {
	return "/v1/project-assistants/" + m.key.BotID + "?machine_id=" + m.key.MachineID
}

func (m *paMatrix) viewFields(t *testing.T, mode contract.ProjectAssistantReadViewMode, cursor string, size int64) contract.ProjectAssistantReadViewRequest {
	g := m.get(t, m.local)
	return contract.ProjectAssistantReadViewRequest{Key: paWireBotKey(m.key), ConversationID: g.Conversation.ID, Mode: mode, Cursor: cursor, PageSize: size}
}

// Matrix rows A1-A6 in docs/project-assistants-cloud-access-verification.md.
func TestProjectAssistantCloudAccessMatrixIdentityGrantsAndSpoofing(t *testing.T) {
	m := newPAMatrix(t)
	m.pair(t, "owner", adaptercloud.ScopeOperate, false)
	m.pair(t, "reader", adaptercloud.ScopeReadOnly, false)
	m.pair(t, "guest-reader", adaptercloud.ScopeReadOnly, true)
	m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
	key := map[string]any{"machine_id": m.key.MachineID, "bot_id": m.key.BotID}
	g := m.get(t, m.local)
	history := map[string]any{"key": key, "conversation_id": g.Conversation.ID, "before_seq": 0, "page_size": 10}

	// A1: every completed pin may read metadata and history through real words.
	for _, sender := range []string{"owner", "reader", "guest-reader", "guest-writer"} {
		for word, fields := range map[string]any{"get": key, "history": history} {
			if a, inner := m.word(t, sender, word, fields); !a.OK() {
				t.Fatalf("A1 %s %s: %d %s", sender, word, a.Status, inner)
			}
		}
	}
	// A1: absent or unpaired senders read nothing.
	for _, sender := range []string{"", "stranger"} {
		if a, inner := m.word(t, sender, "get", key); a.OK() || strings.Contains(string(inner), "Synthetic Bot") {
			t.Fatalf("A1 unpaired %q read metadata: %s", sender, a.Payload)
		}
	}

	// A2: read-only grants keep history and are refused before any effect.
	for _, sender := range []string{"reader", "guest-reader"} {
		a, inner := m.word(t, sender, "send", m.sendBody(t, "denied-"+sender, "Must not run"))
		if a.OK() || paMatrixCode(a, inner) != string(pa.ReadOnly) {
			t.Fatalf("A2 %s send: %d %s", sender, a.Status, a.Payload)
		}
	}
	if sends, opens := m.p.counts(); sends != 0 || opens != 0 || len(m.export(t).Messages) != 0 {
		t.Fatal("A2 read-only refusal reached the provider or store")
	}
	// A2: an invited writer is admitted as itself.
	a, inner := m.word(t, "guest-writer", "send", m.sendBody(t, "guest-write", "Invited writer input"))
	if !a.OK() {
		t.Fatalf("A2 invited writer: %d %s", a.Status, a.Payload)
	}
	receipt := paMatrixReceipt(t, inner)
	routineHumanIdle(t, m.paServerFixture)
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatalf("A2 invited writer sends=%d", sends)
	}

	// A3: the body's reader id is never identity; the grant is.
	readers := map[string]string{}
	for i, sender := range []string{"owner", "reader", "guest-reader", "guest-writer"} {
		a, inner := m.word(t, sender, "read", m.readBody(t, fmt.Sprintf("read-%d", i), 0))
		if !a.OK() {
			t.Fatalf("A3 %s read: %d %s", sender, a.Status, a.Payload)
		}
		c := paMatrixCursor(t, inner)
		if c.ReaderID == "spoofed-reader" || c.ReaderID != m.grant(t, sender).ReaderID {
			t.Fatalf("A3 %s cursor reader %q", sender, c.ReaderID)
		}
		readers[sender] = c.ReaderID
	}
	_, owner := adaptercloud.ProjectAssistantOwnerIdentity(m.identity.AccountID)
	if readers["owner"] != owner || readers["reader"] != owner || readers["guest-reader"] == owner || readers["guest-writer"] == owner || readers["guest-reader"] == readers["guest-writer"] {
		t.Fatalf("A3 reader namespaces: %v", readers)
	}

	// A4: a LAN device over TCP cannot borrow a Cloud viewer, owner, or another
	// reader through headers or body.
	spoof := map[string]string{actorHeader: actorDevice, "X-Reader-ID": owner, "X-Sender": "owner", "X-Account-ID": m.identity.AccountID, "X-Clawdline-Viewer": "owner"}
	path := "/v1/project-assistants/" + m.key.BotID + "/read"
	status, plain := m.tcp(t, "POST", path, m.readonly, m.readBody(t, "lan-read", 0), nil)
	status2, spoofed := m.tcp(t, "POST", path, m.readonly, m.readBody(t, "lan-read-spoof", 0), spoof)
	if status != 200 || status2 != 200 {
		t.Fatalf("A4 LAN read %d %s / %d %s", status, plain, status2, spoofed)
	}
	lan := paMatrixCursor(t, plain).ReaderID
	if paMatrixCursor(t, spoofed).ReaderID != lan || lan == owner || lan == readers["guest-reader"] || lan == readers["guest-writer"] {
		t.Fatalf("A4 LAN reader changed under spoofing: %s", lan)
	}
	status, raw := m.tcp(t, "POST", "/v1/project-assistants/"+m.key.BotID+"/messages", m.readonly, m.sendBody(t, "lan-denied", "Must not run"), spoof)
	// The device gate refuses a read-only token before the Bot route runs.
	if status != 403 || !bytes.Contains(raw, []byte(`"code":"forbidden"`)) || bytes.Contains(raw, []byte("Must not run")) {
		t.Fatalf("A4 LAN read-only send %d %s", status, raw)
	}
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatal("A4 spoofed LAN send reached the provider")
	}

	// A5: a replaced source identity refuses every route before effects, and a
	// restored identity is again the same source.
	ids := adaptercloud.NewIdentityStore(filepath.Join(m.s.cfg.Dir, cloudkeys.DirName))
	replaced := m.identity
	replaced.AccountID = "replacement-account"
	if err := ids.Save(replaced); err != nil {
		t.Fatal(err)
	}
	// Pins belong to the account that paired them, so a Cloud viewer has no
	// grant under the replacement account; a local token meets the changed
	// source identity itself.
	if a, inner := m.word(t, "owner", "get", key); a.OK() || paMatrixCode(a, inner) != string(pa.Revoked) {
		t.Fatalf("A5 replaced identity Cloud get: %d %s", a.Status, a.Payload)
	}
	if a, inner := m.word(t, "guest-writer", "send", m.sendBody2(t, g, "after-replace", "Must not run")); a.OK() || paMatrixCode(a, inner) != string(pa.Revoked) {
		t.Fatalf("A5 replaced identity Cloud send: %d %s", a.Status, a.Payload)
	}
	if status, raw := m.tcp(t, "GET", m.getPath(), m.local, nil, nil); status != 403 || paMatrixFailure(t, raw) != string(pa.ScopeDenied) {
		t.Fatalf("A5 replaced identity TCP get: %d %s", status, raw)
	}
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatal("A5 replaced identity reached provider")
	}
	if err := ids.Save(m.identity); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.word(t, "owner", "get", key); !a.OK() {
		t.Fatalf("A5 restored identity: %d %s", a.Status, a.Payload)
	}

	// A6: the accepted invited-writer receipt is the command's exact identity.
	a, inner = m.word(t, "guest-writer", "send", m.sendBody2(t, g, "guest-write", "Invited writer input"))
	if !a.OK() || paMatrixReceipt(t, inner).ID != receipt.ID {
		t.Fatalf("A6 replay changed receipt: %d %s", a.Status, a.Payload)
	}
	if sends, _ := m.p.counts(); sends != 1 || len(m.export(t).Turns) != 1 {
		t.Fatal("A6 replay created another prompt")
	}
	if m.providerCalls.Load() == 0 {
		t.Fatal("matrix never composed the provider")
	}
}

// sendBody2 builds a send from an earlier snapshot without calling the local
// route, for use while the current source identity is deliberately replaced.
func (m *paMatrix) sendBody2(t *testing.T, g contract.ProjectAssistantGetResponse, id, text string) contract.ProjectAssistantSendRequest {
	fence := pa.Fence{Key: m.key, Project: paDomainProjectScope(g.Bot.Project), ConversationID: g.Conversation.ID, BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation}
	return contract.ProjectAssistantSendRequest{Command: m.command(pa.SendOperation, id), Fence: paWireFence(fence), Text: text}
}

// Matrix rows B1-B4: authority changes between admission and publication or
// effect are interleaved with explicit barriers, never with sleeps.
func TestProjectAssistantCloudAccessMatrixRevocationBarriers(t *testing.T) {
	m := newPAMatrix(t)
	m.pair(t, "guest-reader", adaptercloud.ScopeReadOnly, true)
	m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
	m.pair(t, "owner-two", adaptercloud.ScopeOperate, false)

	// B1: a Cloud read revoked after the SQL read and before publication
	// publishes nothing of the packed snapshot.
	revoked := 0
	m.s.projectAssistants.beforeViewPublish = func() {
		revoked++
		if _, err := m.pinned().Revoke("guest-reader", time.Now()); err != nil {
			t.Error(err)
		}
	}
	a, inner := m.word(t, "guest-reader", "view", m.viewFields(t, contract.ProjectAssistantReadViewModeSnapshot, "", 100))
	if revoked != 1 || a.OK() || strings.Contains(string(a.Payload), "Synthetic Bot") || strings.Contains(string(inner), `"snapshot"`) {
		t.Fatalf("B1 Cloud publication escaped revocation: %d %s", a.Status, a.Payload)
	}
	// B1: the same barrier on a TCP LAN device revokes its gate grant.
	m.s.projectAssistants.beforeViewPublish = func() {
		revoked++
		if err := m.s.gate().auth.Revoke(m.s.gate().auth.Verify(m.lan).Device); err != nil {
			t.Error(err)
		}
	}
	g := m.get(t, m.local)
	status, raw := m.tcp(t, "GET", paViewURL(m.key, g.Conversation.ID, pa.ReadViewSnapshot, "", "", 0, 100), m.lan, nil, nil)
	if revoked != 2 || status != 403 || bytes.Contains(raw, []byte("Synthetic Bot")) {
		t.Fatalf("B1 TCP publication escaped revocation: %d %s", status, raw)
	}
	m.s.projectAssistants.beforeViewPublish = nil
	if a, _ := m.word(t, "guest-reader", "view", m.viewFields(t, contract.ProjectAssistantReadViewModeSnapshot, "", 100)); a.OK() {
		t.Fatal("B1 revoked reader read after barrier removal")
	}

	// B2: a command accepted under one grant is not executed again after a
	// fresh re-pair of the same sender: the replay is the same receipt.
	// A re-pair while the send is still inside runtime admission is defect
	// candidate D1, kept as a private reproducer rather than blessed here.
	body := m.sendBody(t, "accepted-before-repair", "Exactly once across re-pair")
	a, inner = m.word(t, "guest-writer", "send", body)
	if !a.OK() {
		t.Fatalf("B2 first send: %d %s", a.Status, a.Payload)
	}
	first := paMatrixReceipt(t, inner)
	routineHumanIdle(t, m.paServerFixture)
	before := m.grant(t, "guest-writer")
	m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
	after := m.grant(t, "guest-writer")
	if before.AuthorizationRevision == after.AuthorizationRevision || before.PrincipalID != after.PrincipalID || before.ReaderID != after.ReaderID {
		t.Fatal("B2 re-pair did not change only the authorization revision")
	}
	a, inner = m.word(t, "guest-writer", "send", body)
	if !a.OK() || paMatrixReceipt(t, inner).ID != first.ID {
		t.Fatalf("B2 replay: %d %s", a.Status, a.Payload)
	}
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatalf("B2 sends=%d", sends)
	}

	// B3: revocation inside admission refuses before effect, and retrying the
	// unknown command does not create a prompt.
	m.p.setCaps(func() {
		m.p.setCaps(nil)
		if _, err := m.pinned().Revoke("owner-two", time.Now()); err != nil {
			t.Error(err)
		}
	})
	revokedBody := m.sendBody(t, "revoke-at-admission", "Must not run")
	for i := 0; i < 2; i++ {
		a, inner = m.word(t, "owner-two", "send", revokedBody)
		if a.OK() || paMatrixCode(a, inner) != string(pa.Revoked) {
			t.Fatalf("B3 attempt %d: %d %s", i, a.Status, a.Payload)
		}
	}
	if sends, _ := m.p.counts(); sends != 1 || len(m.export(t).Turns) != 1 {
		t.Fatal("B3 revoked admission prompted")
	}

	// B4: a downgrade to read-only while a TCP write is admitted is refused.
	m.pair(t, "owner-three", adaptercloud.ScopeOperate, false)
	m.p.setCaps(func() {
		m.p.setCaps(nil)
		pin := m.pins["owner-three"]
		if err := m.pinned().SetScope(pin.DeviceID, adaptercloud.ScopeReadOnly); err != nil {
			t.Error(err)
		}
	})
	a, inner = m.word(t, "owner-three", "send", m.sendBody(t, "downgrade-at-admission", "Must not run"))
	if a.OK() || paMatrixCode(a, inner) != string(pa.ReadOnly) {
		t.Fatalf("B4 downgraded admission: %d %s", a.Status, a.Payload)
	}
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatal("B4 downgraded admission prompted")
	}
}
