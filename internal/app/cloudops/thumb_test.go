package cloudops

import (
	"context"
	"encoding/json"
	"testing"
)

// A Board card or a to-do row asks for its reference image with
// `size: "thumb"`, and that is carried to the route as its query and answered
// as the JPEG the route drew. A page built before it sends no `size` and
// still gets the whole picture; any other size is not a read this machine
// knows, and no route is asked.
func TestAReferenceImageReadCarriesItsThumbnailSize(t *testing.T) {
	read := func(extra map[string]any) (*router, Answer) {
		r := &router{body: "jpg", media: "image/jpeg"}
		body := map[string]any{"type": "work.v2.image", "session": MachineReplySession, "request": "req-1", "id": "img1"}
		for k, v := range extra {
			body[k] = v
		}
		plain, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		b := Bridge{MachineID: "mac-01", Router: r, AllowCommands: func() bool { return true }}
		return r, b.Handle(context.Background(), Command{Channel: "ctl/mac-01", Class: ClassCtl,
			Sender: "viewer-01", Sequence: 1, Plaintext: plain})
	}

	r, answer := read(map[string]any{"size": "thumb"})
	if len(r.seen) != 1 || r.seen[0].Path != "/v1/work/v2/images/img1" || r.seen[0].Query["size"] != "thumb" {
		t.Fatalf("the route was asked %+v", r.seen)
	}
	if !answer.OK() || answer.Name != "read:req-1" || answer.Session != MachineReplySession {
		t.Fatalf("answered %+v", answer)
	}
	var payload struct {
		Body struct {
			MediaType string `json:"media_type"`
		} `json:"body"`
	}
	if err := json.Unmarshal(answer.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Body.MediaType != "image/jpeg" {
		t.Fatalf("a thumbnail crossed as %s", answer.Payload)
	}

	r, answer = read(nil)
	if len(r.seen) != 1 || len(r.seen[0].Query) != 0 || !answer.OK() {
		t.Fatalf("an unsized read: %+v %+v", r.seen, answer)
	}

	for _, size := range []any{"full", "", 480, nil} {
		r, answer = read(map[string]any{"size": size})
		if len(r.seen) != 0 || answer.OK() {
			t.Fatalf("size %v asked the route %+v and answered %+v", size, r.seen, answer)
		}
	}
}
