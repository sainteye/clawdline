package ports

import (
	"context"
	"fmt"
)

// A platform capability is one thing this machine can or cannot do, by name
// (docs/broker-design.md #43, docs/cross-platform.md §5).
//
// **Named, and answered before anything is tried.** A machine that cannot open
// a child used to find out by opening one: the task was recorded, the spawn
// failed, and the failure was a record somebody had to read. Each capability
// here is answered from facts that cost nothing to read — is tmux installed,
// is iTerm2 running, which operating system is this — so a caller can be
// refused at the door, and the refusal can say which thing is missing.
//
// Not the device capabilities of internal/domain/auth (`read`, `send`, …),
// which say what a paired device may ask for. These say what this machine can
// do at all, whoever asks.

// CapabilityName is one platform capability.
type CapabilityName string

const (
	// CapOpenChild is opening a session for a dispatched child and briefing
	// it: a terminal this daemon can open, type into and read. It is the
	// broker's to answer, because which terminal it opens is decided by the
	// machine's `terminal` setting as well as by what is installed.
	CapOpenChild CapabilityName = "open_child"
	// CapReadScreen is reading what a session's terminal shows.
	CapReadScreen CapabilityName = "read_screen"
	// CapSendKeys is typing into a session: a line, a keystroke, an interrupt.
	CapSendKeys CapabilityName = "send_keys"
	// CapClipboard is lending a picture to the system clipboard for a send.
	CapClipboard CapabilityName = "clipboard"
	// CapGlobalHotkey is a key combination that answers wherever the focus is.
	CapGlobalHotkey CapabilityName = "global_hotkey"
	// CapNotch is the island drawn around a MacBook's notch.
	CapNotch CapabilityName = "notch"
	// CapLaunchAtLogin is starting when the person logs in.
	CapLaunchAtLogin CapabilityName = "launch_at_login"
)

// CapabilityNames is every platform capability, in the order a reader is
// shown them.
var CapabilityNames = []CapabilityName{
	CapOpenChild, CapReadScreen, CapSendKeys, CapClipboard, CapGlobalHotkey, CapNotch, CapLaunchAtLogin,
}

// CapabilityState is three values, not two (design-guidelines DG-7): a probe
// that could not be read has said nothing, and "unknown" never refuses anybody.
type CapabilityState string

const (
	CapabilityAvailable   CapabilityState = "available"
	CapabilityUnavailable CapabilityState = "unavailable"
	CapabilityUnknown     CapabilityState = "unknown"
)

// Capability is one answer about this machine.
type Capability struct {
	Name  CapabilityName
	State CapabilityState
	// Via is what provides it here — "tmux", "iterm2", "pasteboard", or
	// "shell" for the macOS shell (shell/darwin) — and empty when nothing
	// does.
	Via []string
	// Reason is this machine's sentence: why not, why unknown, or what the
	// capability rests on when it is there.
	Reason string
}

// Capabilities is a set of answers, at most one per name.
type Capabilities []Capability

// Find is the answer for name, if this set has one.
func (c Capabilities) Find(name CapabilityName) (Capability, bool) {
	for _, one := range c {
		if one.Name == name {
			return one, true
		}
	}
	return Capability{}, false
}

// Missing is every one of names this set knows to be unavailable, in the order
// asked. A capability that is unknown, or that the set does not answer, is not
// missing: only a positive "no" refuses (DG-7).
func (c Capabilities) Missing(names ...CapabilityName) Capabilities {
	var out Capabilities
	for _, name := range names {
		if one, ok := c.Find(name); ok && one.State == CapabilityUnavailable {
			out = append(out, one)
		}
	}
	return out
}

// Unavailable is the named refusal for something this machine cannot do.
//
// It says which capability and why, on this machine: "this platform cannot
// do that" is a sentence nobody can act on, and "global_hotkey is unavailable
// on linux: Wayland does not let a program take a key" is one somebody can.
type Unavailable struct {
	Capability CapabilityName
	// Platform is the operating system the answer was given on.
	Platform string
	Reason   string
}

func (e Unavailable) Error() string {
	return fmt.Sprintf("%s is unavailable on this machine (%s): %s", e.Capability, e.Platform, e.Reason)
}

// Refusal is the error a request for this capability gets, or nil when the
// capability is not known to be unavailable.
func (c Capability) Refusal(platform string) error {
	if c.State != CapabilityUnavailable {
		return nil
	}
	return Unavailable{Capability: c.Name, Platform: platform, Reason: c.Reason}
}

// CapabilityHost answers what this machine can do without trying any of it.
// Every probe behind it is a read — a path lookup, a question to Launch
// Services, an environment variable — and none opens, types or registers
// anything.
type CapabilityHost interface {
	Capabilities(ctx context.Context) Capabilities
}
