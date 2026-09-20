//go:build darwin

package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// One real block of this Mac's process table, as
// `/bin/ps -Ao tty=,pid=,ppid=,comm=` printed it on 2026-09-20, with the
// person's home directory replaced and the rows that are not the point cut.
//
// 741 is iTerm2's session-restoration server and 92110 is the application; the
// processes under them hold the ptys the listing reads. `comm=` prints the
// executable and none of its arguments, which is why the server's path — with
// a space in `Application Support` — is the whole of what follows the ppid,
// and why nothing here can be spoofed by a command line that mentions iTerm2.
//
// The last four rows are the traps: two programs from iTerm2's own bundle that
// do not host sessions, a shell reparented away from iTerm2, and a pty
// somebody else owns.
const itermTable = `??        741     1 /Users/x/Library/Application Support/iTerm2/iTermServer-3.6.10
??      92110     1 /Applications/iTerm.app/Contents/MacOS/iTerm2
ttys001 65844   741 /usr/bin/login
ttys002 11304   741 /usr/bin/login
ttys005 67272   741 /usr/bin/login
ttys099 40119 92110 /bin/zsh
??      27581     1 /Applications/iTerm.app/Contents/XPCServices/iTerm2SandboxedWorker.xpc/Contents/MacOS/iTerm2SandboxedWorker
??      92288     1 /Applications/iTerm.app/Contents/XPCServices/pidinfo.xpc/Contents/MacOS/pidinfo
ttys015 74269     1 /bin/zsh
ttys070 31337   999 /usr/bin/script
`

// What the process table says iTerm2 owns, and what it deliberately does not.
func TestTheProcessTableNamesITerm2sOwnPseudoTerminals(t *testing.T) {
	found := parseITermPTYs(itermTable)
	for _, want := range []string{"ttys001", "ttys002", "ttys005", "ttys099"} {
		if !found.ttys[want] {
			t.Errorf("%s is iTerm2's and was not attributed to it: %v", want, found.ttys)
		}
	}
	for _, not := range []string{
		"ttys015", // reparented to launchd, so no longer attributable
		"ttys070", // somebody else's pty
		"??",      // no controlling terminal at all
	} {
		if found.ttys[not] {
			t.Errorf("%s was attributed to iTerm2 and is not its: %v", not, found.ttys)
		}
	}
	// The bundle's other programs are in the table and are not session hosts.
	if found.hosts != 2 {
		t.Errorf("iTerm2 processes: %d, want 2 (the app and its server)", found.hosts)
	}
}

// blindInventory is a listing with one window it could not read.
func blindInventory(ttys ...string) session.Inventory {
	inv := session.Inventory{Provenance: "iterm", Complete: false,
		Gaps: []session.Gap{{Source: "iterm", Scope: "window", ID: "27898",
			Detail: "iTerm2 window 27898 would not list its tabs (tabs() answered null)"}}}
	for _, tty := range ttys {
		inv.Sessions = append(inv.Sessions, session.Session{ID: "S" + tty, TTY: tty, Backend: session.BackendITerm})
	}
	return inv
}

func ptys(ttys ...string) func(context.Context) (itermPTYs, bool) {
	set := map[string]bool{}
	for _, tty := range ttys {
		set[tty] = true
	}
	return func(context.Context) (itermPTYs, bool) { return itermPTYs{ttys: set, hosts: 2}, true }
}

// A window that is hiding no pseudo-terminal is hiding no session this adapter
// would publish, and that is a fact the process table states rather than a
// length of time anybody waited out.
func TestAWindowHidingNoPseudoTerminalStopsCostingTheListingItsAnswer(t *testing.T) {
	inv := sealITermGaps(context.Background(), blindInventory("ttys001", "ttys002"), ptys("ttys001", "ttys002"))
	if !inv.Complete {
		t.Errorf("the listing is still incomplete: %v", inv.Notes)
	}
	if !inv.Gaps[0].Sealed || !strings.Contains(inv.Gaps[0].SealedBy, "2 pseudo-terminal") {
		t.Errorf("the seal does not carry its own numbers: %+v", inv.Gaps[0])
	}
	// The window is still there and still says so: a sealed gap is an answer
	// about what it can hide, not a repair of the person's iTerm2.
	if inv.Gaps[0].ID != "27898" {
		t.Errorf("the window stopped being reported once it was sealed: %+v", inv.Gaps)
	}
}

// One pty iTerm2 owns and the listing did not read is the whole seal refused,
// and the reading says which pty it was.
func TestAPseudoTerminalTheListingMissedRefusesTheSeal(t *testing.T) {
	inv := sealITermGaps(context.Background(), blindInventory("ttys001"), ptys("ttys001", "ttys077"))
	if inv.Complete || inv.Gaps[0].Sealed {
		t.Fatalf("a listing that missed a pty sealed anyway: %+v", inv)
	}
	if !strings.Contains(strings.Join(inv.Notes, " "), "ttys077") ||
		!strings.Contains(inv.Gaps[0].Detail, "ttys077") {
		t.Errorf("the hidden pty is not named: %v / %q", inv.Notes, inv.Gaps[0].Detail)
	}
}

// An attribution that found nothing must seal nothing.
//
// This one went red on its way in, on the real machine and not in a fixture.
// The first version of the seal asked only whether every pty it attributed to
// iTerm2 was on the listing — and an empty set is inside every set, so when the
// attribution silently failed (the server's path has a space in it, the program
// was read as `/Users/x/Library/Application`, nothing matched) it sealed the
// window on the strength of zero ptys and wrote "attributes 0 pseudo-terminal(s)
// to iTerm2 and this listing read every one of them". That is the mistake the
// whole file exists to refuse, made by the code doing the refusing: an empty
// measurement standing in for evidence.
//
// So the sets must be equal. The attribution has to account for the ptys the
// listing *did* read before it is believed about the ones it did not.
func TestAnAttributionThatAccountsForNothingSealsNothing(t *testing.T) {
	blind := func(context.Context) (itermPTYs, bool) { return itermPTYs{ttys: map[string]bool{}, hosts: 0}, true }
	inv := sealITermGaps(context.Background(), blindInventory("ttys001", "ttys002"), blind)
	if inv.Complete || inv.Gaps[0].Sealed {
		t.Fatalf("zero attributed pseudo-terminals sealed a window: %+v", inv)
	}
	if !strings.Contains(strings.Join(inv.Notes, " "), "0 of this listing's 2") {
		t.Errorf("the reading does not say the attribution came up short: %v", inv.Notes)
	}
	// Half an answer is no better: a listing the process table only partly
	// accounts for is a process table that is not reading this machine.
	half := func(context.Context) (itermPTYs, bool) {
		return itermPTYs{ttys: map[string]bool{"ttys001": true}, hosts: 2}, true
	}
	if inv := sealITermGaps(context.Background(), blindInventory("ttys001", "ttys002"), half); inv.Complete {
		t.Errorf("a partial attribution sealed a window: %v", inv.Notes)
	}
	// And a listing with nothing in it agrees with nothing.
	empty := blindInventory()
	if inv := sealITermGaps(context.Background(), empty, ptys("ttys001")); inv.Complete {
		t.Errorf("a listing that read no session at all sealed a window: %v", inv.Notes)
	}
}

// Unknown never seals: a process table that could not be read accounts for
// nothing, and neither does a region the listing could not name.
func TestNothingSealsOnASourceThatDidNotAnswer(t *testing.T) {
	unread := func(context.Context) (itermPTYs, bool) { return itermPTYs{}, false }
	inv := sealITermGaps(context.Background(), blindInventory("ttys001"), unread)
	if inv.Complete || inv.Gaps[0].Sealed {
		t.Errorf("an unreadable process table sealed a window: %+v", inv)
	}
	if inv = sealITermGaps(context.Background(), blindInventory("ttys001"), nil); inv.Complete {
		t.Errorf("a machine with no process reader at all sealed a window: %+v", inv)
	}

	nameless := blindInventory("ttys001")
	nameless.Gaps = append(nameless.Gaps, session.Gap{Source: "iterm", Scope: "listing",
		Detail: "iTerm2 left 1 window(s) or tab(s) unread and would not name them"})
	inv = sealITermGaps(context.Background(), nameless, ptys("ttys001"))
	if inv.Complete || inv.Gaps[0].Sealed {
		t.Errorf("a region nothing could name was sealed anyway: %+v", inv)
	}
}

// The script's answer becomes named gaps, and a count it could not attribute
// to any window becomes a gap of its own that nothing can seal.
func TestAnUnreadableWindowReachesTheReadingWithItsIDOnIt(t *testing.T) {
	gaps := itermGaps([]itermGap{{Window: "27898", Why: "tabs() answered null", Regions: 1}}, 1)
	if len(gaps) != 1 || gaps[0].ID != "27898" || gaps[0].Source != "iterm" || gaps[0].Scope != "window" {
		t.Fatalf("got %+v", gaps)
	}
	if !strings.Contains(gaps[0].Detail, "27898") {
		t.Errorf("the detail does not name the window: %q", gaps[0].Detail)
	}
	many := itermGaps([]itermGap{{Window: "27898", Why: "a tab's sessions() answered null", Regions: 3}}, 5)
	if len(many) != 2 {
		t.Fatalf("a count with two windows' worth of regions in it: %+v", many)
	}
	if !strings.Contains(many[0].Detail, "3 of its regions") || many[1].ID != "" {
		t.Errorf("got %+v", many)
	}
}

// The listing the adapter takes from this machine, with nothing changed on it.
//
// It is skipped where iTerm2 is not running, because the point of it is the
// real Apple Event: the walk this repository ships must go on returning the
// gaps in the shape the seal reads, and a mock of osascript would prove only
// that the mock agrees with itself.
func TestTheRealListingCarriesWhateverItCouldNotRead(t *testing.T) {
	it := NewITerm()
	inv, err := it.Inventory(context.Background())
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if strings.Contains(strings.Join(inv.Notes, " "), "iTerm2 is not running") {
		t.Skip("iTerm2 is not running on this machine")
	}
	for _, g := range inv.Gaps {
		if g.Source != "iterm" {
			t.Errorf("a gap that is not iTerm2's: %+v", g)
		}
		if g.Sealed && g.SealedBy == "" {
			t.Errorf("a sealed gap with no reason on it: %+v", g)
		}
		t.Logf("gap: id=%q sealed=%v detail=%q sealedBy=%q", g.ID, g.Sealed, g.Detail, g.SealedBy)
	}
	if len(inv.Gaps) == 0 && !inv.Complete {
		t.Errorf("an incomplete listing that named nothing it could not read: %v", inv.Notes)
	}
	var unsealed bool
	for _, g := range inv.Gaps {
		unsealed = unsealed || g.Open()
	}
	if inv.Complete == unsealed && len(inv.Gaps) > 0 {
		t.Errorf("complete=%v with open gaps=%v", inv.Complete, unsealed)
	}
	_ = errors.New // keep the import honest if the skip above is taken
}
