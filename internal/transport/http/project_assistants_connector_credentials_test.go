package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/botcapabilities"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func connectorCredentialHTTPAction(f *paServerFixture, g contract.ProjectAssistantGetResponse, ref contract.ProjectAssistantExtensionRef, action, target, id string) map[string]any {
	return map[string]any{
		"command":          f.command(pa.ConnectorCredentialOperation, id),
		"fence":            contract.ProjectAssistantFence{Key: g.Bot.Key, Project: g.Bot.Project, ConversationID: g.Conversation.ID, BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation},
		"expected_version": g.Bot.Version, "ref": ref, "action": action, "target": target,
	}
}

func connectorCredentialHTTPResult(t *testing.T, f *paServerFixture, path string, q map[string]any) (pa.ConnectorCredential, []byte) {
	t.Helper()
	w := f.request(t, "POST", path, f.local, q)
	paServerOK(t, w)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response may be cached")
	}
	var response struct {
		Credential pa.ConnectorCredential `json:"credential"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Credential, append([]byte{}, w.Body.Bytes()...)
}

func TestConnectorCredentialHTTPNativeIsolationAndFixedOperationReplay(t *testing.T) {
	f, _ := loginCapabilitiesFixture(t)
	g := f.get(t, f.local)
	const value = "SYNTHETIC_CONNECTOR_HTTP_KEY_ONLY"
	var calls, wrongCredential atomic.Int64
	effectReached, releaseEffect := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseEffect) }) }
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.Header.Get("Authorization") != value || r.Header.Get("Authorization") == "Bearer "+f.local {
			wrongCredential.Add(1)
			http.Error(w, "fixture credential rejected", 403)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || bytes.Contains(body, []byte(value)) || bytes.Contains(body, []byte(f.local)) {
			wrongCredential.Add(1)
			http.Error(w, "fixture credential leak", 400)
			return
		}
		if call == 2 {
			// The backend has consumed tools/call and recorded its effect. Hold
			// only publication while the credential is revoked through HTTP.
			close(effectReached)
			select {
			case <-releaseEffect:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"synthetic fixture connector"}]}}`)
	}))
	defer target.Close()
	defer release()
	definitions, _ := json.Marshal([]botcapabilities.Connector{{ID: "synthetic", Name: "fixture-connector", URL: target.URL, CredentialHeader: "Authorization", Project: paDomainProjectScope(g.Bot.Project), Agent: pa.Codex, Enabled: true}})
	if err := os.WriteFile(filepath.Join(f.s.cfg.Dir, "project-assistants", "connectors.json"), definitions, 0600); err != nil {
		t.Fatal(err)
	}
	base := "/v1/project-assistants/" + f.key.BotID
	path := base + "/extensions?" + url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {g.Bot.ConversationID}}.Encode()
	w := f.request(t, "GET", path, f.local, nil)
	paServerOK(t, w)
	catalog := paHTTPDecode[contract.ProjectAssistantExtensionsResponse](t, w).Catalog
	var ref contract.ProjectAssistantExtensionRef
	for _, offer := range catalog.Items {
		if offer.ID == "connector:synthetic" {
			ref = contract.ProjectAssistantExtensionRef{ID: offer.ID, Fingerprint: offer.Fingerprint}
		}
	}
	if ref.ID == "" {
		t.Fatal("credential-backed source definition not offered")
	}
	selection := contract.ProjectAssistantExtensionSelection{Skills: []contract.ProjectAssistantExtensionRef{}, Connectors: []contract.ProjectAssistantExtensionRef{ref}}
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "select-credential-connector"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Extensions: &selection}}
	paServerOK(t, f.request(t, "PATCH", base, f.local, edit))
	g = f.get(t, f.local)
	prepared, _ := connectorCredentialHTTPResult(t, f, base+"/connector-credential", connectorCredentialHTTPAction(f, g, ref, "prepare", "", "connector-prepare"))
	if prepared.Handoff == nil || prepared.State != "prepared" {
		t.Fatal("missing explicit connector target", prepared)
	}
	submit := connectorCredentialHTTPAction(f, g, ref, "submit", prepared.Handoff.ID, "connector-submit")
	submit["value"] = value
	if readonly := f.request(t, "POST", base+"/connector-secret", f.readonly, submit); readonly.Code < 400 {
		t.Fatal("read-only principal submitted a connector credential")
	}
	accepted, receipt := connectorCredentialHTTPResult(t, f, base+"/connector-secret", submit)
	if accepted.State != "accepted" {
		t.Fatal("submission did not return original acceptance", accepted)
	}
	for _, guess := range []string{"A_DIFFERENT_SYNTHETIC_GUESS", ""} {
		submit["value"] = guess
		_, replay := connectorCredentialHTTPResult(t, f, base+"/connector-secret", submit)
		if !bytes.Equal(replay, receipt) {
			t.Fatal("fixed-operation replay became a credential oracle")
		}
	}
	status, _ := connectorCredentialHTTPResult(t, f, base+"/connector-credential", connectorCredentialHTTPAction(f, g, ref, "status", "", "connector-status"))
	if status.State != "ready" {
		t.Fatal("encrypted credential not stored", status)
	}
	sent := f.send(t, f.local, "connector-credential-native-turn")
	paServerOK(t, sent)
	turn := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, sent).Receipt.ResourceID
	x := fallbackHTTPWait(t, f)
	found := false
	for _, message := range x.Messages {
		found = found || message.TurnID == turn && strings.Contains(message.Text, "connector=true")
	}
	if !found || calls.Load() != 1 || wrongCredential.Load() != 0 {
		t.Fatalf("credential did not cross actual native proxy only to its backend: delivered=%t calls=%d wrong=%d turns=%+v", found, calls.Load(), wrongCredential.Load(), x.Turns)
	}
	// A committed backend effect cannot publish after credential revocation or
	// become an automatic retry. The native fixture rejects the withheld reply.
	second := f.send(t, f.local, "connector-credential-inflight-revocation")
	paServerOK(t, second)
	secondTurn := paHTTPDecode[contract.ProjectAssistantReceiptResponse](t, second).Receipt.ResourceID
	paResilienceAwait(t, func() bool {
		select {
		case <-effectReached:
			return true
		default:
			return false
		}
	})
	g = f.get(t, f.local)
	removed, _ := connectorCredentialHTTPResult(t, f, base+"/connector-credential", connectorCredentialHTTPAction(f, g, ref, "remove", "", "connector-remove-after-effect"))
	if removed.State != "removed" {
		t.Fatal("credential was not revoked during the committed backend effect", removed)
	}
	release()
	x = fallbackHTTPWait(t, f)
	for _, completed := range x.Turns {
		if completed.ID == secondTurn && completed.State == pa.TurnSucceeded {
			t.Fatal("revoked in-flight connector published native success")
		}
	}
	for _, message := range x.Messages {
		if message.TurnID == secondTurn && message.Kind == pa.AssistantMessage {
			t.Fatal("revoked in-flight connector published an assistant reply")
		}
	}
	if calls.Load() != 2 || wrongCredential.Load() != 0 {
		t.Fatal("an uncertain backend effect was retried or received another credential")
	}
	var holds int
	if err := f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*) FROM project_assistant_connector_holds`).Scan(&holds); err != nil || holds != 0 {
		t.Fatal("verified native shutdown did not release its exact credential holds", holds, err)
	}
	exported := f.request(t, "GET", base+"/export?machine_id="+f.key.MachineID, f.local, nil)
	paServerOK(t, exported)
	hash := sha256.Sum256([]byte(value))
	forbidden := []string{value, "A_DIFFERENT_SYNTHETIC_GUESS", hex.EncodeToString(hash[:])}
	for _, secret := range forbidden {
		if bytes.Contains(exported.Body.Bytes(), []byte(secret)) {
			t.Fatal("Bot export exposed connector secret or a value fingerprint")
		}
	}
	if err := filepath.WalkDir(f.s.cfg.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range forbidden {
			if bytes.Contains(raw, []byte(secret)) {
				t.Error("owned source fixture persisted connector secret or a value fingerprint")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
