package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// This observes the production sealing/spool boundary, not a running relay,
// provider reader, browser, authorization gate or direct carrier.
func TestSyntheticEgressCloud(t *testing.T) {
	r, spool := newRecordingRelay(t, adaptercloud.DefaultSpoolLimits())
	connectionKey, err := domaincloud.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name, channel, body string
		terminal            bool
	}{
		{"session-list", "s/mac-01/synthetic", `{"title":"CANARY_SESSION_TITLE","project":"CANARY_PROJECT"}`, false},
		{"conversation", "t/mac-01/synthetic", `{"text":"CANARY_CONVERSATION"}`, false},
		{"history-page", "t/mac-01/synthetic", `{"messages":["CANARY_HISTORY_PAGE"]}`, false},
		{"document", "t/mac-01/__clawdline_machine__", `{"name":"CANARY_FILENAME.md","body":"CANARY_DOCUMENT"}`, false},
		{"image-original", "t/mac-01/__clawdline_machine__", `{"data":"CANARY_IMAGE_ORIGINAL","metadata":"CANARY_IMAGE_METADATA"}`, false},
		{"image-thumbnail", "t/mac-01/__clawdline_machine__", `{"data":"CANARY_IMAGE_THUMBNAIL"}`, false},
		{"terminal", "term/mac-01/synthetic/synthetic", `{"screen":"CANARY_TERMINAL"}`, true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			plain := []byte(sample.body)
			if !bytes.Contains(plain, []byte("CANARY_")) {
				t.Fatal("empty canary fixture")
			}
			out := Outbound{Channel: sample.channel, Payload: plain}
			key := r.Secret
			if sample.terminal {
				out.Key, out.KeyID, key = connectionKey, "rk-synthetic", connectionKey
			}
			seq, err := r.PublishTracked(context.Background(), out)
			if err != nil {
				t.Fatal(err)
			}
			row, ok := spool.Row(seq)
			if !ok || len(row.Sealed) == 0 {
				t.Fatal("no sealed spool row")
			}
			// A controlled receiver sees the application bytes inside HTTP, with
			// no TLS obscuring a plaintext mistake. Transport draining is not run.
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				leaked := syntheticCloudPlaintext(body, plain)
				if req.URL.Path == "/negative" {
					if !leaked {
						t.Error("detector missed controlled plaintext leak")
					}
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				if leaked {
					t.Error("receiver got plaintext")
				}
				env, err := domaincloud.DecodeEnvelope(body)
				if err != nil {
					t.Error(err)
					return
				}
				opened, err := env.Open(key, func(id string) (ed25519.PublicKey, bool) { return r.Signer.PublicKey(), id == r.MachineID })
				if err != nil || !bytes.Equal(opened, plain) {
					t.Error("authorized viewer did not recover exact fixture", err)
				}
				wrong, err := domaincloud.NewContentKey(nil)
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := env.Open(wrong, func(string) (ed25519.PublicKey, bool) { return r.Signer.PublicKey(), true }); err == nil {
					t.Error("wrong content key decrypted")
				}
				if env.Ch != sample.channel || env.Sender != r.MachineID {
					t.Error("routing metadata changed")
				}
			}))
			defer receiver.Close()
			for _, arm := range []struct {
				path   string
				body   []byte
				status int
			}{
				{"/sealed", row.Sealed, http.StatusOK},
				{"/negative", plain, http.StatusUnprocessableEntity},
				{"/negative", []byte(base64.StdEncoding.EncodeToString(plain)), http.StatusUnprocessableEntity},
			} {
				resp, err := receiver.Client().Post(receiver.URL+arm.path, "application/json", bytes.NewReader(arm.body))
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != arm.status {
					t.Fatalf("arm %s: status %d", arm.path, resp.StatusCode)
				}
			}
			t.Logf("SYNTHETIC_EGRESS sample=%s fixture_sha256=%x plaintext=%d sealed=%d negative_controls=2 authorized_open=pass wrong_key=refused", sample.name, sha256.Sum256(plain), len(plain), len(row.Sealed))
		})
	}
}

func syntheticCloudPlaintext(body, plain []byte) bool {
	var unescaped any
	if json.Unmarshal(body, &unescaped) == nil {
		if normalized, err := json.Marshal(unescaped); err == nil && bytes.Contains(normalized, []byte("CANARY_")) {
			return true
		}
	}
	return bytes.Contains(body, []byte("CANARY_")) || bytes.Contains(body, []byte(base64.StdEncoding.EncodeToString(plain))) || bytes.Contains(body, []byte(base64.RawURLEncoding.EncodeToString(plain)))
}
