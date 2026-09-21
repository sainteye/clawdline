package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

// W7 ③: everything only the macOS shell has is, on every other platform, a
// named refusal that says what this machine is and what to do instead — never
// an answer that stores a setting for nothing to act on.
func TestEverythingOnlyAMacHasIsANamedRefusalElsewhere(t *testing.T) {
	cases := []struct {
		name string
		host Host
		want map[ports.CapabilityName]string
	}{
		{"linux on Wayland", Host{GOOS: "linux", Getenv: env(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})},
			map[ports.CapabilityName]string{
				ports.CapClipboard:     "a send hands the assistant the picture's path",
				ports.CapGlobalHotkey:  "Wayland",
				ports.CapNotch:         "no notch on linux",
				ports.CapLaunchAtLogin: "loginctl enable-linger",
			}},
		{"linux on X11", Host{GOOS: "linux", Getenv: env(map[string]string{"DISPLAY": ":0"})},
			map[ports.CapabilityName]string{ports.CapGlobalHotkey: "X11 could grab a key"}},
		{"headless linux", Host{GOOS: "linux", Getenv: env(nil)},
			map[ports.CapabilityName]string{ports.CapGlobalHotkey: "no desktop"}},
		{"windows", Host{GOOS: "windows", Getenv: env(nil)},
			map[ports.CapabilityName]string{
				ports.CapClipboard:     "no clipboard on windows",
				ports.CapGlobalHotkey:  "RegisterHotKey",
				ports.CapNotch:         "no notch on windows",
				ports.CapLaunchAtLogin: "shell:startup",
			}},
	}
	for _, c := range cases {
		caps := c.host.Capabilities(context.Background())
		if len(caps) != 4 {
			t.Fatalf("%s: %+v", c.name, caps)
		}
		for name, says := range c.want {
			got, ok := caps.Find(name)
			if !ok || got.State != ports.CapabilityUnavailable || !strings.Contains(got.Reason, says) {
				t.Errorf("%s: %s = %+v, want unavailable saying %q", c.name, name, got, says)
				continue
			}
			var named ports.Unavailable
			if err := got.Refusal(c.host.GOOS); !errors.As(err, &named) || named.Capability != name ||
				!strings.Contains(err.Error(), string(name)+" is unavailable on this machine ("+c.host.GOOS+")") {
				t.Errorf("%s: %s refusal %v", c.name, name, err)
			}
		}
	}
}

// On a Mac the shell has them — except the notch, which is the display's to
// say and so is unknown here, not yes; and the clipboard, which is whatever
// the clipboard adapter says.
func TestAMacHasTheShellsCapabilities(t *testing.T) {
	caps := Host{GOOS: "darwin", Getenv: env(nil), Clipboard: true}.Capabilities(context.Background())
	for name, state := range map[ports.CapabilityName]ports.CapabilityState{
		ports.CapClipboard:     ports.CapabilityAvailable,
		ports.CapGlobalHotkey:  ports.CapabilityAvailable,
		ports.CapNotch:         ports.CapabilityUnknown,
		ports.CapLaunchAtLogin: ports.CapabilityAvailable,
	} {
		got, _ := caps.Find(name)
		if got.State != state || got.Refusal("darwin") != nil {
			t.Errorf("%s = %+v, want %s and no refusal", name, got, state)
		}
	}
	if got, _ := (Host{GOOS: "darwin"}).Capabilities(context.Background()).Find(ports.CapClipboard); got.State != ports.CapabilityUnavailable {
		t.Errorf("a clipboard adapter that says no was answered %+v", got)
	}
}
