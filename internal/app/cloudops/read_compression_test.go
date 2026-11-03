package cloudops

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"encoding/json"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
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

func decodedCompressedAnswer(t *testing.T, answer Answer) map[string]any {
	t.Helper()
	raw := answer.Payload
	if bytes.HasPrefix(raw, []byte{'g', 'z', '1', 0}) {
		reader, err := gzip.NewReader(bytes.NewReader(raw[4:]))
		if err != nil {
			t.Fatal(err)
		}
		raw, err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestGenericReadCompressionPreservesRoutesAndLegacyReplies(t *testing.T) {
	var routes []LocalRequest
	b := open(localRouterFunc(func(_ context.Context, request LocalRequest) (LocalResponse, error) {
		routes = append(routes, request)
		return LocalResponse{Status: 200, Body: []byte(`{"rows":"` + strings.Repeat("fixture", 1000) + `"}`)}, nil
	}))
	for _, word := range []string{"personas", "work.v2.cards", "projects", "schedules", "provider-quotas"} {
		fields := map[string]any{"type": word, "session": MachineReplySession, "request": "req-encoded"}
		if word == "work.v2.cards" {
			fields["project"] = ""
			fields["status"] = "open"
			fields["query"] = ""
		}
		request := request(t, ClassCtl, fields)
		legacy := b.Handle(context.Background(), request)
		var raw map[string]any
		json.Unmarshal(request.Plaintext, &raw)
		raw["accept_encoding"] = "gzip-v1"
		request.Plaintext, _ = json.Marshal(raw)
		encoded := b.Handle(context.Background(), request)
		if !legacy.OK() || !encoded.OK() || !bytes.HasPrefix(encoded.Payload, []byte{'g', 'z', '1', 0}) || encoded.Name != legacy.Name+readCompressionSuffix {
			t.Fatalf("%s did not negotiate: %s", word, encoded.Code)
		}
		oldBody, newBody := decodedCompressedAnswer(t, legacy), decodedCompressedAnswer(t, encoded)
		if newBody["read"] != encoded.Name {
			t.Fatal("reply name differs from payload")
		}
		newBody["read"] = oldBody["read"]
		oldJSON, _ := json.Marshal(oldBody)
		newJSON, _ := json.Marshal(newBody)
		if !bytes.Equal(oldJSON, newJSON) {
			t.Fatal("decoded content changed")
		}
		if len(encoded.Payload) >= len(legacy.Payload) {
			t.Fatal("compressed reply grew")
		}
		if routes[len(routes)-1].Path != routes[len(routes)-2].Path {
			t.Fatal("negotiation changed route")
		}
	}
}

func TestGenericReadCompressionNeverWidensAnOperation(t *testing.T) {
	b := open(localRouterFunc(func(context.Context, LocalRequest) (LocalResponse, error) {
		t.Fatal("invalid representation reached a route")
		return LocalResponse{}, nil
	}))
	for _, raw := range []map[string]any{
		{"type": "personas", "session": MachineReplySession, "request": "req", "accept_encoding": "gzip-v2"},
		{"type": "personas", "session": MachineReplySession, "request": "req", "accept_encoding": true},
		{"type": "personas", "session": MachineReplySession, "request": "req", "accept_encoding": "gzip-v1", "unknown": true},
		{"type": "send", "session": pane, "text": "fixture", "request": "req", "accept_encoding": "gzip-v1"},
		{"type": "image", "session": pane, "id": "picture", "accept_encoding": "gzip-v1"},
	} {
		answer := b.Handle(context.Background(), request(t, ClassCtl, raw))
		if answer.OK() || !strings.HasPrefix(answer.Code, "malformed_") {
			t.Fatalf("invalid format accepted: %s", answer.Code)
		}
	}
	plain := []byte(`{"read":"small","status":200}`)
	if !bytes.Equal(gzipReadPayload(plain), plain) {
		t.Fatal("incompressible response changed")
	}
	over := make([]byte, ReadPlaintextBytesLimit+1)
	if !bytes.Equal(gzipReadPayload(over), over) {
		t.Fatal("oversized input compressed")
	}
}

func TestNegotiatedPinnedReadsKeepProofAndOverloadRefusals(t *testing.T) {
	b := open(localRouterFunc(func(_ context.Context, request LocalRequest) (LocalResponse, error) {
		if request.Header["X-Clawdline-Execution-Generation"] != pinnedGeneration {
			t.Fatal("execution pin lost")
		}
		return LocalResponse{Status: 200, Body: []byte(`{"skills":[]}`)}, nil
	}))
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(true) }
	request := contentReadRequest(t, map[string]any{"type": "skills", "session": pane, "expected_generation": pinnedGeneration, "accept_encoding": "gzip-v1"})
	for name, answer := range map[string]Answer{"success": b.Handle(context.Background(), request), "ingress": b.Busy(request, 1), "egress": b.Undeliverable(request, 1)} {
		body := decodedCompressedAnswer(t, answer)
		if answer.Name != "skills"+readCompressionSuffix || body["read"] != answer.Name || body["seq"] != float64(request.Sequence) || body["expected_generation"] != pinnedGeneration || body["session_id"] != pane {
			t.Fatalf("%s lost its exact reply proof", name)
		}
		if name != "success" && answer.Code != "cloud_ingress_busy" && answer.Code != "cloud_read_busy" {
			t.Fatalf("%s lost overload code", name)
		}
	}
	b.TranscriptAuthority = func(context.Context, string, ed25519.PublicKey) Authority { return contentReadAuthority(false) }
	denied := b.Handle(context.Background(), request)
	if denied.Code != "read_transcript_required" || denied.Name != "skills"+readCompressionSuffix {
		t.Fatal("negotiation bypassed authorization or lost refusal")
	}
}

// Replays only explicitly exported response bodies; no private fixture is committed.
func TestReplayStructuredReadCompression(t *testing.T) {
	directory := os.Getenv("CLAWDLINE_STRUCTURED_READ_REPLAY_DIR")
	if directory == "" {
		t.Skip("explicit private export required")
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var seq uint64
	sealedSize := func(payload []byte) int {
		t.Helper()
		seq++
		e, err := domaincloud.Seal(payload, domaincloud.SealParams{Ch: "t/fixture-machine/fixture-session", Seq: seq, Ts: 1, Class: domaincloud.ClassStream, KeyID: "ms-1", Sender: "fixture-signer", Key: key, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := e.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return len(raw)
	}
	samples := []struct{ file, word string }{{"cards.json", "work.v2.cards"}, {"personas.json", "personas"}, {"projects.json", "projects"}, {"schedules.json", "schedules"}, {"quotas.json", "provider-quotas"}}
	for _, name := range []string{"persona-display.json", "persona-unchanged.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
			samples = append(samples, struct{ file, word string }{name, "personas"})
		}
	}
	for _, sample := range samples {
		raw, err := os.ReadFile(filepath.Join(directory, sample.file))
		if err != nil {
			t.Fatal(err)
		}
		b := open(localRouterFunc(func(context.Context, LocalRequest) (LocalResponse, error) {
			return LocalResponse{Status: 200, Body: raw}, nil
		}))
		fields := map[string]any{"type": sample.word, "session": MachineReplySession, "request": "fixed-measure"}
		if sample.word == "work.v2.cards" {
			fields["project"] = ""
			fields["query"] = ""
			fields["status"] = "open"
		}
		if strings.HasPrefix(sample.file, "persona-") {
			fields["view"] = "display-v1"
		}
		plain := b.Handle(context.Background(), request(t, ClassCtl, fields))
		fields["accept_encoding"] = "gzip-v1"
		encoded := b.Handle(context.Background(), request(t, ClassCtl, fields))
		if !plain.OK() || !encoded.OK() {
			t.Fatal("sample did not traverse its normal read route")
		}
		left, right := decodedCompressedAnswer(t, plain), decodedCompressedAnswer(t, encoded)
		right["read"] = left["read"]
		a, _ := json.Marshal(left)
		z, _ := json.Marshal(right)
		if !bytes.Equal(a, z) {
			t.Fatal("read content changed")
		}
		fmt.Printf("sample=%s kind=%s http_json=%d legacy_payload=%d negotiated_payload=%d legacy_sealed=%d negotiated_sealed=%d\n", sample.file, sample.word, len(raw), len(plain.Payload), len(encoded.Payload), sealedSize(plain.Payload), sealedSize(encoded.Payload))
	}
}
