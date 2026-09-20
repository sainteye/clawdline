package cloud

import (
	"context"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
)

// A machine that has never been named must not publish a platform it is not.
//
// This is the defect in the shape it reached a person: the descriptor carries
// `machine.name` and `machine.platform` side by side, and the name said Mac
// while the platform said linux. Every case below is a machine whose `goos`
// this build knows is not macOS, so "Mac" anywhere in the answer is wrong
// whatever else the answer says.
func TestAnUnnamedMachineNeverPublishesItselfAsAMac(t *testing.T) {
	for _, tc := range []struct {
		goos, host, want string
	}{
		{goos: "linux", want: "Unnamed Linux machine"},
		{goos: "windows", want: "Unnamed Windows machine"},
		{goos: "openbsd", want: "Unnamed machine"},
		// A host name every machine on the network answers names none of
		// them, so it is no answer and the ladder goes on.
		{goos: "linux", host: "localhost", want: "Unnamed Linux machine"},
		{goos: "linux", host: "localhost.localdomain", want: "Unnamed Linux machine"},
		{goos: "linux", host: "  ", want: "Unnamed Linux machine"},
	} {
		got := machineName(adaptercloud.Identity{}, adaptercloud.Settings{}, tc.host, tc.goos)
		if got != tc.want {
			t.Errorf("a nameless %s machine with host %q publishes %q, want %q", tc.goos, tc.host, got, tc.want)
		}
		if strings.Contains(got, "Mac") {
			t.Errorf("a %s machine published itself as %q: the account's machine list would show a Mac that is not one",
				tc.goos, got)
		}
	}
}

// A Mac with no name is still allowed to say Mac — the fix is that the word is
// read off the platform rather than assumed.
func TestAnUnnamedMacStillSaysMac(t *testing.T) {
	if got := machineName(adaptercloud.Identity{}, adaptercloud.Settings{}, "", "darwin"); got != "Unnamed Mac" {
		t.Errorf("a nameless Mac publishes %q", got)
	}
}

// The host name is the rung above "no name at all", and it is the same host
// name `clawdline cloud login` registers with, so a machine whose identity
// predates the stored name goes on being called what it was called.
func TestTheHostNameIsTheRungAboveNoNameAtAll(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"host-in-a-datacentre", "host-in-a-datacentre"},
		{"studio.local", "studio"},
		{"studio.local.", "studio"},
		{"  studio  ", "studio"},
	} {
		if got := machineName(adaptercloud.Identity{}, adaptercloud.Settings{}, tc.host, "linux"); got != tc.want {
			t.Errorf("host %q publishes %q, want %q", tc.host, got, tc.want)
		}
	}
}

// Precedence, and that it is the caller's to state: the publisher trusts the
// settings override over the registered name because renaming a machine must
// not need the control plane.
func TestTheChosenNameOutranksTheRegisteredOneWhichOutranksTheHost(t *testing.T) {
	identity := adaptercloud.Identity{Name: "registered"}
	settings := adaptercloud.Settings{MachineName: "chosen"}

	if got := machineName(identity, settings, "the-host", "linux"); got != "chosen" {
		t.Errorf("the settings override did not win: %q", got)
	}
	if got := machineName(identity, adaptercloud.Settings{}, "the-host", "linux"); got != "registered" {
		t.Errorf("the registered name did not win over the host: %q", got)
	}
	if got := machineName(adaptercloud.Identity{}, adaptercloud.Settings{}, "the-host", "linux"); got != "the-host" {
		t.Errorf("the host name did not win over having no name: %q", got)
	}
	// Login states the other order — its `-name` flag over the settings — and
	// the ladder takes it as given rather than holding an opinion.
	if got := MachineName("the-host", "linux", "", "from-settings"); got != "from-settings" {
		t.Errorf("an empty first candidate was not skipped: %q", got)
	}
}

// The descriptor is where the name is read from, so the guarantee is asserted
// where a viewer would see it break: one snapshot carrying both fields.
func TestTheDescriptorNeverNamesAPlatformThisMachineIsNot(t *testing.T) {
	out := &collector{}
	publisher := newPublisher(&fixedRouter{body: completeScan}, out)
	publisher.MachineName = machineName(adaptercloud.Identity{}, adaptercloud.Settings{}, "", "linux")
	publisher.Platform = "linux"
	publisher.firstPass(context.Background())

	body := out.payload(t, "orch/mac-01")
	machine, _ := body["machine"].(map[string]any)
	name, _ := machine["name"].(string)
	if machine["platform"] != "linux" {
		t.Fatalf("the descriptor did not carry the platform under test: %v", machine)
	}
	if strings.Contains(name, "Mac") {
		t.Errorf("the account's machine list would show %q for a machine whose own descriptor says platform %v",
			name, machine["platform"])
	}
}
