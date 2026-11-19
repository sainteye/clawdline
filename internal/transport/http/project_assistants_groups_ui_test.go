package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This is the decrypted Cloud vocabulary boundary, followed by actual TCP
// HTTP and SQLite. It is not a claim about production pairing or encryption.
type groupUIRouter struct{ origin, token string }

// Only the test owns this latch. The browser observes a real persisted
// running turn before allowing the local scripted provider to finish.
type groupUIHeldProvider struct {
	app.Provider
	mu   sync.Mutex
	hold chan struct{}
}

func (p *groupUIHeldProvider) block() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold == nil {
		p.hold = make(chan struct{})
	}
}
func (p *groupUIHeldProvider) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold != nil {
		close(p.hold)
		p.hold = nil
	}
}
func (p *groupUIHeldProvider) Open(ctx context.Context, request app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	inner, err := p.Provider.Open(ctx, request, sink)
	if err != nil {
		return nil, err
	}
	return &groupUIHeldSession{ProviderSession: inner, owner: p}, nil
}

type groupUIHeldSession struct {
	app.ProviderSession
	owner *groupUIHeldProvider
}

func (s *groupUIHeldSession) Send(ctx context.Context, request app.PromptRequest) (app.PendingPrompt, error) {
	pending, err := s.ProviderSession.Send(ctx, request)
	if err != nil {
		return nil, err
	}
	return &groupUIHeldPrompt{PendingPrompt: pending, owner: s.owner}, nil
}

type groupUIHeldPrompt struct {
	app.PendingPrompt
	owner *groupUIHeldProvider
}

func (s *groupUIHeldPrompt) Wait(ctx context.Context) (app.PromptResult, error) {
	s.owner.mu.Lock()
	hold := s.owner.hold
	s.owner.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return app.PromptResult{}, ctx.Err()
		}
	}
	return s.PendingPrompt.Wait(ctx)
}

func (r groupUIRouter) Do(ctx context.Context, in cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	u, _ := url.Parse(r.origin + in.Path)
	q := url.Values{}
	for k, v := range in.Query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, in.Method, u.String(), bytes.NewReader(in.Body))
	if err != nil {
		return cloudops.LocalResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range in.Header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return cloudops.LocalResponse{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	return cloudops.LocalResponse{Status: res.StatusCode, Body: raw, ContentType: res.Header.Get("Content-Type")}, err
}

func TestGroupCloudDecoderReachesRealHTTPAndSQLite(t *testing.T) {
	f := newGroupHTTPFixture(t)
	bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
	call := func(word string, fields map[string]any) json.RawMessage {
		t.Helper()
		envelope := map[string]any{"type": "project-assistants." + word, "session": cloudops.MachineReplySession, "request": "read-" + word}
		for key, value := range fields {
			envelope[key] = value
		}
		if c, ok := fields["command"].(contract.ProjectAssistantCommand); ok {
			envelope["request"] = c.IdempotencyKey
		}
		raw, _ := json.Marshal(envelope)
		answer := bridge.Handle(t.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "synthetic-viewer", Sequence: 1, Plaintext: raw})
		if !answer.OK() {
			t.Fatalf("%s: %d %s %s", word, answer.Status, answer.Code, answer.Payload)
		}
		var result struct {
			Body json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(answer.Payload, &result); err != nil {
			t.Fatal(err)
		}
		return result.Body
	}
	created := call("group-create", map[string]any{"command": f.command(pa.CreateOperation, "cloud-create"), "settings": contract.ProjectAssistantGroupCreateSettings{Target: contract.ProjectAssistantProjectSelection{MachineID: "machine", ProjectID: "project"}, Name: "Cloud room", Members: []contract.ProjectAssistantBotKey{paWireBotKey(f.bots[0].Key), paWireBotKey(f.bots[1].Key)}}})
	var group contract.ProjectAssistantGroupResponse
	if err := json.Unmarshal(created, &group); err != nil {
		t.Fatal(err)
	}
	f.groupID = group.Group.Key.BotID
	if list := call("groups", map[string]any{"machine_id": "machine"}); !bytes.Contains(list, []byte("Cloud room")) {
		t.Fatal(string(list))
	}
	call("group-send", map[string]any{"command": f.command(pa.SendOperation, "cloud-send"), "text": "Review this"})
	deadline := time.Now().Add(3 * time.Second)
	var history json.RawMessage
	for time.Now().Before(deadline) {
		history = call("group-history", map[string]any{"key": group.Group.Key, "before_seq": 0, "page_size": 100})
		if strings.Count(string(history), `"kind":"member"`) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if strings.Count(string(history), `"kind":"member"`) != 2 {
		t.Fatal(string(history))
	}
	var page contract.ProjectAssistantGroupHistoryResponse
	if err := json.Unmarshal(history, &page); err != nil {
		t.Fatal(err)
	}
	original := page.Lines[len(page.Lines)-1]
	reaction := map[string]any{
		"command": f.command(pa.ReactOperation, "cloud-room-react"), "conversation_id": group.Group.ConversationID,
		"group_version": group.Group.Version, "message_seq": original.Seq, "emoji": "👍", "present": true, "expected_revision": 0,
	}
	first := call("group-react", reaction)
	if repeated := call("group-react", reaction); !bytes.Equal(first, repeated) {
		t.Fatalf("replayed reaction changed its receipt: %s / %s", first, repeated)
	}
	history = call("group-history", map[string]any{"key": group.Group.Key, "before_seq": original.Seq + 1, "page_size": 1})
	var reacted contract.ProjectAssistantGroupHistoryResponse
	if err := json.Unmarshal(history, &reacted); err != nil || len(reacted.Lines) != 1 || len(reacted.Lines[0].Reactions) != 1 || !reacted.Lines[0].Reactions[0].Own || reacted.Lines[0].Reactions[0].Emojis[0] != "👍" {
		t.Fatalf("paged source did not publish the reaction: %s %v", history, err)
	}
	var previews contract.ProjectAssistantGroupListResponse
	if err := json.Unmarshal(call("groups", map[string]any{"machine_id": "machine"}), &previews); err != nil || len(previews.Groups) != 1 || previews.Groups[0].LastEntry == nil || previews.Groups[0].LastEntry.Seq != original.Seq || previews.Groups[0].LastEntry.At == "" || previews.Groups[0].Room == nil {
		t.Fatalf("Cloud list did not retain its real latest line: %+v %v", previews, err)
	}
	quoted := map[string]any{"command": f.command(pa.SendOperation, "cloud-quote"), "text": "Explain that result", "quote": map[string]any{"seq": original.Seq}}
	var reply contract.ProjectAssistantGroupSendResponse
	if err := json.Unmarshal(call("group-send", quoted), &reply); err != nil || reply.Entry.Quote == nil || reply.Entry.Quote.Seq != original.Seq || reply.Entry.Quote.Text != original.Text || reply.Entry.Quote.Author == nil || *reply.Entry.Quote.Author != *original.Author {
		t.Fatalf("exact quote was lost across Cloud decode and SQLite: %+v %v", reply, err)
	}
	var replay contract.ProjectAssistantGroupSendResponse
	if err := json.Unmarshal(call("group-send", quoted), &replay); err != nil || replay.Entry.Seq != reply.Entry.Seq || replay.Entry.Quote == nil || replay.Entry.Quote.Text != original.Text {
		t.Fatalf("a Cloud quote replay changed its line: %+v %v", replay, err)
	}
	call("group-edit", map[string]any{"command": f.command(pa.EditOperation, "cloud-edit"), "settings": contract.ProjectAssistantGroupEditSettings{ExpectedVersion: group.Group.Version, Name: "Cloud renamed", Members: []contract.ProjectAssistantBotKey{paWireBotKey(f.bots[0].Key)}}})
	call("group-stop", map[string]any{"command": f.command(pa.StopOperation, "cloud-stop")})
	call("group-delete", map[string]any{"command": f.command(pa.DeleteOperation, "cloud-delete"), "expected_version": group.Group.Version + 1})
}

func TestGroupRoomsHeadlessUI(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_UI_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_GROUP_UI_ARTIFACTS for the installed headless-shell UI acceptance")
	}
	provider := &groupUIHeldProvider{Provider: &routineHumanProvider{}}
	f := newGroupHTTPFixture(t, provider)
	t.Cleanup(provider.release)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" || r.URL.Path == "/release" {
			if r.URL.Path == "/hold" {
				provider.block()
			} else {
				provider.release()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"fixture":true}`))
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
		answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "synthetic-viewer", Sequence: 1, Plaintext: data})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Payload)
	}))
	defer cloud.Close()
	timeout := 3 * time.Minute
	if os.Getenv("CLAWDLINE_GROUP_UI_PREVIEW") == "1" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-groups.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+f.server.URL, "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_ARTIFACTS="+dir, "BOTS_WORK="+filepath.Join(filepath.Dir(dir), "work"))
	if os.Getenv("CLAWDLINE_GROUP_UI_PREVIEW") == "1" {
		cmd.Env = append(cmd.Env, "BOTS_PREVIEW=1")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("headless group UI: %v\n%s", err, out)
	}
	if os.Getenv("CLAWDLINE_GROUP_UI_PREVIEW") == "1" {
		t.Skip("task-only manual preview closed; no automated UI assertions ran")
	}
	t.Log(string(out))
}
