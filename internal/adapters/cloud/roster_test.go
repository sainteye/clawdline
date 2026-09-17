package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The roster is what stands between "this device is paired" and every inbound
// envelope being counted `unknown_sender`, so what it does with each shape of
// answer is worth pinning.

func TestARevokedDeviceIsNotOnTheRoster(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer machine-credential" {
			t.Errorf("the roster asked without this machine's credential: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"devices":[
          {"id":"web_live","kind":"browser","name":"Web browser","caps":["read_sessions","send_prompt"],
           "public_key":"7VtRINFXV/rWD1nwMh0SXHbBpYo+hIrqpMHLVLFfd9A=","key_fingerprint":"AAAA-BBBB","revoked_at":null},
          {"id":"web_gone","kind":"browser","name":"Old phone","caps":["read_sessions"],
           "public_key":"7VtRINFXV/rWD1nwMh0SXHbBpYo+hIrqpMHLVLFfd9A=","key_fingerprint":"CCCC-DDDD","revoked_at":"2026-09-01T00:00:00Z"},
          {"id":"web_unreadable","kind":"browser","name":"Broken","caps":[],
           "public_key":"not base64","key_fingerprint":"","revoked_at":null}]}`))
	}))
	defer server.Close()

	roster := NewRoster(server.URL, "machine-credential", time.Now)
	if err := roster.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, ok := roster.PublicKeyFor("web_live"); !ok {
		t.Error("the live device was not admitted")
	}
	if _, ok := roster.PublicKeyFor("web_gone"); ok {
		t.Error("a revoked device was admitted")
	}
	// One malformed row must not lock out the others: it is left out, and the
	// roster is still readable.
	if _, ok := roster.PublicKeyFor("web_unreadable"); ok {
		t.Error("a device whose key does not decode was admitted")
	}
	if readable, err := roster.Readable(); !readable || err != nil {
		t.Errorf("the roster should be readable: %v %v", readable, err)
	}
	if _, ok := roster.PublicKeyFor("web_never_seen"); ok {
		t.Error("a device that is not on the roster was admitted")
	}
}

func TestAnUnreadableRosterIsNotAnEmptyOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	roster := NewRoster(server.URL, "machine-credential", time.Now)
	if err := roster.Refresh(context.Background()); err == nil {
		t.Fatal("a 503 was reported as a successful read")
	}
	readable, err := roster.Readable()
	if readable || err == nil {
		t.Errorf("an unreadable roster reported itself readable: %v %v", readable, err)
	}
	if len(roster.Devices()) != 0 {
		t.Error("a failed fetch invented devices")
	}
}

func TestAFailedRefreshKeepsTheLastGoodAnswer(t *testing.T) {
	answer := `{"devices":[{"id":"web_live","public_key":"7VtRINFXV/rWD1nwMh0SXHbBpYo+hIrqpMHLVLFfd9A=","revoked_at":null}]}`
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	roster := NewRoster(server.URL, "machine-credential", time.Now)
	if err := roster.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	fail = true
	_ = roster.Refresh(context.Background())
	// A viewer admitted a second ago must not stop being admitted because the
	// control plane hiccuped.
	if _, ok := roster.PublicKeyFor("web_live"); !ok {
		t.Error("a failed refresh forgot a device it had already admitted")
	}
	if readable, _ := roster.Readable(); readable {
		t.Error("a failed refresh still reported the roster readable")
	}
}

func TestARosterWithNoCredentialAsksNobody(t *testing.T) {
	asked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked = true
		_, _ = w.Write([]byte(`{"devices":[]}`))
	}))
	defer server.Close()

	roster := NewRoster(server.URL, "", time.Now)
	if err := roster.Refresh(context.Background()); err == nil {
		t.Error("a roster with no credential reported a successful read")
	}
	if asked {
		t.Error("a roster with no credential still made a request")
	}
}
