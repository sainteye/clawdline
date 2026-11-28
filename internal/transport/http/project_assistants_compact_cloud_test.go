package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The phone's Cloud relay drops the empty `text` of a "Compact now" (and of a
// call greeting) because the generated decoder refuses an empty text. The
// daemon must still read that request as the compaction it is: on 2026-11-24
// every phone press answered pa_invalid on field "body".
func TestCompactNowThroughCloudReachesTheBot(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		return paServerProvider{open: func(context.Context, app.OpenRequest) (app.ProviderSession, error) {
			return &paServerSession{sent: make(chan struct{})}, nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	g := f.get(t, f.local)
	f.pin(t, "compact-phone", "operate", false)
	bridge := cloudops.Bridge{MachineID: f.identity.MachineID, AllowCommands: func() bool { return true }, Router: f.cloudRouter()}
	const id = "compact-c6000001-0000-4000-8000-000000000003"
	fence := contract.ProjectAssistantFence{Key: g.Bot.Key, Project: g.Bot.Project, ConversationID: g.Conversation.ID,
		BindingID: g.Conversation.BindingID, Generation: g.Conversation.Generation}
	// Exactly what web/console/src/cloud/project-assistants.ts relays: no text.
	raw, err := json.Marshal(map[string]any{"type": "project-assistants.send", "session": cloudops.MachineReplySession, "request": id,
		"command": f.command(pa.SendOperation, id), "fence": fence, "quote_message_id": "", "compact": true})
	if err != nil {
		t.Fatal(err)
	}
	answer := bridge.Handle(t.Context(), cloudops.Command{Channel: "ctl/" + f.key.MachineID, Class: cloudops.ClassCtl,
		Sender: "compact-phone", Sequence: 1, Plaintext: raw})
	// A Bot with no native session yet is refused by the compaction itself,
	// which proves the request reached it instead of failing to decode.
	if answer.Code == string(pa.Invalid) {
		t.Fatalf("Cloud compact was refused as malformed: status %d field %q payload %s", answer.Status, answer.Field, answer.Payload)
	}
	if answer.Code != string(pa.Unsupported) || answer.Field != "compact_no_session" {
		t.Fatalf("Cloud compact did not reach the Bot's compaction: status %d code %q field %q", answer.Status, answer.Code, answer.Field)
	}
	// The local route keeps accepting its explicit empty text, and still refuses
	// user text riding on a compaction.
	local := contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, "compact-local-1"), Fence: fence, Compact: true}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.lan, local), http.StatusNotImplemented, pa.Unsupported)
	local.Text = "hello"
	local.Command = f.command(pa.SendOperation, "compact-local-2")
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", f.lan, local), http.StatusBadRequest, pa.Invalid)
}
