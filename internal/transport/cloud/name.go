package cloud

// What this machine is called, in one place.
//
// The name goes on the account's machine list, which is the one screen where a
// person tells their machines apart — on a phone, away from all of them. So a
// name that is wrong is not a wording problem: it is the wrong row.
//
// **There were two ladders and they disagreed.** `clawdline cloud login` chose
// the name it registered with (flag, settings, host name, and a last resort);
// the publisher chose the name it publishes every heartbeat (settings,
// registered name, and a last resort). The two last resorts were different
// words, and the publisher's was `"Mac"` — so a Linux machine that had never
// been named registered as its host name and then published itself as a Mac,
// beside a `platform` field saying `linux`. One question, one ladder.
//
// **The last rung says there is no name.** Inventing one is what produced the
// Mac: a made-up name cannot be told from a chosen one, and two unnamed
// machines inventing the same word are two rows a person cannot tell apart at
// all. Naming the platform instead is the most this machine honestly knows,
// and `runtime.GOOS` always answers it.

import (
	"os"
	"strings"
)

// MachineName is what a person picks this machine out by in their machine
// list: the first of `given` that says anything, then this machine's host
// name, and finally a word that says it has no name.
//
// `given` is in the caller's own order of precedence — the publisher trusts
// the settings override over the registered name, and login trusts the `-name`
// flag over the settings — because the two are asking the same question from
// different distances, not asking different questions.
func MachineName(host, goos string, given ...string) string {
	for _, name := range given {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			return trimmed
		}
	}
	if name := hostAsName(host); name != "" {
		return name
	}
	return unnamedMachine(goos)
}

// HostName is this machine's host name, or "" when the system will not say.
// An unreadable host name is an absence, never an error to report: nothing a
// person does about it would change the name they see.
func HostName() string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return host
}

// hostAsName is a host name as a machine list should show it, or "" when it
// would not tell one machine from another.
//
// The mDNS `.local` suffix comes off because it is on every machine on the
// network and names none of them; `localhost` and its variants come off for
// the same reason, and they are what a machine with no configured name
// answers. Answering "" here is what sends the ladder on to its last rung.
func hostAsName(host string) string {
	name := strings.TrimSpace(host)
	name = strings.TrimSuffix(name, ".") // a fully-qualified name ends at the root
	name = strings.TrimSuffix(name, ".local")
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "", "localhost", "localhost.localdomain", "localhost.local":
		return ""
	}
	return name
}

// unnamedMachine is what a machine with no name of any kind publishes.
//
// It is not a guess at a name. It says there is none, and it names the kind of
// machine so the row is at least distinguishable from one of another kind —
// which is exactly what `"Mac"` failed to do, because it said the one thing
// the machine might not be. A `runtime.GOOS` this build has no word for gets
// no word invented for it: unknown is not macOS.
func unnamedMachine(goos string) string {
	switch goos {
	case "darwin":
		return "Unnamed Mac"
	case "linux":
		return "Unnamed Linux machine"
	case "windows":
		return "Unnamed Windows machine"
	}
	return "Unnamed machine"
}
