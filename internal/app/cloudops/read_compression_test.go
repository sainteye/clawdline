package cloudops

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestPinnedTranscriptCompressesOnlyAnExplicitNewView(t *testing.T) {
	var route LocalRequest
	b := open(localRouterFunc(func(_ context.Context, r LocalRequest) (LocalResponse, error) {
		route = r
		body, _ := json.Marshal(map[string]any{"entries": strings.Repeat("synthetic result\n", 2000)})
		return LocalResponse{Status: 200, ContentType: "application/json", Body: body}, nil
	}))
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	for _, view := range []string{"", "tools-v1", "tools-v1z", "tools-v2z", "delta-v1z:" + strings.Repeat("a", 64), "entry-v1z:0:" + strings.Repeat("a", 64), "tool-v1z:0:" + strings.Repeat("a", 64) + ":text"} {
		body := map[string]any{"type": "transcript", "session": pane, "limit": 200, "expected_generation": pinnedGeneration}
		if view != "" {
			body["view"] = view
		}
		answer := b.Handle(context.Background(), contentReadRequest(t, body))
		if !answer.OK() {
			t.Fatalf("%s: %+v", view, answer)
		}
		if route.Query["view"] != view || route.Header["X-Clawdline-Execution-Generation"] != pinnedGeneration {
			t.Fatalf("pin/view lost: %+v", route)
		}
		payload := answer.Payload
		zipped := bytes.HasPrefix(payload, []byte{'g', 'z', '1', 0})
		if zipped != (strings.Contains(view, "v1z") || strings.Contains(view, "v2z")) {
			t.Fatalf("view=%s compressed=%v", view, zipped)
		}
		if zipped {
			r, err := gzip.NewReader(bytes.NewReader(payload[4:]))
			if err != nil {
				t.Fatal(err)
			}
			payload, err = io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err != nil {
			t.Fatal(err)
		}
		if data["seq"] != float64(411) || data["expected_generation"] != pinnedGeneration || data["session_id"] != pane {
			t.Fatal("compression lost request proof")
		}
	}
}
