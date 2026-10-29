package cloudops

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func TestOutputReadsUsePinnedContentChannel(t *testing.T) {
	var route LocalRequest
	b := open(localRouterFunc(func(_ context.Context, request LocalRequest) (LocalResponse, error) {
		route = request
		return LocalResponse{Status: 200, ContentType: "application/json", Body: []byte(`{"files":[],"more":false,"partial":false,"coverage":"published_only"}`)}, nil
	}))
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	for _, word := range []string{"outputs", "output-file"} {
		body := map[string]any{"type": word, "session": pane, "expected_generation": pinnedGeneration}
		if word == "output-file" {
			body["id"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			body["offset"] = "0"
			body["revision"] = ""
			body["size"] = ""
			body["request"] = "output-read"
		}
		cmd := contentReadRequest(t, body)
		answer := b.Handle(context.Background(), cmd)
		if answer.Code != "" {
			t.Fatalf("%s: %+v", word, answer)
		}
		if route.Method != "GET" || route.Header["X-Clawdline-Execution-Generation"] != pinnedGeneration {
			t.Fatalf("unbound route: %+v", route)
		}
		delete(body, "expected_generation")
		if denied := b.Handle(context.Background(), contentReadRequest(t, body)); denied.Code != "malformed_read" && denied.Code != "execution_target_required" {
			t.Fatalf("unpinned %s: %+v", word, denied)
		}
	}
}
