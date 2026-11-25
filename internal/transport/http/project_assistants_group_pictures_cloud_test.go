package http

import (
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A phone sends a picture-only room line through the Cloud relay, which cannot
// carry an empty text: the relay leaves the field out, as it does for Compact
// now. The generated decoder and the daemon's room send must still read it as
// the picture line it is, and a line with neither text nor pictures stays
// refused.
func TestGroupPictureWithoutTextThroughCloudReachesTheRoom(t *testing.T) {
	f := newGroupHTTPFixture(t)
	f.create(t)
	bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
	const picture = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAAAFklEQVR4nGMwaEkhCTGMahjVMHw1AACoJBgQ9FGyagAAAABJRU5ErkJggg=="
	send := func(id string, fields map[string]any) cloudops.Answer {
		t.Helper()
		body := map[string]any{"type": "project-assistants.group-send", "session": cloudops.MachineReplySession, "request": id, "command": f.command(pa.SendOperation, id)}
		for k, v := range fields {
			body[k] = v
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return bridge.Handle(t.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "group-picture-phone", Sequence: 1, Plaintext: raw})
	}
	// Exactly what web/console/src/cloud/project-assistants.ts relays for a
	// picture with no words: no text field.
	answer := send("picture-only-1", map[string]any{"images": []string{picture}})
	if !answer.OK() {
		t.Fatalf("Cloud picture-only room line refused: status %d code %q payload %s", answer.Status, answer.Code, answer.Payload)
	}
	status, body := f.request(t, "GET", "/v1/project-assistants/rooms/groups/room?machine_id=machine&before_seq=0&page_size=100", "reader", nil)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	var page contract.ProjectAssistantGroupHistoryResponse
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) == 0 || page.Lines[0].Kind != "person" || page.Lines[0].Text != "" || len(page.Lines[0].Attachments) != 1 {
		t.Fatalf("room did not store the picture-only person line: %s", body)
	}
	// Without pictures the missing text is still malformed, over Cloud and
	// locally; the local route keeps accepting its explicit empty text.
	if answer := send("nothing-1", nil); answer.OK() {
		t.Fatal("a Cloud room line with neither text nor pictures was accepted")
	}
	status, body = f.request(t, "POST", "/v1/project-assistants/rooms/groups/room/messages", "person", map[string]any{"command": f.command(pa.SendOperation, "nothing-local")})
	if status != 400 {
		t.Fatalf("local room line without text or pictures: %d %s", status, body)
	}
	local := contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, "picture-local"), Images: []string{picture}}
	if status, body = f.request(t, "POST", "/v1/project-assistants/rooms/groups/room/messages", "person", local); status != 200 {
		t.Fatalf("local picture-only room line: %d %s", status, body)
	}
}
