package http

import (
	"context"
	"encoding/json"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

type reactionHTTPPending struct{ tools app.ReactionTools }

func (p reactionHTTPPending) Wait(ctx context.Context) (app.PromptResult, error) {
	_, e := p.tools.React(ctx, "👀", "")
	return app.PromptResult{FinalText: "Scripted final answer"}, e
}
func reactionHTTPFixture(t *testing.T, native ...bool) *paServerFixture {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		f.providerCalls.Add(1)
		return paServerProvider{open: func(_ context.Context, opened app.OpenRequest) (app.ProviderSession, error) {
			return &fallbackHTTPSession{send: func(app.PromptRequest) (app.PendingPrompt, error) {
				if len(native) > 0 && native[0] {
					return reactionHTTPPending{tools: opened.Tools.(app.ReactionTools)}, nil
				}
				return fallbackHTTPPending{}, nil
			}}, nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "reaction-message"))
	fallbackHTTPWait(t, f)
	return f
}

func TestReactionsCrossAuthenticatedHTTPHostSQLite(t *testing.T) {
	f := reactionHTTPFixture(t)
	before := fallbackHTTPWait(t, f)
	calls := f.providerCalls.Load()
	path := "/v1/project-assistants/" + f.key.BotID + "/react"
	for _, m := range before.Messages {
		if !pa.Reactable(m) {
			continue
		}
		q := contract.ProjectAssistantReactRequest{Command: f.command(pa.ReactOperation, "react-"+m.ID), Fence: paWireFence(m.Fence), MessageID: m.ID, Emoji: "👍", Present: true, ExpectedRevision: 0}
		for _, token := range []string{"", f.readonly} {
			r := f.request(t, "POST", path, token, q)
			if r.Code < 400 {
				t.Fatal("read-only reaction", r.Body.String())
			}
		}
		r := f.request(t, "POST", path, f.local, q)
		paServerOK(t, r)
		retry := f.request(t, "POST", path, f.local, q)
		paServerOK(t, retry)
		if r.Body.String() != retry.Body.String() {
			t.Fatal("same ID changed receipt")
		}
		q.Present = false
		r = f.request(t, "POST", path, f.local, q)
		if r.Code != http.StatusConflict {
			t.Fatal("changed payload accepted", r.Code, r.Body.String())
		}
		q.Command = f.command(pa.ReactOperation, "remove-"+m.ID)
		q.ExpectedRevision = 1
		paServerOK(t, f.request(t, "POST", path, f.local, q))
	}
	after := fallbackHTTPWait(t, f)
	if !reflect.DeepEqual(before.Bot, after.Bot) || !reflect.DeepEqual(before.Conversation, after.Conversation) || !reflect.DeepEqual(before.Turns, after.Turns) || !reflect.DeepEqual(before.Messages, after.Messages) || f.providerCalls.Load() != calls {
		t.Fatal("human reactions mutated native/content state")
	}
	g := f.get(t, f.local)
	r := f.request(t, "GET", path[:len(path)-5]+"messages?machine_id="+f.key.MachineID+"&conversation_id="+g.Conversation.ID+"&before_seq=0&page_size=10", f.local, nil)
	paServerOK(t, r)
	var page contract.ProjectAssistantHistoryResponse
	if e := json.Unmarshal(r.Body.Bytes(), &page); e != nil {
		t.Fatal(e)
	}
	for _, m := range page.Items {
		if len(m.Reactions) != 1 || !m.Reactions[0].Own || len(m.Reactions[0].Emojis) != 0 || m.Reactions[0].Revision != 2 {
			t.Fatal("durable identity/removal", r.Body.String())
		}
	}
}

func TestProjectAssistantReactionsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_REACTIONS_BROWSER_DIR")
	if dir == "" {
		return
	}
	f := reactionHTTPFixture(t, true)
	before := fallbackHTTPWait(t, f)
	calls := f.providerCalls.Load()
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-reactions.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	out, e := cmd.CombinedOutput()
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if e != nil {
		t.Fatalf("browser: %v\n%s", e, out)
	}
	after := fallbackHTTPWait(t, f)
	if !reflect.DeepEqual(before.Bot, after.Bot) || !reflect.DeepEqual(before.Conversation, after.Conversation) || !reflect.DeepEqual(before.Turns, after.Turns) || f.providerCalls.Load() != calls {
		t.Fatal("browser reactions changed execution")
	}
}

func TestReactionsSeparateAuthenticatedActorsAndRetainRevokedOutcome(t *testing.T) {
	f := reactionHTTPFixture(t)
	x := fallbackHTTPWait(t, f)
	m := x.Messages[0]
	path := "/v1/project-assistants/" + f.key.BotID + "/react"
	q := contract.ProjectAssistantReactRequest{Command: f.command(pa.ReactOperation, "owner-add"), Fence: paWireFence(m.Fence), MessageID: m.ID, Emoji: "👍", Present: true, ExpectedRevision: 0}
	paServerOK(t, f.request(t, "POST", path, f.local, q))
	q.Command = f.command(pa.ReactOperation, "paired-add")
	q.Emoji = "🎉"
	paServerOK(t, f.request(t, "POST", path, f.lan, q))
	read := func(token string) contract.ProjectAssistantHistoryResponse {
		r := f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/messages?machine_id="+f.key.MachineID+"&conversation_id="+m.Fence.ConversationID+"&before_seq=0&page_size=10", token, nil)
		paServerOK(t, r)
		var p contract.ProjectAssistantHistoryResponse
		if e := json.Unmarshal(r.Body.Bytes(), &p); e != nil {
			t.Fatal(e)
		}
		return p
	}
	for _, token := range []string{f.local, f.lan} {
		p := read(token)
		found := false
		for _, message := range p.Items {
			if message.ID != m.ID {
				continue
			}
			own := 0
			for _, a := range message.Reactions {
				if a.Own {
					own++
				}
			}
			if own != 1 || len(message.Reactions) != 2 {
				t.Fatal("actor identities were merged", p)
			}
			found = true
		}
		if !found {
			t.Fatal("missing message")
		}
	}
	q.Command = f.command(pa.ReactOperation, "owner-remove")
	q.Emoji = "👍"
	q.Present = false
	q.ExpectedRevision = 1
	paServerOK(t, f.request(t, "POST", path, f.local, q))
	q.Command = f.command(pa.ReactOperation, "paired-lost-reply")
	q.Emoji = "👀"
	q.Present = true
	q.ExpectedRevision = 1
	f.s.projectAssistants.beforeViewPublish = func() {
		device := f.s.gate().auth.Verify(f.lan).Device
		if e := f.s.gate().auth.Revoke(device); e != nil {
			t.Error(e)
		}
	}
	r := f.request(t, "POST", path, f.lan, q)
	var refused contract.ProjectAssistantErrorResponse
	_ = json.Unmarshal(r.Body.Bytes(), &refused)
	if string(refused.Error.Code) != "pa_outcome_unknown" {
		t.Fatal("post-commit refusal was reported as non-delivery", r.Body.String())
	}
	f.s.projectAssistants.beforeViewPublish = nil
	p := read(f.local)
	for _, message := range p.Items {
		if message.ID == m.ID {
			for _, a := range message.Reactions {
				if !a.Own && len(a.Emojis) != 2 {
					t.Fatal("other actor's retained state changed", p)
				}
			}
		}
	}
}
