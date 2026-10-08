package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestViewerLoginRejectsMachineCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/device/start":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["kind"] != "viewer" || body["public_key"] == "" {
				t.Errorf("start did not identify a viewer: %v", body)
			}
			_ = json.NewEncoder(w).Encode(LoginStart{DeviceCode: "secret-code", UserCode: "ABCD", VerificationURIComplete: "https://example.test/connect", ExpiresIn: 60, Interval: 1})
		case "/v1/auth/device/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "complete", "account_id": "account", "machine_id": "machine", "machine_credential": "machine-secret"})
		}
	}))
	defer server.Close()
	client := NewAccountClient(server.URL)
	start, err := client.StartViewerLogin(context.Background(), "CLI", make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.WaitForViewerApproval(context.Background(), start, time.Second, func(context.Context, time.Duration) error { return nil })
	if err == nil {
		t.Fatal("machine credential was accepted as a viewer credential")
	}
}

func TestViewerIdentityStoreSeparatesCredential(t *testing.T) {
	store := NewViewerIdentityStore(t.TempDir())
	v := ViewerIdentity{AccountID: "account", DeviceID: "viewer", ViewerCredential: "secret", APIBase: "https://api.example.test"}
	if err := store.Save(v); err != nil {
		t.Fatal(err)
	}
	got, found, err := store.Load()
	if err != nil || !found || got != v {
		t.Fatalf("viewer identity: %+v %v %v", got, found, err)
	}
}

func TestViewerTokenMatchesRoleIdentityAndKey(t *testing.T) {
	key := make([]byte, 32)
	claims := map[string]any{"role": "viewer", "acct": "account", "dev": "viewer", "pk": base64.StdEncoding.EncodeToString(key)}
	encode := func() string {
		payload, _ := json.Marshal(claims)
		return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	}
	if !ViewerTokenMatches(encode(), "account", "viewer", key) {
		t.Fatal("matching viewer token refused")
	}
	claims["role"] = "machine"
	if ViewerTokenMatches(encode(), "account", "viewer", key) {
		t.Fatal("machine token accepted")
	}
	claims["role"] = "viewer"
	claims["mid"] = "machine"
	if ViewerTokenMatches(encode(), "account", "viewer", key) {
		t.Fatal("machine-bound token accepted")
	}
	delete(claims, "mid")
	if ViewerTokenMatches(encode(), "different", "viewer", key) || ViewerTokenMatches(encode(), "account", "other", key) {
		t.Fatal("wrong principal accepted")
	}
	key[0] = 1
	if ViewerTokenMatches(encode(), "account", "viewer", key) {
		t.Fatal("wrong key accepted")
	}
}
