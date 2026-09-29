package terminal

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestDecideInput(t *testing.T) {
	cases := []struct {
		name                         string
		applied, epoch, inEpoch, seq uint64
		want                         InputDecision
	}{
		{"the first input of a lease", 0, 1, 1, 1, InputApply},
		{"the next input", 4, 1, 1, 5, InputApply},
		{"the same input again", 5, 1, 1, 5, InputDuplicate},
		{"an older input again", 5, 1, 1, 2, InputDuplicate},
		{"one missing before it", 5, 1, 1, 7, InputGap},
		{"far ahead", 0, 3, 3, 1000, InputGap},
		{"an older epoch", 5, 2, 1, 6, InputSuperseded},
		{"an older epoch that would have been a duplicate", 5, 2, 1, 3, InputSuperseded},
		{"an epoch this lease never handed out", 5, 2, 3, 6, InputSuperseded},
		{"seq zero", 0, 1, 1, 0, InputInvalid},
		{"seq zero after inputs", 9, 1, 1, 0, InputInvalid},
		{"superseded outranks invalid", 0, 1, 2, 0, InputSuperseded},
		{"the highest number there is, again", ^uint64(0), 1, 1, ^uint64(0), InputDuplicate},
	}
	for _, c := range cases {
		if got := DecideInput(c.applied, c.epoch, c.inEpoch, c.seq); got != c.want {
			t.Errorf("%s: DecideInput(applied=%d, epoch=%d, in=%d, seq=%d) = %s, want %s",
				c.name, c.applied, c.epoch, c.inEpoch, c.seq, got, c.want)
		}
	}
}

// Two copies of one input decided one after the other, as the caller's
// critical section runs them, type it once.
func TestAnInputSentTwiceIsTypedOnce(t *testing.T) {
	applied, typed := uint64(0), 0
	for range 2 {
		switch DecideInput(applied, 7, 7, 1) {
		case InputApply:
			typed++
			applied = 1
		case InputDuplicate:
		default:
			t.Fatal("the resend was refused rather than answered as a duplicate")
		}
	}
	if typed != 1 {
		t.Fatalf("typed %d times", typed)
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		id := NewID()
		if !id.Valid() {
			t.Fatalf("NewID made an id it does not accept: %s", id)
		}
		name := id.SessionName()
		if !strings.HasPrefix(name, "clt-") || len(name) != 16 || strings.ContainsAny(name[4:], "-_") {
			t.Fatalf("session name %q for %s", name, id)
		}
		if seen[name] {
			t.Fatalf("two ids made the same session name %s", name)
		}
		seen[name] = true
	}
	for _, bad := range []ID{"", "trm_", "trm_*", "trm_x", "clt-0123456789ab", "TRM_" + NewID()[4:], NewID() + " "} {
		if bad.Valid() || bad.SessionName() != "" {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestARefusalCarriesItsCodeThroughWrapping(t *testing.T) {
	err := fmt.Errorf("opening: %w", Refuse(CodeFull, "8 terminals are open"))
	if code, ok := CodeOf(err); !ok || code != CodeFull {
		t.Fatalf("CodeOf = %q %v", code, ok)
	}
	if _, ok := CodeOf(errors.New("plain")); ok {
		t.Fatal("a plain error has a code")
	}
	seen := map[RefusalCode]bool{}
	for _, c := range RefusalCodes {
		if seen[c] {
			t.Errorf("%s twice", c)
		}
		seen[c] = true
	}
	if len(seen) != 18 {
		t.Errorf("%d codes; the list and the constants disagree", len(seen))
	}
}

// The constants the adapter enforces are the register's rows, spelled once.
func TestTheBoundsAreTheRegisteredRows(t *testing.T) {
	for name, want := range map[string]int64{
		capacity.TerminalCount:        MaxTerminals,
		capacity.TerminalInputBytes:   MaxInputBytes,
		capacity.TerminalPasteBytes:   MaxPasteBytes,
		capacity.TerminalHistoryLines: MaxHistoryLines,
	} {
		if got := capacity.Default(name); got != want {
			t.Errorf("%s: register says %d, the code enforces %d", name, got, want)
		}
	}
}
