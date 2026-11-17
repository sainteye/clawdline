package http

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestGroupPreviewsCannotMakeReadableMetadataExceedCloudBudget(t *testing.T) {
	group := contract.ProjectAssistantGroup{Name: "Synthetic room"}
	out := contract.ProjectAssistantGroupListResponse{Groups: []contract.ProjectAssistantGroup{group, group}}
	preview := pa.GroupPreview{Room: &pa.RoomState{Revision: 2}, LastEntry: &pa.RoomEntry{Seq: 1, Kind: pa.RoomMemberEntry, Text: strings.Repeat("<", cloudops.ReadPlaintextBytesLimit)}}
	encoded, err := paPackGroupPreviews(out, []pa.GroupPreview{preview, {LastEntry: &pa.RoomEntry{Seq: 2, Kind: pa.RoomPersonEntry, Text: "A later fitting preview"}}}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal("an oversized optional preview made metadata unreadable", err)
	}
	output := encoded.(paEncodedOutput)
	size, err := paGroupReplyBytes(output.body)
	if err != nil || size > cloudops.ReadPlaintextBytesLimit {
		t.Fatal("packed previews escaped the actual Cloud reply envelope", size, err)
	}
	var got contract.ProjectAssistantGroupListResponse
	if err = json.Unmarshal(output.body, &got); err != nil || len(got.Groups) != 2 || got.Groups[0].LastEntry != nil || got.Groups[0].Room == nil || got.Groups[1].LastEntry == nil || got.Groups[1].LastEntry.Text != "A later fitting preview" {
		t.Fatalf("oversized preview suppressed metadata or another fitting preview: %+v %v", got, err)
	}
}

func TestGroupQuoteKeepsTheExactRoomMessageAndReplay(t *testing.T) {
	f := newGroupHTTPFixture(t)
	f.create(t)
	post := func(request map[string]any) (int, map[string]any) {
		t.Helper()
		status, raw := f.request(t, "POST", "/v1/project-assistants/rooms/groups/room/messages", "person", request)
		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return status, result
	}
	status, original := post(map[string]any{"command": f.command(pa.SendOperation, "quote-source"), "text": "Synthetic original room message"})
	if status != 200 {
		t.Fatal(status, original)
	}
	seq := original["entry"].(map[string]any)["seq"]
	request := map[string]any{"command": f.command(pa.SendOperation, "quote-reply"), "text": "Reply in this room", "quote": map[string]any{"seq": seq}}
	status, reply := post(request)
	if status != 200 {
		t.Fatalf("a reply to a specific room message was refused: %d %v", status, reply)
	}
	entry := reply["entry"].(map[string]any)
	quote, ok := entry["quote"].(map[string]any)
	if !ok || quote["seq"] != seq || quote["kind"] != "person" || quote["text"] != "Synthetic original room message" {
		t.Fatalf("the host did not derive the original quote: %v", entry)
	}
	status, replay := post(request)
	if status != 200 || !reflect.DeepEqual(replay["entry"], entry) {
		t.Fatalf("replay changed the stored message or quote: %d %v", status, replay)
	}
	request["quote"] = map[string]any{"seq": float64(999999)}
	if status, result := post(request); status < 400 {
		t.Fatalf("the same command key silently changed its quoted message: %v", result)
	}
	request["command"] = f.command(pa.SendOperation, "missing-quote")
	if status, result := post(request); status < 400 {
		t.Fatalf("a missing room message was accepted as a quote: %v", result)
	}
	status, raw := f.request(t, "GET", "/v1/project-assistants/rooms/groups/room?machine_id=machine&before_seq=0&page_size=100", "reader", nil)
	var history struct {
		Lines []map[string]any `json:"lines"`
	}
	if status != 200 || json.Unmarshal(raw, &history) != nil {
		t.Fatal(status, string(raw))
	}
	for _, line := range history.Lines {
		if line["seq"] == entry["seq"] {
			if !reflect.DeepEqual(line["quote"], quote) {
				t.Fatalf("history lost the persisted quote: %v", line)
			}
			return
		}
	}
	t.Fatal("the reply did not stay in the original room history")
}
