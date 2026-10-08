package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestViewerPairPinsOnlyAuthenticatedHandoverFromSelectedMachine(t *testing.T) {
	keys, err := cloudkeys.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := domain.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := domain.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := domain.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := cloud.ViewerIdentity{AccountID: "account", DeviceID: "viewer", ViewerCredential: "viewer-bearer", APIBase: "test"}
	var out bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+identity.ViewerCredential {
			t.Errorf("request lacked viewer credential")
		}
		switch r.URL.Path {
		case "/v1/machines":
			_ = json.NewEncoder(w).Encode(map[string]any{"machines": []any{map[string]any{
				"id": "machine", "name": "Desk", "public_key": base64.StdEncoding.EncodeToString(machine.PublicKey()),
				"key_fingerprint": machine.Fingerprint(), "revoked_at": nil,
			}}})
		case "/v1/pairing/start":
			_ = json.NewEncoder(w).Encode(map[string]any{"pairing_id": "pair-1", "claim_nonce": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
				"expires_at": time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano), "expires_in": 120})
		case "/v1/pairing/claim":
			fields := strings.Fields(out.String())
			fragment := ""
			for _, field := range fields {
				if _, parseErr := domain.DecodePairingOfferFragment(field, time.Now().UnixMilli()); parseErr == nil {
					fragment = field
					break
				}
			}
			offer, parseErr := domain.DecodePairingOfferFragment(fragment, time.Now().UnixMilli())
			if parseErr != nil {
				t.Errorf("offer: %v", parseErr)
				w.WriteHeader(500)
				return
			}
			private, keyErr := domain.NewX25519PrivateKey(nil)
			if keyErr != nil {
				t.Error(keyErr)
				w.WriteHeader(500)
				return
			}
			nonce := make([]byte, domain.NonceBytes)
			if _, keyErr = rand.Read(nonce); keyErr != nil {
				t.Error(keyErr)
				w.WriteHeader(500)
				return
			}
			handover := domain.PairingHandover{AccountID: "account", MachineID: "machine", MachineSigningKey: base64.StdEncoding.EncodeToString(machine.PublicKey()),
				MachineFingerprint: machine.Fingerprint(), KeyID: "ms-1", MasterSecret: base64.StdEncoding.EncodeToString(secret.Bytes())}
			wrapper, sealErr := domain.SealPairingHandover(handover, offer, "machine", private, nonce, time.Now().UnixMilli())
			if sealErr != nil {
				t.Errorf("seal: %v", sealErr)
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ciphertext": base64.StdEncoding.EncodeToString(wrapper.CanonicalJSON()), "sender_device_id": "machine"})
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	if err := viewerPairRun(context.Background(), &out, cloud.NewAccountClient(server.URL), keys, identity, viewer.PublicKey(), "machine", machine.Fingerprint(), time.Minute); err != nil {
		t.Fatal(err)
	}
	pins, err := cloud.NewViewerPinStore(keys.Dir()).Load(identity)
	if err != nil || len(pins) != 1 || pins["machine"].MasterSecret != base64.StdEncoding.EncodeToString(secret.Bytes()) {
		t.Fatalf("pins %#v: %v", pins, err)
	}
	if strings.Contains(out.String(), pins["machine"].MasterSecret) {
		t.Fatal("printed content secret")
	}
}
