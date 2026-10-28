package clauderemote

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func controlFixture(t *testing.T, pending []any) (*Client, session.Session, *[]map[string]any) {
	t.Helper()
	events := []map[string]any{}
	requestID := ""
	c, row := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var body struct {
				Events []struct{ Payload map[string]any }
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Events) != 1 {
				t.Error("wrong event count")
				return
			}
			p := body.Events[0].Payload
			events = append(events, p)
			if p["type"] == "control_request" {
				requestID, _ = p["request_id"].(string)
			}
			accept(w, p["uuid"].(string))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/events/stream") || r.URL.Query().Get("from_sequence_num") != "9" {
			t.Error("control stream did not resume after its own acknowledgement")
		}
		ev := map[string]any{"source": "worker", "payload": map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": requestID, "response": map[string]any{}, "pending_permission_requests": pending}}}
		raw, _ := json.Marshal(ev)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: "+string(raw)+"\n\n")
	})
	return c, row, &events
}
func TestNativeControlRequiresTheMatchingWorkerResponse(t *testing.T) {
	c, row, events := controlFixture(t, []any{})
	if err := c.InterruptRemote(context.Background(), row, nil); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 1 || (*events)[0]["request"].(map[string]any)["subtype"] != "interrupt" {
		t.Fatal("not an interrupt control")
	}
	c, row = fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			var body struct {
				Events []struct{ Payload map[string]any }
			}
			json.NewDecoder(r.Body).Decode(&body)
			accept(w, body.Events[0].Payload["uuid"].(string))
			return
		}
		io.WriteString(w, `data: {"source":"client","payload":{"type":"control_response","response":{"subtype":"success","request_id":"wrong"}}}`+"\n\n")
	})
	if err := c.InterruptRemote(context.Background(), row, nil); err == nil {
		t.Fatal("unrelated controller response confirmed interrupt")
	}
}
func TestPermissionPinsRequestAndFullInput(t *testing.T) {
	pending := []any{map[string]any{"request_id": "permission-1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "printf fixture"}, "tool_use_id": "tool-1"}}}
	c, row, events := controlFixture(t, pending)
	menu, err := c.InspectRemote(context.Background(), row)
	if err != nil || menu == nil {
		t.Fatal(err)
	}
	expect := session.MenuFingerprint(*menu)
	if err := c.SelectRemote(context.Background(), row, 1, expect, nil); err != nil {
		t.Fatal(err)
	}
	if len(*events) != 3 {
		t.Fatalf("events=%d", len(*events))
	}
	response := (*events)[2]["response"].(map[string]any)
	if response["request_id"] != "permission-1" || response["tool_use_id"] != "tool-1" || response["response"].(map[string]any)["behavior"] != "allow" {
		t.Fatal("permission response lost native identity")
	}
	// Same visible command under another request must never inherit the first consent.
	pending[0].(map[string]any)["request_id"] = "permission-2"
	before := len(*events)
	err = c.SelectRemote(context.Background(), row, 1, expect, nil)
	var unsent terminal.Unsent
	if !errors.As(err, &unsent) || len(*events) != before+1 {
		t.Fatalf("stale consent wrote an answer: %v", err)
	}
}
func TestQuestionCannotReceiveAnEmptyAllow(t *testing.T) {
	c, row, events := controlFixture(t, []any{map[string]any{"request_id": "question-1", "tool_name": "AskUserQuestion", "input": map[string]any{"questions": []any{}}}})
	menu, err := c.InspectRemote(context.Background(), row)
	if err != nil || menu == nil || len(menu.Options) != 1 || menu.Options[0].Number != 2 {
		t.Fatal("structured question offered an empty allow")
	}
	before := len(*events)
	err = c.SelectRemote(context.Background(), row, 1, session.MenuFingerprint(*menu), nil)
	var unsent terminal.Unsent
	if !errors.As(err, &unsent) || len(*events) != before+1 {
		t.Fatal("structured question was approved without answers")
	}
}
func TestImagesUseNativeBlocksWithoutPasteboard(t *testing.T) {
	var imageBytes bytes.Buffer
	png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes.Bytes())
	calls := 0
	c, row := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Events []struct {
				Payload struct {
					UUID    string
					Message struct{ Content []map[string]any }
				}
			}
		}
		json.NewDecoder(r.Body).Decode(&body)
		blocks := body.Events[0].Payload.Message.Content
		if len(blocks) != 2 || blocks[1]["type"] != "image" || blocks[1]["source"].(map[string]any)["media_type"] != "image/png" {
			t.Error("image was not sent as a native image block")
		}
		accept(w, body.Events[0].Payload.UUID)
	})
	if err := c.SendRemoteImages(context.Background(), row, "fixture caption", []string{data}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SendRemoteImages(context.Background(), row, "", []string{"data:application/pdf;base64,AA=="}, nil); err == nil || calls != 1 {
		t.Fatal("non-image was silently sent or dropped")
	}
}
func TestCloseArchivesOnlySelectedBridgeAndRequiresExit(t *testing.T) {
	calls := 0
	c, row := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/code/sessions/cse_fixture/archive" || r.Method != "POST" {
			t.Error("wrong archive target")
		}
		io.WriteString(w, `{}`)
	})
	c.gone = func(context.Context, session.Session) (bool, error) { return true, nil }
	if err := c.CloseRemote(context.Background(), row, nil); err != nil {
		t.Fatal(err)
	}
	c.gone = func(context.Context, session.Session) (bool, error) { return false, errors.New("unreadable") }
	if err := c.CloseRemote(context.Background(), row, nil); err == nil {
		t.Fatal("archive acknowledgement was mistaken for worker exit")
	}
	if calls != 2 {
		t.Fatal("close retried unexpectedly")
	}
}
