package orchestrator

import (
	"context"
	"net/http"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// Whether this machine can open a child, answered before anything exists
// (docs/broker-design.md #43 and §6.7, design-decisions W7).
//
// A spawn that cannot happen used to be discovered by trying it: the task was
// recorded, the terminal refused, and a `spawn_failed` record was all anybody
// learned — the Swift app's D5, where failed spawns pile up on their own. Now
// a dispatch asks first, from facts that cost nothing to read, and a machine
// that cannot carry a child answers `409 no_child_capability` naming which
// capability is missing and why, with nothing recorded, made or opened.
//
// **The question is the one the spawn acts on.** planChild is the decision
// spawn takes — the machine's `terminal` setting, whether iTerm2 is running,
// how far tmux reaches — read by the same calls, so the door and the room
// cannot disagree about which terminal a child opens in (design-guidelines
// DG-6). The facts can still move between the two reads: iTerm2 can be quit in
// between. The spawn then fails exactly as it always has, with the same
// sentence this refusal would have given.
//
// Only a positive "no" refuses. A broker with no launcher to ask, or an iTerm2
// that could not be asked, has said nothing, and the dispatch goes on to the
// spawn that decides with evidence (DG-7).

// childNeeds is what a child needs of this machine: a terminal the broker can
// open, and in it a screen to read before the briefing is typed — typing at a
// dialog answers the dialog (composer.go) — and keys to type it with.
var childNeeds = []ports.CapabilityName{ports.CapOpenChild, ports.CapReadScreen, ports.CapSendKeys}

// childPlan is which terminal a child would open in now, and the facts that
// decided it.
type childPlan struct {
	kind      projects.PlanKind
	choice    projects.TerminalChoice
	reach     projects.TmuxReach
	itermOpen bool
	// itermErr is iTerm2's running state failing to be read. The plan then
	// reads it as closed, as the spawn always has; the capability reads it as
	// unknown.
	itermErr error
	// asked is whether there was a launcher to ask at all.
	asked bool
}

// planChild reads the three facts a spawn chooses its terminal from.
func (b *Broker) planChild(ctx context.Context) childPlan {
	p := childPlan{choice: projects.TerminalAuto, reach: projects.TmuxAbsent}
	if b.Launcher != nil {
		p.asked = true
		p.itermOpen, p.itermErr = b.Launcher.ITermRunning(ctx)
		p.reach = projects.TmuxReach(b.Launcher.TmuxReach(ctx))
	}
	if b.Terminal != nil {
		p.choice = b.Terminal()
	}
	p.kind = projects.ChoosePlan(p.choice, p.itermOpen, p.reach)
	return p
}

// backend is the session backend the plan opens a child in, or "" for none.
func (p childPlan) backend() string {
	switch p.kind {
	case projects.PlanITerm:
		return "iterm"
	case projects.PlanTmux, projects.PlanTmuxDetached:
		return "tmux"
	}
	return ""
}

// capability is open_child as this plan answers it.
func (p childPlan) capability(goos string) ports.Capability {
	c := ports.Capability{Name: ports.CapOpenChild}
	switch backend := p.backend(); {
	case backend == "iterm":
		c.State, c.Via, c.Reason = ports.CapabilityAvailable, []string{backend}, "a new iTerm2 tab for each child"
	case backend == "tmux":
		c.State, c.Via, c.Reason = ports.CapabilityAvailable, []string{backend}, "a new detached tmux session for each child"
	case !p.asked:
		c.State, c.Reason = ports.CapabilityUnknown, "this broker has no launcher to ask"
	case p.itermErr != nil && p.choice != projects.TerminalTmux:
		c.State, c.Reason = ports.CapabilityUnknown, "whether iTerm2 is running could not be read: "+p.itermErr.Error()
	default:
		c.State, c.Reason = ports.CapabilityUnavailable, p.failure(goos)
	}
	return c
}

// failure is the sentence for a plan that opens nothing: the refusal's clause
// and, on the spawn that loses the race, its own message. On a Mac the two the
// Swift app has keep its words; everywhere else they say what this platform
// is, because "iTerm2 is not running" is not the answer on a machine that can
// never have it.
func (p childPlan) failure(goos string) string {
	mac := goos == "darwin"
	switch {
	case p.kind == projects.PlanNoTmux && mac:
		return "tmux is the terminal for new sessions in Settings, and there is no tmux on this machine."
	case p.kind == projects.PlanNoTmux:
		return "tmux is the terminal for new sessions in Settings, and tmux is not installed."
	case p.choice == projects.TerminalITerm && mac:
		return "iTerm2 is not running, and this will not launch it for you."
	case p.choice == projects.TerminalITerm:
		return "iTerm2 is the terminal for new sessions in Settings, and " + goos + " has no iTerm2; set it to tmux."
	}
	// auto, with neither iTerm2 open nor a tmux server running.
	var tmux string
	switch p.reach {
	case projects.TmuxAbsent:
		tmux = "tmux is not installed"
	default:
		tmux = "no tmux server is running, and the auto terminal setting opens a child only into a running one " +
			"(start one, or set the terminal to tmux)"
	}
	if mac {
		return "iTerm2 is not running, and this will not launch it for you; " + tmux + "."
	}
	return goos + " has no iTerm2, and " + tmux + "."
}

// ChildCapabilities is open_child, read_screen and send_keys on this machine:
// the first from the plan a spawn would take now, the other two from this
// machine's terminals. It opens, types and lists nothing.
func (b *Broker) ChildCapabilities(ctx context.Context) ports.Capabilities {
	out := ports.Capabilities{b.planChild(ctx).capability(runtime.GOOS)}
	var terminals ports.Capabilities
	if b.TerminalCapabilities != nil {
		terminals = b.TerminalCapabilities(ctx)
	}
	for _, name := range childNeeds[1:] {
		c, ok := terminals.Find(name)
		if !ok {
			c = ports.Capability{Name: name, State: ports.CapabilityUnknown,
				Reason: "this broker was given no account of its terminals"}
		}
		out = append(out, c)
	}
	return out
}

// checkChildCapability refuses a dispatch this machine cannot carry.
//
// A capability the machine has only through a backend the child will not open
// in is missing for this child: read_screen through iTerm2 reads nothing of a
// tmux pane.
func (b *Broker) checkChildCapability(ctx context.Context) error {
	caps := b.ChildCapabilities(ctx)
	missing := caps.Missing(childNeeds...)
	if open, ok := caps.Find(ports.CapOpenChild); ok && open.State == ports.CapabilityAvailable && len(open.Via) == 1 {
		backend := open.Via[0]
		for _, name := range childNeeds[1:] {
			c, ok := caps.Find(name)
			if !ok || c.State != ports.CapabilityAvailable || contains(c.Via, backend) {
				continue
			}
			missing = append(missing, ports.Capability{Name: name, State: ports.CapabilityUnavailable, Via: c.Via,
				Reason: "a child would open in " + backend + ", and this machine has it only " + c.Reason})
		}
	}
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, 0, len(missing))
	clauses := make([]string, 0, len(missing))
	for _, c := range missing {
		names = append(names, string(c.Name))
		clauses = append(clauses, string(c.Name)+" is unavailable: "+strings.TrimSuffix(c.Reason, "."))
	}
	return refuseWith(http.StatusConflict, "no_child_capability",
		"This machine cannot open a child session — "+strings.Join(clauses, "; ")+
			". Nothing was recorded, made or opened.",
		map[string]any{
			"missing":      names,
			"capabilities": CapabilityRows(caps),
			"platform":     runtime.GOOS,
		})
}

// CapabilityRow is one capability as a refusal and /v1/diagnostics carry it.
type CapabilityRow struct {
	Name   string   `json:"name"`
	State  string   `json:"state"`
	Via    []string `json:"via,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// CapabilityRows is caps as rows.
func CapabilityRows(caps ports.Capabilities) []CapabilityRow {
	out := make([]CapabilityRow, 0, len(caps))
	for _, c := range caps {
		out = append(out, CapabilityRow{Name: string(c.Name), State: string(c.State), Via: c.Via, Reason: c.Reason})
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
