package http

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type handoffSourceProbe struct {
	mu      sync.Mutex
	opens   []bool
	prompts []string
	opening []string
	before  func(context.Context)
	waitErr error
}

func (p *handoffSourceProbe) provider() (app.Provider, error) {
	return paServerProvider{
		capability: func(b pa.Bot) (pa.Capabilities, error) {
			caps := paServerCaps(b)
			caps.Resume = true
			return caps, nil
		},
		open: func(ctx context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			p.mu.Lock()
			p.opens = append(p.opens, q.Resume)
			before, waitErr := p.before, p.waitErr
			p.mu.Unlock()
			if before != nil {
				before(ctx)
			}
			return &fallbackHTTPSession{send: func(q app.PromptRequest) (app.PendingPrompt, error) {
				var opening string
				if q.Handoff != nil {
					var err error
					opening, err = q.Handoff.Opening(q.Turn, q.Context)
					if err != nil {
						return nil, err
					}
				}
				p.mu.Lock()
				p.prompts = append(p.prompts, q.Text)
				p.opening = append(p.opening, opening)
				p.mu.Unlock()
				return fallbackHTTPPending{err: waitErr}, nil
			}}, nil
		},
	}, nil
}

func newHandoffSource(t *testing.T) (*paServerFixture, *handoffSourceProbe) {
	t.Helper()
	f := newPAServerFixture(t, true)
	probe := &handoffSourceProbe{}
	f.s.projectAssistants.provider = probe.provider
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paServerOK(t, f.send(t, f.local, "seed-history"))
	x := waitInputTurn(t, f)
	if len(x.Turns) != 1 || len(x.Messages) != 2 {
		t.Fatalf("synthetic source did not open a native conversation: %+v", x.Turns)
	}
	return f, probe
}

func handoffPath(f *paServerFixture) string {
	return "/v1/project-assistants/" + f.key.BotID + "/handoff-card"
}

func handoffReadPath(f *paServerFixture, conversation string) string {
	return handoffPath(f) + "?machine_id=" + f.key.MachineID + "&conversation_id=" + conversation
}

func readHandoffResponse(t *testing.T, raw []byte) contract.ProjectAssistantHandoffResponse {
	t.Helper()
	var answer contract.ProjectAssistantHandoffResponse
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Fence.ConversationID == "" {
		t.Fatalf("invalid handoff response: %s, %v", raw, err)
	}
	return answer
}

func handoffRead(t *testing.T, f *paServerFixture) contract.ProjectAssistantHandoffResponse {
	t.Helper()
	got := f.get(t, f.local)
	response := f.request(t, "GET", handoffReadPath(f, got.Conversation.ID), f.local, nil)
	paServerOK(t, response)
	return readHandoffResponse(t, response.Body.Bytes())
}

func handoffChange(t *testing.T, f *paServerFixture, q contract.ProjectAssistantHandoffRequest) contract.ProjectAssistantHandoffResponse {
	t.Helper()
	response := f.request(t, "POST", handoffPath(f), f.local, q)
	paServerOK(t, response)
	return readHandoffResponse(t, response.Body.Bytes())
}

func handoffPrepare(t *testing.T, f *paServerFixture, id string) contract.ProjectAssistantHandoffResponse {
	t.Helper()
	prior := handoffRead(t, f)
	if !prior.CanPrepare {
		t.Fatalf("source cannot prepare: %+v", prior)
	}
	got := handoffChange(t, f, contract.ProjectAssistantHandoffRequest{Command: f.command(pa.EditOperation, id), Fence: prior.Fence, Action: "prepare"})
	if got.Card == nil || got.Card.State != "draft" || got.Card.BookmarkMessageID == "" || !strings.Contains(got.Card.Text, "Synthetic first input") {
		t.Fatalf("preview is missing source history: %+v", got)
	}
	return got
}

func handoffSend(t *testing.T, f *paServerFixture, token string, send contract.ProjectAssistantSendRequest) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", token, send)
}

func TestHandoffCardSourceKeepEditAdoptAndReplay(t *testing.T) {
	f, probe := newHandoffSource(t)
	keep := handoffPrepare(t, f, "prepare-keep")
	kept := handoffChange(t, f, contract.ProjectAssistantHandoffRequest{Command: f.command(pa.EditOperation, "keep"), Fence: keep.Fence, Action: "keep", Ref: &keep.Card.Ref})
	if kept.Card.State != "kept" || kept.Card.Text != "" || kept.Card.BookmarkMessageID != keep.Card.BookmarkMessageID || len(probe.prompts) != 1 {
		t.Fatalf("Keep changed history or started an input: %+v", kept)
	}
	paServerOK(t, f.send(t, f.local, "continue-old"))
	waitInputTurn(t, f)
	if len(probe.opens) != 2 || !probe.opens[1] || probe.opening[1] != "" {
		t.Fatalf("Keep did not preserve the resumable existing session: opens=%v", probe.opens)
	}
	initial := handoffPrepare(t, f, "prepare-adopt")
	edit := contract.ProjectAssistantHandoffRequest{Command: f.command(pa.EditOperation, "edit-summary"), Fence: initial.Fence, Action: "edit", Ref: &initial.Card.Ref, Text: "Edited synthetic carry, with a quote: \"only history\""}
	edited := handoffChange(t, f, edit)
	if edited.Card.Ref.Revision != initial.Card.Ref.Revision+1 || len(probe.prompts) != 2 {
		t.Fatalf("edit caused a native prompt or did not advance version: %+v", edited)
	}
	replayedEdit := handoffChange(t, f, edit)
	if replayedEdit.Card.Ref != edited.Card.Ref {
		t.Fatal("exact edit replay changed the revision")
	}
	response := f.request(t, "GET", handoffReadPath(f, initial.Fence.ConversationID), f.readonly, nil)
	if response.Code != 403 || strings.Contains(response.Body.String(), edit.Text) {
		t.Fatalf("read-only device accessed handoff draft: %d %s", response.Code, response.Body)
	}
	send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "stale-message"), Fence: initial.Fence, Text: "New synthetic input", Handoff: &initial.Card.Ref}
	paAssertHTTPFailure(t, handoffSend(t, f, f.local, send), 409, pa.StaleVersion)
	send.Handoff = nil
	paAssertHTTPFailure(t, handoffSend(t, f, f.local, send), 409, pa.StaleVersion)
	if len(probe.prompts) != 2 {
		t.Fatal("stale or unobserved handoff sent a prompt")
	}
	send.Command = f.command(pa.SendOperation, "adopted-message")
	send.Handoff = &edited.Card.Ref
	first := handoffSend(t, f, f.local, send)
	paServerOK(t, first)
	source := waitInputTurn(t, f)
	// Simulate a client that lost the first HTTP answer and explicitly checks
	// its exact original command. This must return the original receipt.
	again := handoffSend(t, f, f.local, send)
	paServerOK(t, again)
	if paMatrixReceipt(t, first.Body.Bytes()) != paMatrixReceipt(t, again.Body.Bytes()) {
		t.Fatal("lost acknowledgement changed the admission receipt")
	}
	card := handoffRead(t, f).Card
	if len(probe.prompts) != 3 || probe.opens[2] || probe.prompts[2] != send.Text || card.State != "adopted" || card.Text != "" {
		t.Fatalf("new input opened incorrectly: opens=%v prompts=%d card=%+v", probe.opens, len(probe.prompts), card)
	}
	opening := probe.opening[2]
	jsonStart := strings.IndexByte(opening, '{')
	if jsonStart < 0 {
		t.Fatal("missing structured handoff")
	}
	var payload struct {
		EditedSummary string          `json:"edited_summary"`
		Continuation  json.RawMessage `json:"continuation"`
	}
	if err := json.Unmarshal([]byte(opening[jsonStart:]), &payload); err != nil || payload.EditedSummary != edit.Text || len(payload.Continuation) != 0 {
		t.Fatalf("edited handoff did not replace filtered excerpts: %+v %v", payload, err)
	}
	if len(source.Messages) != 6 || len(source.Turns) != 3 || source.Messages[0].Text != "Synthetic first input" || source.Messages[0].ID == "" || source.Messages[4].Text != send.Text {
		t.Fatalf("handoff polluted the original history or user's actual input: %+v", source.Messages)
	}
}

func TestHandoffCardSourceForgetAndRevocationBeforePrompt(t *testing.T) {
	for _, effect := range []string{"forget", "revoke-cloud"} {
		t.Run(effect, func(t *testing.T) {
			f, probe := newHandoffSource(t)
			card := handoffPrepare(t, f, "prepare-before-fence")
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			probe.mu.Lock()
			probe.before = func(ctx context.Context) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			probe.mu.Unlock()
			send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "fenced-input"), Fence: card.Fence, Text: "Next human input", Handoff: &card.Card.Ref}
			var matrix *paMatrix
			if effect == "revoke-cloud" {
				matrix = &paMatrix{paServerFixture: f, pins: map[string]adaptercloud.PinnedDevice{}, privs: map[string]ed25519.PrivateKey{},
					bridge: cloudops.Bridge{MachineID: f.key.MachineID, Router: f.cloudRouter(), AllowCommands: func() bool { return true }}}
				matrix.pair(t, "write-viewer", adaptercloud.ScopeOperate, false)
				answer, raw := matrix.word(t, "write-viewer", "handoff-card", map[string]string{"bot_id": f.key.BotID, "machine_id": f.key.MachineID, "conversation_id": card.Fence.ConversationID})
				if !answer.OK() || readHandoffResponse(t, raw).Card.Ref != card.Card.Ref {
					t.Fatalf("Cloud visitor cannot read exact draft: %+v %s", answer, raw)
				}
				update := contract.ProjectAssistantHandoffRequest{Command: f.command(pa.EditOperation, "cloud-edit"), Fence: card.Fence, Action: "edit", Ref: &card.Card.Ref, Text: "Cloud edited summary"}
				answer, raw = matrix.word(t, "write-viewer", "handoff-card-write", update)
				if !answer.OK() {
					t.Fatalf("Cloud edit refused: %+v %s", answer, raw)
				}
				updated := readHandoffResponse(t, raw)
				if updated.Card.Text != update.Text {
					t.Fatal("Cloud edit did not reach source")
				}
				send.Handoff = &updated.Card.Ref
				answer, raw = matrix.word(t, "write-viewer", "send", send)
				if !answer.OK() {
					t.Fatalf("Cloud adoption refused: %+v %s", answer, raw)
				}
			} else {
				paServerOK(t, handoffSend(t, f, f.local, send))
			}
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the synthetic input never entered Open")
			}
			if effect == "revoke-cloud" {
				if _, err := matrix.pinned().Revoke("write-viewer", time.Now()); err != nil {
					t.Fatal(err)
				}
			} else {
				req := contract.ProjectAssistantForgetRequest{Command: f.command(pa.ForgetOperation, "forget-bookmark"), Project: card.Fence.Project, ExpectedRevision: 1, SourceIds: []string{card.Card.BookmarkMessageID}}
				paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/memory/forget", f.local, req))
			}
			release <- struct{}{}
			x := fallbackHTTPWait(t, f)
			if len(probe.prompts) != 1 || len(x.Turns) != 2 || len(x.Messages) != 3 || x.Messages[0].Text != "Synthetic first input" {
				t.Fatalf("fenced summary reached native send or original history disappeared: prompts=%v messages=%+v", probe.prompts, x.Messages)
			}
			card = handoffRead(t, f)
			if card.Card == nil || card.Card.State != "withdrawn" || card.Card.Text != "" {
				t.Fatalf("withdrawal not retained: %+v", card)
			}
			if effect == "revoke-cloud" {
				response, raw := matrix.word(t, "write-viewer", "handoff-card", map[string]string{"bot_id": f.key.BotID, "machine_id": f.key.MachineID, "conversation_id": card.Fence.ConversationID})
				if response.OK() || strings.Contains(string(raw), "Cloud edited summary") {
					t.Fatalf("revoked viewer read the draft: %+v %s", response, raw)
				}
			}
		})
	}
}

func TestHandoffCardSourceRestartRetainsDraftAndNeverRepeatsAnUnknownInput(t *testing.T) {
	f, probe := newHandoffSource(t)
	draft := handoffPrepare(t, f, "persist-preview")
	restart := func() {
		t.Helper()
		if err := f.s.StopProjectAssistants(t.Context()); err != nil {
			t.Fatal(err)
		}
		s, err := New(f.s.cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.StopProjectAssistants(context.Background()); _ = s.store.Close() })
		s.projectAssistants.provider = probe.provider
		if err = s.StartProjectAssistants(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.s, f.h = s, s.Handler()
	}
	restart()
	after := handoffRead(t, f)
	if after.Card == nil || *after.Card != *draft.Card || len(probe.prompts) != 1 {
		t.Fatalf("restart lost the saved draft or sent it: %+v", after)
	}
	probe.mu.Lock()
	probe.waitErr = fmt.Errorf("scripted provider response lost")
	probe.mu.Unlock()
	send := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "response-lost"), Fence: after.Fence, Text: "Synthetic result may be unknown", Handoff: &after.Card.Ref}
	ack := handoffSend(t, f, f.local, send)
	paServerOK(t, ack)
	receipt := paMatrixReceipt(t, ack.Body.Bytes())
	fallbackHTTPWait(t, f)
	restart()
	source := fallbackHTTPWait(t, f)
	if len(probe.prompts) != 2 || len(source.Messages) != 3 {
		t.Fatalf("restart replayed an unknown input: prompts=%v, messages=%d", probe.prompts, len(source.Messages))
	}
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Turn(t.Context(), f.key, receipt.ResourceID)
	if err != nil || x.State != pa.TurnUnknown {
		t.Fatalf("unknown execution fact lost: %+v %v", x, err)
	}
	replay := handoffSend(t, f, f.local, send)
	paServerOK(t, replay)
	if got := paMatrixReceipt(t, replay.Body.Bytes()); got != receipt || len(probe.prompts) != 2 {
		t.Fatal("replay after restart changed the admission or sent again")
	}
}
