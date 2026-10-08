package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
)

func TestViewerLoginRefusesOldMachineOnlyAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"bad_machine","message":"name, platform and public_key are required"}}`))
	}))
	defer server.Close()
	keys, err := cloudkeys.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := cloud.NewViewerIdentityStore(keys.Dir())
	var out, failures bytes.Buffer
	if code := viewerLoginRun(context.Background(), &out, &failures, cloud.NewAccountClient(server.URL), keys, store, server.URL, "CLI", time.Second, nil); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(failures.String(), "viewer_flow_unavailable") || out.Len() != 0 {
		t.Fatalf("unsafe old API answer: out=%q err=%q", out.String(), failures.String())
	}
	if _, found, _ := store.Load(); found {
		t.Fatal("old API made a viewer identity")
	}
	if _, found, _ := keys.ViewerKey(); found {
		t.Fatal("old API saved an unused viewer key")
	}
}

func TestViewerLoginSavesSeparateKeyAndHidesCredential(t *testing.T) {
	const credential = "viewer-secret-never-print"
	var viewerPublicKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/device/start":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			viewerPublicKey, _ = body["public_key"].(string)
			if body["kind"] != "viewer" {
				t.Errorf("wrong principal: %v", body["kind"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "private-code", "user_code": "ABCD", "verification_uri_complete": "https://example.test/connect", "expires_in": 60, "interval": 1})
		case "/v1/auth/device/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "complete", "account_id": "account", "viewer_device_id": "viewer", "viewer_credential": credential})
		case "/v1/tokens/device":
			if r.Header.Get("Authorization") != "Bearer "+credential {
				t.Errorf("wrong credential in token request")
			}
			payload, _ := json.Marshal(map[string]any{"role": "viewer", "acct": "account", "dev": "viewer", "pk": viewerPublicKey})
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature", "expires_in": 300})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	keys, err := cloudkeys.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := cloud.NewViewerIdentityStore(keys.Dir())
	var out, failures bytes.Buffer
	code := viewerLoginRun(context.Background(), &out, &failures, cloud.NewAccountClient(server.URL), keys, store, server.URL, "CLI", time.Second, func(context.Context, time.Duration) error { return nil })
	if code != 0 || failures.Len() != 0 {
		t.Fatalf("exit %d, err=%q", code, failures.String())
	}
	if strings.Contains(out.String(), credential) || strings.Contains(out.String(), "private-code") || !strings.Contains(out.String(), "viewer") {
		t.Fatalf("unsafe output %q", out.String())
	}
	identity, found, err := store.Load()
	if err != nil || !found || identity.ViewerCredential != credential || identity.DeviceID != "viewer" {
		t.Fatalf("identity %+v found=%v err=%v", identity, found, err)
	}
	if _, found, err := keys.ViewerKey(); err != nil || !found {
		t.Fatalf("viewer key missing: %v", err)
	}
	if _, found, err := keys.DeviceKey(); err != nil || found {
		t.Fatalf("viewer login touched machine key: %v", err)
	}
}
