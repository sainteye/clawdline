package terminal

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline-go/internal/app/ports"
)

// What this machine's terminals can do, asked without doing any of it
// (docs/broker-design.md #43): reading a session's screen and typing into it,
// each by name, each with the backends that provide it here and, when none
// does, why not.
//
// Every probe is a read. tmux is looked up on the PATH the tmux backend runs
// it from; iTerm2 is asked of Launch Services, which never starts it. Nothing
// is opened, typed or listed.

// backendReach is one backend's answer to "could a session be driven through
// you right now".
type backendReach struct {
	name   string
	state  ports.CapabilityState
	reason string
}

var _ ports.CapabilityHost = Launcher{}

// Capabilities is read_screen and send_keys on this machine.
func (l Launcher) Capabilities(ctx context.Context) ports.Capabilities {
	return terminalCapabilities(runtime.GOOS, l.reaches(ctx))
}

// reaches asks each backend this platform has.
func (l Launcher) reaches(ctx context.Context) []backendReach {
	out := []backendReach{l.tmuxReach()}
	if runtime.GOOS == "darwin" {
		out = append(out, l.itermReach(ctx))
	}
	return out
}

// tmuxReach looks for the binary the tmux backend itself runs, which is
// `tmux` on this process's PATH (Tmux.run). The launcher also tries the places
// package managers put tmux (Launcher.binary), and when only it finds one the
// two disagree: a child's tab would open and the briefing typed into it would
// fail. That is named rather than reported as either answer.
func (l Launcher) tmuxReach() backendReach {
	binary := "tmux"
	if l.Tmux != nil && l.Tmux.Binary != "" {
		binary = l.Tmux.Binary
	}
	if _, err := exec.LookPath(binary); err == nil {
		return backendReach{name: "tmux", state: ports.CapabilityAvailable}
	}
	if l.Tmux != nil {
		if found := l.binary(); found != "" {
			return backendReach{name: "tmux", state: ports.CapabilityUnavailable,
				reason: "tmux is at " + found + ", which is not on this daemon's PATH, and the tmux backend runs `tmux` from the PATH"}
		}
	}
	return backendReach{name: "tmux", state: ports.CapabilityUnavailable, reason: "tmux is not installed"}
}

// itermReach is whether iTerm2 is running: its sessions are read and typed
// into through Apple Events, and a closed iTerm2 has no sessions to drive.
func (l Launcher) itermReach(ctx context.Context) backendReach {
	running, err := l.ITermRunning(ctx)
	switch {
	case err != nil:
		return backendReach{name: "iterm", state: ports.CapabilityUnknown,
			reason: "whether iTerm2 is running could not be read: " + err.Error()}
	case running:
		return backendReach{name: "iterm", state: ports.CapabilityAvailable}
	}
	return backendReach{name: "iterm", state: ports.CapabilityUnavailable, reason: "iTerm2 is not running"}
}

// terminalCapabilities is the two answers from the backends' reaches. It reads
// nothing, so every platform's answer can be checked on any one of them.
func terminalCapabilities(goos string, reaches []backendReach) ports.Capabilities {
	one := func(name ports.CapabilityName) ports.Capability {
		var via, unknown, absent []string
		for _, r := range reaches {
			switch r.state {
			case ports.CapabilityAvailable:
				via = append(via, r.name)
			case ports.CapabilityUnknown:
				unknown = append(unknown, r.reason)
			default:
				absent = append(absent, r.reason)
			}
		}
		switch {
		case len(via) > 0:
			// The backends that cannot are said too: a child opens in one
			// backend, and "available through iTerm2" is no answer for a
			// child that would open in tmux (orchestrator capability.go).
			reason := "through " + strings.Join(via, " and ")
			if len(absent)+len(unknown) > 0 {
				reason += "; not otherwise: " + strings.Join(append(absent, unknown...), "; ")
			}
			return ports.Capability{Name: name, State: ports.CapabilityAvailable, Via: via, Reason: reason}
		case len(unknown) > 0:
			return ports.Capability{Name: name, State: ports.CapabilityUnknown,
				Reason: strings.Join(append(unknown, absent...), "; ")}
		}
		// The platform's own absences are said too, so that a reader on Linux
		// knows installing tmux is the whole answer and one on Windows knows
		// it is not.
		switch goos {
		case "darwin":
		case "windows":
			absent = append(absent, "windows has no iTerm2, and a console this daemon owns (ConPTY) is not built yet")
		default:
			absent = append(absent, goos+" has no iTerm2")
		}
		return ports.Capability{Name: name, State: ports.CapabilityUnavailable, Reason: strings.Join(absent, "; ")}
	}
	return ports.Capabilities{one(ports.CapReadScreen), one(ports.CapSendKeys)}
}
