package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/ports"
)

// Every platform's answer, checked on whichever one runs the test: the
// answers are decided from the backends' reaches, which are arguments here.
func TestTerminalCapabilitiesNameWhatIsMissingOnEachPlatform(t *testing.T) {
	noTmux := backendReach{name: "tmux", state: ports.CapabilityUnavailable, reason: "tmux is not installed"}
	tmux := backendReach{name: "tmux", state: ports.CapabilityAvailable}
	closed := backendReach{name: "iterm", state: ports.CapabilityUnavailable, reason: "iTerm2 is not running"}
	unread := backendReach{name: "iterm", state: ports.CapabilityUnknown, reason: "whether iTerm2 is running could not be read: x"}

	cases := []struct {
		name    string
		goos    string
		reaches []backendReach
		state   ports.CapabilityState
		via     string
		says    []string
	}{
		{"linux without tmux", "linux", []backendReach{noTmux}, ports.CapabilityUnavailable, "",
			[]string{"tmux is not installed", "linux has no iTerm2"}},
		{"windows", "windows", []backendReach{noTmux}, ports.CapabilityUnavailable, "",
			[]string{"tmux is not installed", "windows has no iTerm2", "ConPTY"}},
		{"linux with tmux", "linux", []backendReach{tmux}, ports.CapabilityAvailable, "tmux", []string{"through tmux"}},
		{"a machine with neither", "darwin", []backendReach{noTmux, closed}, ports.CapabilityUnavailable, "",
			[]string{"tmux is not installed", "iTerm2 is not running"}},
		{"a machine with tmux and iTerm2 closed", "darwin", []backendReach{tmux, closed}, ports.CapabilityAvailable, "tmux",
			[]string{"through tmux", "not otherwise: iTerm2 is not running"}},
		// Unknown is not no (DG-7).
		{"a machine whose iTerm2 could not be asked", "darwin", []backendReach{noTmux, unread}, ports.CapabilityUnknown, "",
			[]string{"could not be read", "tmux is not installed"}},
	}
	for _, c := range cases {
		caps := terminalCapabilities(c.goos, c.reaches)
		if len(caps) != 2 {
			t.Fatalf("%s: %+v", c.name, caps)
		}
		for _, name := range []ports.CapabilityName{ports.CapReadScreen, ports.CapSendKeys} {
			got, ok := caps.Find(name)
			if !ok || got.State != c.state || strings.Join(got.Via, ",") != c.via {
				t.Errorf("%s: %s = %+v", c.name, name, got)
				continue
			}
			for _, want := range c.says {
				if !strings.Contains(got.Reason, want) {
					t.Errorf("%s: %s does not say %q: %s", c.name, name, want, got.Reason)
				}
			}
			err := got.Refusal(c.goos)
			if (c.state == ports.CapabilityUnavailable) != (err != nil) {
				t.Errorf("%s: %s refusal %v", c.name, name, err)
			}
			var named ports.Unavailable
			if err != nil && (!asUnavailable(err, &named) || named.Capability != name || named.Platform != c.goos) {
				t.Errorf("%s: the refusal is not named: %v", c.name, err)
			}
		}
	}
}

func asUnavailable(err error, out *ports.Unavailable) bool {
	u, ok := err.(ports.Unavailable)
	if ok {
		*out = u
	}
	return ok
}

// tmux is looked for on the PATH the backend runs it from. A binary that is
// not there is not there, whatever else the launcher can find.
func TestATmuxTheBackendCannotRunIsNotAvailable(t *testing.T) {
	l := Launcher{Tmux: &Tmux{Binary: "clawdline-no-such-tmux"}}
	got := l.tmuxReach()
	if got.state != ports.CapabilityUnavailable || got.reason == "" {
		t.Fatalf("%+v", got)
	}
	caps := l.Capabilities(context.Background())
	if c, _ := caps.Find(ports.CapSendKeys); c.State == ports.CapabilityAvailable && strings.Join(c.Via, ",") == "tmux" {
		t.Fatalf("send_keys through a tmux that cannot run: %+v", c)
	}
}
