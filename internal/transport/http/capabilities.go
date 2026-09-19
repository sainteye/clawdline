package http

import (
	"context"
	"runtime"

	"github.com/sainteye/clawdline-go/internal/adapters/desktop"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// This machine's platform capabilities, by name (broker-design #43,
// cross-platform §5, W7): the block /v1/diagnostics carries, and the settings
// a machine without the capability refuses by name rather than storing for
// nothing to act on.
//
// The three a dispatch needs are the broker's own answer — the one its door
// asks (orchestrator capability.go) — so what diagnostics shows and what a
// dispatch is refused on are one reading, not two.

// platformCapabilities is every platform capability, in ports.CapabilityNames
// order.
func (s *Server) platformCapabilities(ctx context.Context) ports.Capabilities {
	var all ports.Capabilities
	if s.broker != nil {
		all = append(all, s.broker.ChildCapabilities(ctx)...)
	}
	all = append(all, s.desktopHost().Capabilities(ctx)...)
	out := make(ports.Capabilities, 0, len(ports.CapabilityNames))
	for _, name := range ports.CapabilityNames {
		if c, ok := all.Find(name); ok {
			out = append(out, c)
		}
	}
	return out
}

// desktopHost is this machine's desktop, with the clipboard answer the send path
// itself acts on.
func (s *Server) desktopHost() desktop.Host { return desktop.New(s.pictures.pasteboard.Available()) }

// platformDiagnostics is /v1/diagnostics.platform.
func (s *Server) platformDiagnostics(ctx context.Context) contract.PlatformDiagnostics {
	caps := s.platformCapabilities(ctx)
	out := contract.PlatformDiagnostics{Os: runtime.GOOS, Arch: runtime.GOARCH,
		Capabilities: make([]contract.PlatformCapability, 0, len(caps))}
	for _, c := range caps {
		out.Capabilities = append(out.Capabilities, contract.PlatformCapability{
			Name: contract.PlatformCapabilityName(c.Name), State: contract.PlatformCapabilityState(c.State),
			Via: c.Via, Reason: c.Reason,
		})
	}
	return out
}

// platformSettings are the settings that turn on a capability this daemon
// does not provide itself: the macOS shell reads them, and on a machine with
// no such shell nothing ever would.
var platformSettings = map[string]ports.CapabilityName{
	"hotkey": ports.CapGlobalHotkey,
	"notch":  ports.CapNotch,
}

// platformSettingRefusal is a settings change that turns on something this
// machine positively does not have: `capability_unavailable`, the start
// route's code for the same fact, with the capability's own sentence.
// Turning one off is always taken — a config.json copied from a Mac can be
// cleaned — and an unknown answer refuses nothing.
func platformSettingRefusal(caps ports.Capabilities, goos string, changes map[string]any) *settingsRefusal {
	for key, name := range platformSettings {
		value, ok := changes[key]
		if !ok || !turnsOn(value) {
			continue
		}
		c, known := caps.Find(name)
		if !known {
			continue
		}
		if err := c.Refusal(goos); err != nil {
			return &settingsRefusal{"capability_unavailable", key + ": " + err.Error()}
		}
	}
	return nil
}

// turnsOn is a setting's value asking for the thing: a key combination, or
// true.
func turnsOn(value any) bool {
	switch v := value.(type) {
	case string:
		return v != ""
	case bool:
		return v
	}
	return false
}
