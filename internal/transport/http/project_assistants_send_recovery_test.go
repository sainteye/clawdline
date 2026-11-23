package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func TestProjectAssistantSendRecoveryHasOneWinner(t *testing.T) {
	for _, order := range []string{"original first", "editor first", "concurrent", "editor through Cloud"} {
		t.Run(order, func(t *testing.T) {
			f := newPAServerFixture(t, true)
			f.s.projectAssistants.provider = func() (app.Provider, error) {
				return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
					return &paServerSession{sent: make(chan struct{})}, nil
				}}, nil
			}
			f.enable(t)
			paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
			g := f.get(t, f.local)
			const id = "c6000001-0000-4000-8000-000000000001"
			const editedID = "c6000001-0000-4000-8000-000000000002"
			original := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, id), Text: "Original",
				Fence: contract.ProjectAssistantFence{Key: g.Bot.Key, Project: g.Bot.Project, ConversationID: g.Conversation.ID,
					BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation}}
			edited := original
			edited.Command.ClientAttemptID, edited.Text = editedID, "Edited"
			base := "/v1/project-assistants/" + f.key.BotID
			lookup := func(conversation string) string {
				return base + "/send-receipt?" + url.Values{"machine_id": {f.key.MachineID}, "conversation_id": {conversation}, "idempotency_key": {id}}.Encode()
			}
			paAssertHTTPFailure(t, f.request(t, "GET", lookup(g.Conversation.ID), f.readonly, nil), http.StatusNotFound, pa.NotFound)
			bridge := cloudops.Bridge{MachineID: f.identity.MachineID, AllowCommands: func() bool { return true }, Router: cloudtransport.Router{
				Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine),
			}}
			if order == "editor through Cloud" {
				f.pin(t, "send-recovery-owner", "operate", false)
			}
			var sequence uint64
			transmit := func(i int, override string) int {
				request := []contract.ProjectAssistantSendRequest{original, edited}[i]
				if order != "editor through Cloud" {
					return f.request(t, "POST", base+"/messages", f.lan, request).Code
				}
				correlation := request.Command.IdempotencyKey
				if request.Command.ClientAttemptID != "" {
					correlation = request.Command.ClientAttemptID
				}
				if override != "" {
					correlation = override
				}
				raw, err := json.Marshal(struct {
					contract.ProjectAssistantSendRequest
					Type    string `json:"type"`
					Session string `json:"session"`
					Request string `json:"request"`
				}{request, "project-assistants.send", cloudops.MachineReplySession, correlation})
				if err != nil {
					t.Fatal(err)
				}
				sequence++
				result := bridge.Handle(t.Context(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl,
					Sender: "send-recovery-owner", Sequence: sequence, Plaintext: raw})
				return result.Status
			}
			if order == "editor through Cloud" {
				for i := range 2 {
					if status := transmit(i, "unrelated-request"); status != http.StatusBadRequest {
						t.Fatalf("Cloud accepted a mismatched request identity: HTTP %d", status)
					}
				}
			}
			send := func(i int) int { return transmit(i, "") }
			var statuses [2]int
			switch order {
			case "original first":
				statuses[0], statuses[1] = send(0), send(1)
			case "editor first", "editor through Cloud":
				statuses[1], statuses[0] = send(1), send(0)
			case "concurrent":
				var parallel sync.WaitGroup
				for i := range statuses {
					parallel.Go(func() { statuses[i] = send(i) })
				}
				parallel.Wait()
			}
			winner := 0
			if statuses[1] == http.StatusOK {
				winner = 1
			}
			if statuses[winner] != http.StatusOK || statuses[1-winner] != http.StatusConflict {
				t.Fatalf("expected one admission and one conflict, got %v", statuses)
			}
			w := f.request(t, "GET", lookup(g.Conversation.ID), f.readonly, nil)
			paServerOK(t, w)
			var answer contract.ProjectAssistantReceiptResponse
			if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
				t.Fatal(err)
			}
			want := []contract.ProjectAssistantSendRequest{original, edited}[winner]
			if answer.Receipt.Command.ClientAttemptID != want.Command.ClientAttemptID || answer.Receipt.Command.IdempotencyKey != id ||
				answer.Receipt.State != contract.ProjectAssistantEffectStateAccepted || answer.Receipt.ResourceID == "" {
				t.Fatalf("lookup did not identify the winning attempt: %+v", answer.Receipt)
			}
			if replayed := send(winner); replayed != http.StatusOK {
				t.Fatalf("the winner could not safely retry: HTTP %d", replayed)
			}
			paAssertHTTPFailure(t, f.request(t, "GET", lookup("foreign-conversation"), f.readonly, nil), http.StatusNotFound, pa.NotFound)
			x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(t.Context(), f.key)
			if err != nil || len(x.Turns) != 1 || len(x.Messages) != 1 || x.Messages[0].Text != want.Text {
				t.Fatalf("winner or persisted effects changed, turns=%d messages=%+v err=%v", len(x.Turns), x.Messages, err)
			}
			f.pin(t, "send-receipt-reader", "readonly", false)
			raw, _ := json.Marshal(map[string]string{"type": "project-assistants.send-receipt", "session": cloudops.MachineReplySession,
				"request": "lookup-send", "machine_id": f.key.MachineID, "bot_id": f.key.BotID, "conversation_id": g.Conversation.ID, "idempotency_key": id})
			result := bridge.Handle(t.Context(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl,
				Sender: "send-receipt-reader", Sequence: 1, Plaintext: raw})
			var envelope struct {
				Body contract.ProjectAssistantReceiptResponse
			}
			if err = json.Unmarshal(result.Payload, &envelope); !result.OK() || err != nil || envelope.Body.Receipt != answer.Receipt {
				t.Fatalf("Cloud receipt changed: %s (%v)", result.Payload, err)
			}
		})
	}
}
