package http

import (
	"context"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/desktop"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// One spelling (DG-5): the names this daemon answers are the contract's names,
// in the contract's order, and a capability added to one and not the other is
// a failing test.
func TestPlatformCapabilityNamesAreTheContracts(t *testing.T) {
	if len(ports.CapabilityNames) != len(contract.PlatformCapabilityNameValues) {
		t.Fatalf("%v and %v", ports.CapabilityNames, contract.PlatformCapabilityNameValues)
	}
	for i, name := range ports.CapabilityNames {
		if string(name) != string(contract.PlatformCapabilityNameValues[i]) {
			t.Fatalf("#%d: %s here, %s in the contract", i, name, contract.PlatformCapabilityNameValues[i])
		}
	}
	for i, state := range []ports.CapabilityState{ports.CapabilityAvailable, ports.CapabilityUnavailable, ports.CapabilityUnknown} {
		if string(state) != string(contract.PlatformCapabilityStateValues[i]) {
			t.Fatalf("state %s is %s in the contract", state, contract.PlatformCapabilityStateValues[i])
		}
	}
}

// /v1/diagnostics.platform names every capability, and the three a dispatch
// needs are the broker's own answer — the same reading its door refuses on.
func TestDiagnosticsNamesEveryPlatformCapability(t *testing.T) {
	s := capacityServer(t)
	s.broker = &orchestrator.Broker{TerminalCapabilities: func(context.Context) ports.Capabilities {
		return ports.Capabilities{
			{Name: ports.CapReadScreen, State: ports.CapabilityUnavailable, Reason: "the fake machine cannot read"},
			{Name: ports.CapSendKeys, State: ports.CapabilityAvailable, Via: []string{"tmux"}},
		}
	}}
	d := s.platformDiagnostics(context.Background())
	if d.Os != runtime.GOOS || d.Arch != runtime.GOARCH || len(d.Capabilities) != len(ports.CapabilityNames) {
		t.Fatalf("%+v", d)
	}
	for i, c := range d.Capabilities {
		if c.Name != contract.PlatformCapabilityNameValues[i] {
			t.Fatalf("#%d is %s", i, c.Name)
		}
		if c.State != contract.PlatformCapabilityStateAvailable && c.Reason == "" {
			t.Errorf("%s is %s and says nothing about why", c.Name, c.State)
		}
	}
	if d.Capabilities[0].State != contract.PlatformCapabilityStateUnknown {
		t.Errorf("open_child with no launcher: %+v", d.Capabilities[0])
	}
	if d.Capabilities[1].Reason != "the fake machine cannot read" {
		t.Errorf("read_screen is not the broker's reading: %+v", d.Capabilities[1])
	}
}

// W7 ③ at the settings route: turning on what only the macOS shell does is
// refused by name on a machine without it; turning it off never is.
func TestSettingsRefuseWhatThisMachineCannotDo(t *testing.T) {
	linux := desktop.Host{GOOS: "linux", Getenv: func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}}.Capabilities(context.Background())
	for _, c := range []struct {
		changes map[string]any
		says    string
	}{
		{map[string]any{"hotkey": "cmd+shift+k"}, "hotkey: global_hotkey is unavailable on this machine (linux): this desktop is Wayland"},
		{map[string]any{"notch": true, "width": 800.0}, "notch: notch is unavailable on this machine (linux)"},
	} {
		got := platformSettingRefusal(linux, "linux", c.changes)
		if got == nil || got.code != "capability_unavailable" || !strings.HasPrefix(got.message, c.says) {
			t.Errorf("%v: %+v", c.changes, got)
		}
	}
	for _, off := range []map[string]any{{"hotkey": ""}, {"notch": false}, {"width": 800.0}} {
		if got := platformSettingRefusal(linux, "linux", off); got != nil {
			t.Errorf("%v was refused: %+v", off, got)
		}
	}
	mac := desktop.Host{GOOS: "darwin"}.Capabilities(context.Background())
	if got := platformSettingRefusal(mac, "darwin", map[string]any{"hotkey": "cmd+shift+k", "notch": true}); got != nil {
		t.Errorf("a machine refused its own shell's settings: %+v", got)
	}

	// And through the route, on whatever this test runs on.
	s := &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}}
	rec, _, refusal := settingsCall(t, s, http.MethodPost, "application/json", `{"hotkey":"cmd+shift+k"}`)
	switch runtime.GOOS {
	case "darwin":
		if rec.Code != http.StatusOK {
			t.Fatalf("a machine: %d %s", rec.Code, rec.Body)
		}
	default:
		if rec.Code != http.StatusNotImplemented || refusal.Error != "capability_unavailable" ||
			!strings.Contains(rec.Body.String(), "global_hotkey is unavailable on this machine ("+runtime.GOOS+")") {
			t.Fatalf("%s: %d %s", runtime.GOOS, rec.Code, rec.Body)
		}
	}
}
