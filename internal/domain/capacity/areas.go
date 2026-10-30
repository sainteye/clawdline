package capacity

import "strings"

// area is one register_<area>.go file: the name prefixes its rows carry and
// the function that declares them. An area of its own holds four rows or
// more; the smaller prefixes share the file of the area they serve:
//
//   - artifacts: icons, screens, voice
//   - board: decisions, epic, planreview, proposals, run, usage
//   - broker: coordinator, dispatch, handoff, leases, orchestrator, waits
//   - daemon: audit, deploy, devices, diagnostics, log, notification, push,
//     settings, sse
//   - projectfiles: projects
//   - session: naming, schedule, snippets
//   - squad: personas, workflow
//
// internal/transport/http measures each area in the capacity_measures_<area>.go
// of the same name.
type area struct {
	name     string
	prefixes []string
	bounds   func() []Entry
}

var areas = []area{
	{"antigravity", []string{"antigravity"}, antigravityBounds},
	{"artifacts", []string{"artifacts", "icons", "screens", "voice"}, artifactsBounds},
	{"board", []string{"board", "decisions", "epic", "planreview", "proposals", "run", "usage"}, boardBounds},
	{"broker", []string{"coordinator", "dispatch", "handoff", "leases", "orchestrator", "waits"}, brokerBounds},
	{"cache", []string{"cache"}, cacheBounds},
	{"callback", []string{"callback"}, callbackBounds},
	{"cloud", []string{"cloud"}, cloudBounds},
	{"console", []string{"console"}, consoleBounds},
	{"daemon", []string{"audit", "deploy", "devices", "diagnostics", "log", "notification", "push", "settings", "sse"}, daemonBounds},
	{"intent", []string{"intent"}, intentBounds},
	{"iterm", []string{"iterm"}, itermBounds},
	{"memory", []string{"memory"}, memoryBounds},
	{"places", []string{"places"}, placesBounds},
	{"projectfiles", []string{"projectfiles", "projects"}, projectFilesBounds},
	{"projectsync", []string{"projectsync"}, projectSyncBounds},
	{"release", []string{"release"}, releaseBounds},
	{"session", []string{"session", "naming", "schedule", "snippets"}, sessionBounds},
	{"sessions", []string{"sessions"}, sessionsBounds},
	{"squad", []string{"squad", "personas", "workflow"}, squadBounds},
	{"squadpackage", []string{"squadpackage"}, squadPackageBounds},
	{"store", []string{"store"}, storeBounds},
	{"terminal", []string{"terminal"}, terminalBounds},
	{"update", []string{"update"}, updateBounds},
	{"work", []string{"work"}, workBounds},
	{"work_gate", []string{"work_gate"}, workGateBounds},
}

// prefix is the part of a row's name that says its area: what comes before
// the first dot, or work_gate for the underscore-delimited gate rows.
func prefix(name string) string {
	if p, _, ok := strings.Cut(name, "."); ok {
		return p
	}
	if strings.HasPrefix(name, "work_gate_") {
		return "work_gate"
	}
	return name
}

// AreaOf is the register area a row's name belongs in, and "" for a name no
// area claims. The area is the file its row is declared in, and the file of
// the same name in internal/transport/http that measures it.
func AreaOf(name string) string {
	p := prefix(name)
	for _, a := range areas {
		for _, q := range a.prefixes {
			if q == p {
				return a.name
			}
		}
	}
	return ""
}

// Areas is the name of every register area, in declaration order.
func Areas() []string {
	out := make([]string, len(areas))
	for i, a := range areas {
		out[i] = a.name
	}
	return out
}
