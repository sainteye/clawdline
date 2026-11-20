package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func onboardingRequest(t *testing.T, s *Server, route func(http.ResponseWriter, *http.Request), method, path, body string, local bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Local: local}}))
	rec := httptest.NewRecorder()
	route(rec, req)
	return rec
}

func TestCloudOnboardingSwitchesRequireTheMachineAndAnApprovedIdentity(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for _, route := range []func(http.ResponseWriter, *http.Request){s.cloudLoginRoute, s.cloudEnabledRoute, s.cloudCommandsRoute, s.cloudDirectRoute, s.cloudDirectLocalNetworkRoute} {
		if rec := onboardingRequest(t, s, route, http.MethodPost, "/v1/cloud/anything", `{"enabled":true}`, false); rec.Code != http.StatusForbidden {
			t.Fatalf("paired browser changed Cloud: %d %s", rec.Code, rec.Body)
		}
	}
	if rec := onboardingRequest(t, s, s.cloudEnabledRoute, http.MethodPost, "/v1/cloud/enabled", `{"enabled":true}`, true); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "cloud_not_signed_in") {
		t.Fatalf("Cloud enabled before approval: %d %s", rec.Code, rec.Body)
	}
	file := nextconfig.Open(s.cfg.Dir)
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil || settings.Enabled {
		t.Fatalf("unexpected defaults: %+v %v", settings, err)
	}
}

func TestLegacyCloudSetupCanEnableButCannotChangeOperationAccess(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	file := nextconfig.Open(s.cfg.Dir)
	if err := adaptercloud.SetEnabled(file, true); err != nil {
		t.Fatal(err)
	}
	if rec := onboardingRequest(t, s, s.cloudCommandsRoute, http.MethodPost, "/v1/cloud/commands", `{"enabled":true}`, true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"commands":true`) {
		t.Fatalf("legacy setup: %d %s", rec.Code, rec.Body)
	}
	if rec := onboardingRequest(t, s, s.cloudCommandsRoute, http.MethodPost, "/v1/cloud/commands", `{"enabled":false}`, true); rec.Code != http.StatusGone {
		t.Fatalf("legacy disable request: %d %s", rec.Code, rec.Body)
	}
	if settings, err := adaptercloud.ReadSettings(file); err != nil || !settings.Enabled {
		t.Fatalf("legacy request changed the Cloud connection: %+v %v", settings, err)
	}
	if rec := onboardingRequest(t, s, s.cloudCommandsRoute, http.MethodPost, "/v1/cloud/commands", `{"enabled":"yes"}`, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed switch: %d %s", rec.Code, rec.Body)
	}
}

func TestCloudDirectSwitchReloadsAnEnabledLineAndKeepsOtherSettings(t *testing.T) {
	dir := t.TempDir()
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true}); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir}}
	reloads := 0
	s.SetCloudReload(func() error { reloads++; return nil })
	for _, on := range []bool{false, true} {
		body := `{"enabled":false}`
		if on {
			body = `{"enabled":true}`
		}
		rec := onboardingRequest(t, s, s.cloudDirectRoute, http.MethodPost, "/v1/cloud/direct", body, true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"terminal_direct":`) {
			t.Fatalf("direct switch: %d %s", rec.Code, rec.Body)
		}
		settings, err := adaptercloud.ReadSettings(file)
		if err != nil || settings.TerminalDirect != on || !settings.Enabled {
			t.Fatalf("direct switch changed another setting: %+v %v", settings, err)
		}
	}
	if reloads != 2 {
		t.Fatalf("expected a live reload for each switch, got %d", reloads)
	}
	if rec := onboardingRequest(t, s, s.cloudDirectRoute, http.MethodPost, "/v1/cloud/direct", `{"enabled":"yes"}`, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed direct switch: %d %s", rec.Code, rec.Body)
	}
}

func TestCloudSameNetworkSwitchReloadsAnEnabledLineAndKeepsOtherSettings(t *testing.T) {
	dir := t.TempDir()
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true}); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir}}
	reloads := 0
	s.SetCloudReload(func() error { reloads++; return nil })
	if settings, err := adaptercloud.ReadSettings(file); err != nil || settings.DirectLocalNetwork {
		t.Fatalf("same network on by default: %+v %v", settings, err)
	}
	for _, on := range []bool{true, false} {
		body := `{"enabled":false}`
		if on {
			body = `{"enabled":true}`
		}
		rec := onboardingRequest(t, s, s.cloudDirectLocalNetworkRoute, http.MethodPost, "/v1/cloud/direct/local-network", body, true)
		if rec.Code != http.StatusOK || rec.Body.String() != fmt.Sprintf("{\"direct_local_network\":%v}\n", on) {
			t.Fatalf("same-network switch: %d %s", rec.Code, rec.Body)
		}
		settings, err := adaptercloud.ReadSettings(file)
		if err != nil || settings.DirectLocalNetwork != on || !settings.Enabled || !settings.TerminalDirect {
			t.Fatalf("same-network switch changed another setting: %+v %v", settings, err)
		}
	}
	if reloads != 2 {
		t.Fatalf("expected a live reload for each switch, got %d", reloads)
	}
	if rec := onboardingRequest(t, s, s.cloudDirectLocalNetworkRoute, http.MethodPost, "/v1/cloud/direct/local-network", `{"enabled":"yes"}`, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed same-network switch: %d %s", rec.Code, rec.Body)
	}
	if rec := onboardingRequest(t, s, s.cloudDirectLocalNetworkRoute, http.MethodGet, "/v1/cloud/direct/local-network", "", true); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("same-network switch by GET: %d %s", rec.Code, rec.Body)
	}
}

func TestCloudOnboardingRoutesRejectPairedBrowsersThroughTheRealGate(t *testing.T) {
	s, err := New(config.Config{Dir: filepath.Join(t.TempDir(), "next"), Port: 7757})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.store.Close() })
	_, viewer, err := s.gate().auth.AddDevice("paired", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/cloud/login", "/v1/cloud/enabled", "/v1/cloud/commands", "/v1/cloud/direct", "/v1/cloud/direct/local-network"} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7757"+path, strings.NewReader(`{"enabled":true}`))
		req.Header.Set("Authorization", "Bearer "+viewer)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
			t.Fatalf("%s reached the local control: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestCloudOnboardingLoginIsIdempotentAndConnectsWithoutRestart(t *testing.T) {
	starts, polls := 0, 0
	plane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/device/start":
			starts++
			_ = json.NewEncoder(w).Encode(adaptercloud.LoginStart{DeviceCode: "secret-device-code", UserCode: "ABCD-1234", VerificationURIComplete: "https://example.test/approve", ExpiresIn: 600, Interval: 5})
		case "/v1/auth/device/poll":
			polls++
			_ = json.NewEncoder(w).Encode(adaptercloud.LoginPoll{Status: adaptercloud.LoginComplete, AccountID: "usr_test", MachineID: "mac_test", MachineCredential: "machine-secret"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer plane.Close()
	dir := t.TempDir()
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyAPIBase: plane.URL}); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir}, version: "v-test"}
	reloads := 0
	s.SetCloudReload(func() error {
		reloads++
		settings, err := adaptercloud.ReadSettings(file)
		if err != nil {
			return err
		}
		if settings.Enabled {
			SetCloudLine(dir, pairingOnlyLine{})
		} else {
			SetCloudLine(dir, nil)
		}
		return nil
	})
	t.Cleanup(func() { SetCloudLine(dir, nil) })
	for i := 0; i < 2; i++ {
		rec := onboardingRequest(t, s, s.cloudLoginRoute, http.MethodPost, "/v1/cloud/login", "", true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ABCD-1234") || strings.Contains(rec.Body.String(), "secret-device-code") {
			t.Fatalf("start %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if starts != 1 {
		t.Fatalf("duplicate start made %d registrations", starts)
	}
	if rec := onboardingRequest(t, s, s.cloudLoginRoute, http.MethodGet, "/v1/cloud/login", "", true); rec.Code != http.StatusOK || polls != 0 {
		t.Fatalf("polled before interval: %d polls, %d %s", polls, rec.Code, rec.Body)
	}
	s.cloudLogin.next = time.Now().Add(-time.Second)
	rec := onboardingRequest(t, s, s.cloudLoginRoute, http.MethodGet, "/v1/cloud/login", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"complete"`) || strings.Contains(rec.Body.String(), "machine-secret") {
		t.Fatalf("approval: %d %s", rec.Code, rec.Body)
	}
	keys, err := cloudkeys.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity, found, err := adaptercloud.NewIdentityStore(keys.Dir()).Load()
	if err != nil || !found || identity.MachineID != "mac_test" {
		t.Fatalf("identity not saved: %+v %t %v", identity, found, err)
	}
	for i := 0; i < 2; i++ {
		rec = onboardingRequest(t, s, s.cloudEnabledRoute, http.MethodPost, "/v1/cloud/enabled", `{"enabled":true}`, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("enable %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if reloads != 1 || polls != 1 {
		t.Fatalf("reloads=%d polls=%d", reloads, polls)
	}
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil || !settings.Enabled {
		t.Fatalf("approval did not connect Cloud: %+v %v", settings, err)
	}
	if rec = onboardingRequest(t, s, s.cloudEnabledRoute, http.MethodPost, "/v1/cloud/enabled", `{"enabled":false}`, true); rec.Code != http.StatusOK || reloads != 2 {
		t.Fatalf("disconnect: %d %s reloads=%d", rec.Code, rec.Body, reloads)
	}
	rec = onboardingRequest(t, s, s.cloudLoginRoute, http.MethodPost, "/v1/cloud/login", `{"replace":true}`, true)
	if rec.Code != http.StatusOK || starts != 2 || !strings.Contains(rec.Body.String(), `"phase":"waiting"`) {
		t.Fatalf("explicit re-registration: starts=%d %d %s", starts, rec.Code, rec.Body)
	}
	rec = onboardingRequest(t, s, s.cloudLoginRoute, http.MethodGet, "/v1/cloud/login", "", true)
	if !strings.Contains(rec.Body.String(), `"phase":"waiting"`) {
		t.Fatalf("existing identity hid the replacement attempt: %s", rec.Body)
	}
}

func TestCloudLoginDenialKeepsItsCodeAndCanStartAgain(t *testing.T) {
	starts := 0
	plane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/device/start" {
			starts++
			_ = json.NewEncoder(w).Encode(adaptercloud.LoginStart{DeviceCode: "secret", UserCode: "CODE", VerificationURIComplete: "https://example.test/approve", ExpiresIn: 600, Interval: 5})
			return
		}
		_ = json.NewEncoder(w).Encode(adaptercloud.LoginPoll{Status: adaptercloud.LoginDenied})
	}))
	defer plane.Close()
	dir := t.TempDir()
	if _, err := nextconfig.Open(dir).Set(map[string]any{adaptercloud.KeyAPIBase: plane.URL}); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: dir}}
	onboardingRequest(t, s, s.cloudLoginRoute, http.MethodPost, "/v1/cloud/login", "", true)
	s.cloudLogin.next = time.Now().Add(-time.Second)
	rec := onboardingRequest(t, s, s.cloudLoginRoute, http.MethodGet, "/v1/cloud/login", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"code":"login_denied"`) {
		t.Fatalf("denial lost its typed cause: %d %s", rec.Code, rec.Body)
	}
	rec = onboardingRequest(t, s, s.cloudLoginRoute, http.MethodPost, "/v1/cloud/login", "", true)
	if rec.Code != http.StatusOK || starts != 2 || !strings.Contains(rec.Body.String(), `"phase":"waiting"`) {
		t.Fatalf("denied login could not retry: starts=%d %d %s", starts, rec.Code, rec.Body)
	}
}

func TestCloudViewerEnablesSameNetworkWithoutLosingItsReceipt(t *testing.T) {
	h := newPushStandIn(t)
	h.bridge.AllowCommands = func() bool { return true }
	file := nextconfig.Open(h.server.cfg.Dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true}); err != nil {
		t.Fatal(err)
	}
	reloads := 0
	h.server.SetCloudReload(func() error { reloads++; return nil })
	for seq := uint64(1); seq <= 2; seq++ {
		a, payload := h.ask(t, seq, map[string]any{"type": "direct-local-network-enable", "session": cloudops.MachineReplySession, "request": fmt.Sprintf("enable-%d", seq)})
		if !a.OK() || !strings.Contains(string(a.Payload), `"direct_local_network":true`) {
			t.Fatalf("enable receipt: %+v, %+v", a, payload)
		}
	}
	settings, err := adaptercloud.ReadSettings(file)
	if err != nil || !settings.DirectLocalNetwork || !settings.TerminalDirect || reloads != 0 {
		t.Fatalf("settings %+v, reloads %d, err %v", settings, reloads, err)
	}
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: false, adaptercloud.KeyDirectLocalNetwork: false}); err != nil {
		t.Fatal(err)
	}
	// The route rechecks the persisted connection setting even after bridge admission.
	a, _ := h.ask(t, 3, map[string]any{"type": "direct-local-network-enable", "session": cloudops.MachineReplySession, "request": "blocked"})
	if a.Status != http.StatusForbidden {
		t.Fatalf("disconnected Cloud: %+v", a)
	}
	settings, _ = adaptercloud.ReadSettings(file)
	if settings.DirectLocalNetwork {
		t.Fatal("a disconnected Cloud viewer changed the policy")
	}
}
