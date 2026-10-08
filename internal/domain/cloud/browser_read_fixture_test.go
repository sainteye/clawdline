package cloud

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// This file is emitted by the hosted JavaScript client's real command sealer.
// Decoding these exact bytes catches a cross-language signing or AES mismatch
// that a Go Seal followed by a Go Open would not catch.
func TestBrowserPinnedReadEnvelopeOpensAndTamperingFails(t *testing.T) {
	raw, err := os.ReadFile("testdata/browser-read-content-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		TestOnly   bool            `json:"test_only"`
		PublicKey  string          `json:"public_key"`
		ContentKey string          `json:"content_key"`
		Envelope   json.RawMessage `json:"envelope"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || !fixture.TestOnly {
		t.Fatalf("test-only fixture: %v", err)
	}
	public, err := base64.StdEncoding.DecodeString(fixture.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	secretBytes, err := base64.StdEncoding.DecodeString(fixture.ContentKey)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ContentKeyFromBytes(secretBytes)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := DecodeEnvelope(fixture.Envelope)
	if err != nil {
		t.Fatalf("Go rejected the browser envelope: %v", err)
	}
	if envelope.Ch != "r/test_machine" || envelope.Class != ClassCtl || envelope.Sender != "test_viewer" ||
		envelope.Seq != 17 || envelope.Ts != 1791484575602 || envelope.KeyID != "test_master" {
		t.Fatalf("browser request identity changed: %+v", envelope)
	}
	keyFor := pinnedOnly("test_viewer", public)
	plaintext, err := envelope.Open(secret, keyFor)
	if err != nil {
		t.Fatalf("Go could not open the browser request: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(plaintext, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture.Body, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opened body=%v, want=%v", got, want)
	}

	for _, tc := range []struct {
		field string
		value any
	}{
		{"ch", "ctl/test_machine"}, {"class", "dispatch"}, {"seq", float64(18)},
		{"ts", float64(1791484575603)}, {"key_id", "other_master"},
		{"sender", "other_viewer"}, {"nonce", "A5Y4fhbjo1i+Rwyn"},
		{"ct", "ANnKkT5aD9VITjRuLav/KzrgvT4Ey6hGvJTwN947AbPmwJ+ZQKhNnHEd8ZVnUkTTmbNWcfnrsT9xOWWxJHCi4uO4suvgLStTL/yYP6DDVbzJvZCJqM9wXRCOOp+fBf9JFUyIEH0tl2E+qVO++o6i5mZrA0Rq/MKztesH+5OU8rmBr5rnzksX8567X5wG0mjOr3OmR7rGH8eynwy+"},
		{"sig", "A5PdYclHAC5gNHOAQ8SkDQF0XoI/4a/vVVwSj8w+kdvzfkZzgmz/WQYhzU/9EXmwvCZutmMs16cL+T31YAuGAA=="},
	} {
		t.Run(tc.field, func(t *testing.T) {
			var changed map[string]any
			if err := json.Unmarshal(fixture.Envelope, &changed); err != nil {
				t.Fatal(err)
			}
			changed[tc.field] = tc.value
			bytes, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			altered, err := DecodeEnvelope(bytes)
			if err == nil {
				_, err = altered.Open(secret, keyFor)
			}
			if err == nil {
				t.Fatalf("tampered %s was accepted", tc.field)
			}
		})
	}
}
