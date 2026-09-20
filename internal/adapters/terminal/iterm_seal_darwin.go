//go:build darwin

package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// A window iTerm2 will not describe is not a reason to stop answering for the
// whole machine — but only something positive may say so.
//
// **What is refused here is the obvious fix.** "That window has been unreadable
// for two hours, treat it as empty" is D05 ③ with a timer in front of it: it
// answers "is anything in there" with "probably not by now", which is the same
// guess made more slowly, and it gets *more* confident exactly as the evidence
// gets older. It would also be wrong in the one case that matters — a window
// that starts answering again with a live session in it — and nothing would
// notice, because the clock would already have said no.
//
// So the seal rests on a second reading instead, taken in the same moment and
// by another mechanism:
//
//	An iTerm2 session is a pseudo-terminal. iTerm2 makes one by forking a
//	process under its own application or under its session-restoration server
//	(iTermServer-<version>, which is what keeps sessions across a restart), and
//	that fork is in the process table with the pty as its controlling terminal.
//	So the process table can enumerate the ptys iTerm2 owns without asking
//	iTerm2 anything. When every one of them is already on this listing, the
//	windows the listing could not read hold no session with a process of its
//	own — which is every session this adapter publishes, because a row with no
//	tty is dropped above as a tmux mirror.
//
// It is evidence rather than a guess because it is falsifiable in the same
// beat: the moment a pty appears that iTerm2 owns and this listing has not
// read, the seal is refused and the reading goes back to incomplete — naming
// the pty. Measured on this Mac on 2026-09-20: the listing read 11 ptys, the
// process table attributed 11 to iTerm2, the sets were equal, and window 27898
// — titled `sleep 300`, with no `sleep 300` anywhere in the process table —
// was accounted for.
//
// What it does not prove is stated where it is written: a session whose own
// program has already exited holds no pty, so a husk inside an unreadable
// window is not seen by this either. Nothing in this daemon lists such a
// session or acts on one; every question that rests on this seal — can this id
// be typed into, is this child's tab gone, may this row be closed — is about a
// session with a process.
type itermPTYs struct {
	// ttys are the pseudo-terminals the process table attributes to iTerm2,
	// without the /dev/ prefix.
	ttys map[string]bool
	// hosts is how many iTerm2 processes were found to attribute them to,
	// for the sentence the seal writes.
	hosts int
}

// systemITermPTYs reads the process table once.
//
// A reading that fails seals nothing: false is returned, every gap stays open
// and the whole listing goes on being incomplete. Unknown never seals.
func systemITermPTYs(ctx context.Context) (itermPTYs, bool) {
	// `comm=` and not `command=`: it prints the executable and none of its
	// arguments, so a process whose *arguments* name iTerm2 — this daemon's
	// own `ps` pipeline, a grep somebody typed — cannot be taken for it. It
	// also makes the program the whole of what is left on the line, which is
	// the only way to read a path with a space in it out of a table with no
	// separators: iTerm2's own server lives under `Application Support`.
	cmd := exec.CommandContext(ctx, "/bin/ps", "-Ao", "tty=,pid=,ppid=,comm=")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return itermPTYs{}, false
	}
	return parseITermPTYs(string(out)), true
}

// itermHosts are the two processes that can hold an iTerm2 session's pty. They
// are matched on the path of the program, which is the bundle's own and cannot
// be spelled by the person's home directory or by a process renaming itself.
var itermHosts = []string{
	"/iTerm.app/Contents/MacOS/iTerm2",
	"/iTerm2/iTermServer-",
}

// parseITermPTYs is the table read in two passes: which processes are iTerm2,
// then which pseudo-terminals are held by a child of one.
//
// Only a direct child counts. A session whose own leader has exited and left
// its work reparented to launchd is no longer attributable to iTerm2 here, and
// the ptys it holds then go unaccounted for — which refuses the seal rather
// than granting a wrong one, and is the direction this has to fail in.
func parseITermPTYs(table string) itermPTYs {
	type row struct {
		tty  string
		ppid int
	}
	rows := make([]row, 0, 512)
	hosts := map[int]bool{}
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		rows = append(rows, row{tty: fields[0], ppid: ppid})
		// Everything after the ppid is the program, because `comm=` prints no
		// arguments — which is what makes a path with a space in it readable
		// out of a table that has no separators.
		program := strings.Join(fields[3:], " ")
		for _, host := range itermHosts {
			if strings.Contains(program, host) {
				hosts[pid] = true
			}
		}
	}
	found := itermPTYs{ttys: map[string]bool{}, hosts: len(hosts)}
	for _, r := range rows {
		if r.tty == "" || r.tty == "??" || r.tty == "-" || !hosts[r.ppid] {
			continue
		}
		found.ttys[strings.TrimPrefix(r.tty, "/dev/")] = true
	}
	return found
}

// missing is the members of a that b does not have, in a stable order.
func missing(a, b map[string]bool) []string {
	out := make([]string, 0, 4)
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// sealITermGaps offers every gap in this reading to the process table.
//
// It is all or nothing on purpose. The ptys are attributed to iTerm2 as a
// whole and not to one window — the process table knows nothing of windows —
// so an unaccounted pty could be behind any of the gaps, and sealing some of
// them would be choosing which. Either every window this listing could not
// read is accounted for, or none of them is.
func sealITermGaps(ctx context.Context, inv session.Inventory, read func(context.Context) (itermPTYs, bool)) session.Inventory {
	if read == nil {
		return inv
	}
	for _, g := range inv.Gaps {
		if g.ID == "" {
			// A region the listing could not even name. Nothing can be
			// attributed to it, so nothing seals it.
			inv.Notes = append(inv.Notes,
				"iTerm2 left a region it would not name, so the process table could not account for it")
			return inv
		}
	}
	owned, ok := read(ctx)
	if !ok {
		inv.Notes = append(inv.Notes,
			"the process table could not be read, so nothing accounts for the windows iTerm2 would not list")
		return inv
	}
	listed := map[string]bool{}
	for _, s := range inv.Sessions {
		if s.TTY != "" {
			listed[strings.TrimPrefix(s.TTY, "/dev/")] = true
		}
	}
	if len(listed) == 0 {
		// Nothing to agree with. An iTerm2 that listed no session at all and
		// has a window it cannot read is a reading with no corroboration in
		// it, whatever the process table says.
		inv.Notes = append(inv.Notes,
			"this listing read no session at all, so nothing in it agrees with the process table")
		return inv
	}
	hidden := missing(owned.ttys, listed)
	if len(hidden) > 0 {
		note := fmt.Sprintf("the process table attributes %d pseudo-terminal(s) to iTerm2 that this listing "+
			"did not read (%s), so the window(s) it could not list are hiding %d session(s)",
			len(hidden), strings.Join(hidden, " "), len(hidden))
		inv.Notes = append(inv.Notes, note)
		for n := range inv.Gaps {
			inv.Gaps[n].Detail += "; " + note
		}
		return inv
	}
	// **The two sets have to be equal, not one contained in the other.**
	//
	// Containment alone is satisfied by a measurement that found nothing: an
	// empty set is inside every set, so an attribution that has quietly stopped
	// working — iTerm2 changing how it launches a session, a macOS that stops
	// answering `ps` for another process — would seal every window on the
	// machine and call it evidence. That is the exact mistake this file exists
	// to refuse, made by the code doing the refusing. It happened here on the
	// first run: the server's path contains a space, the program was read as
	// `/Users/x/Library/Application`, nothing was attributed to iTerm2, and the
	// unreadable window was declared empty on the strength of zero ptys.
	//
	// Equality is the attribution proving itself on this very reading: every
	// pty the listing read was attributed to iTerm2 by a mechanism that never
	// asked iTerm2, and every pty that mechanism found was on the listing. A
	// session in a readable window whose own program has exited breaks it and
	// refuses the seal, which is the right direction to fail in.
	if unattributed := missing(listed, owned.ttys); len(unattributed) > 0 || owned.hosts == 0 {
		inv.Notes = append(inv.Notes, fmt.Sprintf("the process table attributed %d of this listing's %d "+
			"pseudo-terminal(s) to iTerm2 (%d iTerm2 process(es)), so it is not reading this machine well "+
			"enough to account for the window(s) that would not list",
			len(listed)-len(unattributed), len(listed), owned.hosts))
		return inv
	}
	sealed := fmt.Sprintf("the process table attributes %d pseudo-terminal(s) to iTerm2 (%d iTerm2 process(es)) "+
		"and this listing read exactly those, so the window(s) it could not read hold no session with a "+
		"process of its own",
		len(owned.ttys), owned.hosts)
	for n := range inv.Gaps {
		inv.Gaps[n].Sealed = true
		inv.Gaps[n].SealedBy = sealed
	}
	// The listing is now all of iTerm2 that this adapter publishes, so it says
	// so — and keeps the gap beside the answer, because a person looking at
	// this machine still has a window that will not open.
	inv.Complete = true
	inv.Notes = append(inv.Notes, sealed)
	return inv
}
