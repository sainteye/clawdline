package cloudops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
)

func videoReply() string {
	return `{"scope":"project","path":"clip.mp4","media_type":"video/mp4","total_bytes":3,"offset":0,"revision":"3-1","byte_count":3,"data":"YWJj"}`
}
func videoRequest() map[string]any {
	return map[string]any{"type": "video", "session": pane, "request": "video-1", "scope": "project", "task": "", "path": "clip.mp4", "offset": 0, "revision": "", "expected_generation": pinnedGeneration}
}
func TestVideoUsesPinnedReadAuthorityAndRechecksRevocation(t *testing.T) {
	r := &router{body: videoReply(), media: "application/json"}
	b := open(r)
	checks := 0
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		checks++
		return contentReadAuthority(true)
	}
	cmd := contentReadRequest(t, videoRequest())
	answer := b.Handle(context.Background(), cmd)
	if !answer.OK() || len(r.seen) != 1 || r.last().Query["offset"] != "0" || r.last().Header["X-Clawdline-Execution-Generation"] != pinnedGeneration || checks != 2 {
		t.Fatalf("answer=%+v routes=%+v checks=%d", answer, r.seen, checks)
	}
	checks = 0
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		checks++
		return contentReadAuthority(checks == 1)
	}
	if answer := b.Handle(context.Background(), cmd); answer.OK() {
		t.Fatal("revocation during read disclosed video")
	}
	checks = 0
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority {
		checks++
		return contentReadAuthority(false)
	}
	before := len(r.seen)
	if answer := b.Handle(context.Background(), cmd); answer.Code != "read_transcript_required" || len(r.seen) != before {
		t.Fatalf("revoked: %+v routes=%d", answer, len(r.seen))
	}
	for _, change := range []map[string]any{{"offset": 1}, {"offset": -1}, {"path": "../clip.mp4"}, {"path": "clip.html"}, {"extra": true}, {"scope": "task"}, {"expected_generation": "wrong"}} {
		body := videoRequest()
		for k, v := range change {
			body[k] = v
		}
		if answer := b.Handle(context.Background(), contentReadRequest(t, body)); answer.OK() {
			t.Fatal(change)
		}
	}
}
func TestVideoShapingRejectsMismatchedOrUnboundedChunks(t *testing.T) {
	var body map[string]any
	_ = json.Unmarshal([]byte(videoReply()), &body)
	p := plan{scope: "project", path: "clip.mp4"}
	if _, refusal := shapeVideo(p, LocalResponse{ContentType: "application/json", Body: []byte(videoReply())}); refusal.Code != "" {
		t.Fatal(refusal)
	}
	for _, change := range []map[string]any{{"path": "other.mp4"}, {"scope": "task"}, {"offset": 1}, {"byte_count": 4}, {"total_bytes": 1 << 30}, {"revision": ""}, {"media_type": "text/html"}, {"data": "YWJj\n"}, {"extra": true}} {
		changed := map[string]any{}
		for k, v := range body {
			changed[k] = v
		}
		for k, v := range change {
			changed[k] = v
		}
		raw, _ := json.Marshal(changed)
		if _, refusal := shapeVideo(p, LocalResponse{ContentType: "application/json", Body: raw}); refusal.Code != "video_answer_invalid" {
			t.Fatal(change, refusal)
		}
	}
}
