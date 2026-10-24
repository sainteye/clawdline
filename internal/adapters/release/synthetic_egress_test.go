package release_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
)

// Fetch uses the production updater client against an isolated receiver. This
// does not cover automatic scheduling, CDN redirects or artifact downloads.
func TestSyntheticEgressUpdate(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(release.Manifest{Version: "v1.2.3", Commit: strings.Repeat("a", 40), Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	signatures, err := json.Marshal([]release.Signature{release.Sign(manifest, priv)})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
		}
		if req.Method != http.MethodGet || len(body) != 0 || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
			t.Error("unexpected update request content or credentials")
		}
		switch req.URL.Path {
		case "/synthetic/manifest.json":
			_, _ = w.Write(manifest)
		case "/synthetic/manifest.sig.json":
			_, _ = w.Write(signatures)
		case "/BUILD.json":
			_, _ = w.Write([]byte(`{"stamp":"synthetic-build","committed_at":"2026-10-21T00:00:00Z"}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer receiver.Close()
	env := updater.Env{Keys: []ed25519.PublicKey{pub}, Client: receiver.Client()}
	got, err := env.Fetch(context.Background(), receiver.URL+"/synthetic")
	if err != nil || got.Version != "v1.2.3" {
		t.Fatalf("fetch: %+v %v", got, err)
	}
	if len(paths) != 2 || paths[0] != "/synthetic/manifest.json" || paths[1] != "/synthetic/manifest.sig.json" {
		t.Fatalf("unexpected paths: %v", paths)
	}
	stamp, err := updatecheck.Fetch(context.Background(), receiver.Client(), receiver.URL+"/BUILD.json")
	if err != nil || stamp.Stamp != "synthetic-build" || len(paths) != 3 || paths[2] != "/BUILD.json" {
		t.Fatalf("hosted build fetch: %+v %v paths=%v", stamp, err, paths)
	}
	t.Log("SYNTHETIC_EGRESS sample=update-manifest-and-build calls=3 method=GET body=empty query=empty credentials=absent signed_manifest=verified")
}
