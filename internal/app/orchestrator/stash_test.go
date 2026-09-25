package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"
)

// The screens stash.go reads, as Claude Code 2.1.282 drew them in a throwaway
// tmux session on 2026-09-25: the composer holding a suggestion nobody typed
// (identical in plain text to a draft), the composer right after ctrl+s moved a
// draft into the stash, and the stash marker sharing its row with another hint.
const (
	suggestedComposer = `  ✻ Baked for 3m 12s

────────────────────────────────────────────────────────────────────────────────
❯ run the tests again and fix what fails
────────────────────────────────────────────────────────────────────────────────
  ▀▀▀▀▀▀▀▀ clawdline  Terminal
  ⏵⏵ auto mode on (shift+tab to cycle)`

	stashedComposer = `  ✻ Baked for 3m 12s
                                                                   › stashed
────────────────────────────────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────────────────────────────────
  ▀▀▀▀▀▀▀▀ clawdline  Terminal
  ⏵⏵ auto mode on (shift+tab to cycle)`

	stashedBesideHint = `  ✻ Baked for 3m 12s
                                       Ctrl+Y to paste deleted text · › stashed
────────────────────────────────────────────────────────────────────────────────
❯ so about the landing, I think we should
────────────────────────────────────────────────────────────────────────────────
  ▀▀▀▀▀▀▀▀ clawdline  Terminal`
)

func TestTheStashMarkerIsReadWhereClaudeCodeDrawsIt(t *testing.T) {
	for name, c := range map[string]struct {
		screen string
		want   bool
	}{
		"alone on its row":        {stashedComposer, true},
		"beside another hint":     {stashedBesideHint, true},
		"no stash":                {suggestedComposer, false},
		"a draft":                 {draftedComposer, false},
		"said in the transcript":  {"  I pressed › stashed earlier\n\n\n\n\n" + draftedComposer, false},
		"no composer on the page": {"› stashed\nLoading…", false},
	} {
		if got := StashShown(c.screen); got != c.want {
			t.Errorf("%s: StashShown = %v, want %v", name, got, c.want)
		}
	}
}

func TestAKeybindingOfThePersonsOwnTurnsTheStashOff(t *testing.T) {
	for data, want := range map[string]bool{
		``: false,
		`{"bindings":[{"context":"Chat","bindings":{"ctrl+g":"chat:externalEditor"}}]}`: false,
		`{"bindings":[{"context":"Chat","bindings":{"ctrl+s":"chat:submit"}}]}`:         true,
		`{"bindings":[{"context":"Chat","bindings":{"ctrl+t":"chat:stash"}}]}`:          true,
		`{"bindings":[{"context":"Chat","bindings":{"Ctrl+S":null}}]}`:                  true,
	} {
		if got := StashReboundIn([]byte(data)); got != want {
			t.Errorf("%s: %v, want %v", data, got, want)
		}
	}
}

// claudeComposer is a fake Claude Code input line with the stash rules
// stash.go measured: ctrl+s moves a value into the stash, does nothing to an
// empty line, pops a held stash into an empty line, and replaces a held stash.
// shown is text Claude Code drew there that is not the value (a suggestion).
type claudeComposer struct {
	mu      sync.Mutex
	value   string
	shown   string
	stash   *string
	pressed [][]byte
	typed   []string
	// lost is every draft a keystroke destroyed.
	lost []string
}

func (c *claudeComposer) screen() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	line := c.value
	if line == "" {
		line = c.shown
	}
	marker := ""
	if c.stash != nil {
		marker = "                                                                   › stashed"
	}
	return "  ✻ Baked for 1s\n" + marker + "\n" +
		"────────────────────────────────────────────────────────────────────────────────\n" +
		"❯ " + line + "\n" +
		"────────────────────────────────────────────────────────────────────────────────\n" +
		"  ▀▀▀▀▀▀▀▀ clawdline"
}

func (c *claudeComposer) press(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pressed = append(c.pressed, b)
	if len(b) != 1 || b[0] != 0x13 {
		return nil
	}
	switch {
	case c.value == "" && c.stash != nil:
		c.value, c.stash = *c.stash, nil
	case c.value != "":
		if c.stash != nil {
			c.lost = append(c.lost, *c.stash)
		}
		v := c.value
		c.stash, c.value = &v, ""
	}
	return nil
}

// submit is a paste and a Return: appended to whatever value is there, and the
// stash put back afterwards, as Claude Code does.
func (c *claudeComposer) submit(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sent := c.value + text
	if c.value != "" {
		c.lost = append(c.lost, c.value)
	}
	c.typed = append(c.typed, sent)
	c.value, c.shown = "", ""
	if c.stash != nil {
		c.value, c.stash = *c.stash, nil
	}
}

// rigStash gives a broker a root whose composer is c, typed at through the
// prepared path with the lane's order: prepare, then the paste and Return.
func rigStash(t *testing.T, c *claudeComposer) (*Broker, context.Context, *time.Time) {
	t.Helper()
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 25, 1, 26, 47, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Screen = func(context.Context, string) (string, bool) { return c.screen(), true }
	b.Type = func(_ context.Context, _, text string) error { c.submit(text); return nil }
	b.TypePrepared = func(ctx context.Context, _, text string, prepare Prepare) error {
		if err := prepare(ctx, c.press, func() (string, bool) { return c.screen(), true }); err != nil {
			return err
		}
		c.submit(text)
		return nil
	}
	return b, ctx, &now
}

// The bug as it happened. The root's composer showed a suggestion Claude Code
// drew there, the broker read it as a person's draft, and a child that had
// finished at 01:26 was never announced. It goes on the second look now, alone,
// and nothing was put in the stash, because there was nothing to put there.
func TestASuggestionInTheComposerNoLongerHoldsTheNotice(t *testing.T) {
	c := &claudeComposer{shown: "run the tests again and fix what fails"}
	b, ctx, now := rigStash(t, c)
	r := finished(t, b, ctx, "5a5e0000-0000-4000-8000-000000000001")

	b.PumpNotices(ctx)
	if len(c.typed) != 0 {
		t.Fatalf("typed on the first look, before the text was seen standing still: %q", c.typed)
	}
	*now = now.Add(noticeHoldStep)
	b.PumpNotices(ctx)
	if len(c.typed) != 1 || c.typed[0][:len("<clawdline-notice>")] != "<clawdline-notice>" {
		t.Fatalf("after the second look: typed %q", c.typed)
	}
	if c.stash != nil || len(c.lost) != 0 {
		t.Fatalf("a suggestion left a stash %v or lost %q", c.stash, c.lost)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDelivered || after.Notice.Attempts != 1 || after.Notice.LastError != nil {
		t.Fatalf("after delivery: %+v", after.Notice)
	}
}

// A person's real draft: stashed, the notice submitted alone, and the draft back
// in the composer afterwards — by Claude Code, not by anything typed here.
func TestADraftIsStashedAroundTheNoticeAndComesBack(t *testing.T) {
	const draft = "so about the landing, I think we should"
	c := &claudeComposer{value: draft}
	b, ctx, now := rigStash(t, c)
	r := finished(t, b, ctx, "5a5e0000-0000-4000-8000-000000000002")

	b.PumpNotices(ctx)
	*now = now.Add(noticeHoldStep)
	b.PumpNotices(ctx)
	if len(c.typed) != 1 || c.typed[0][:len("<clawdline-notice>")] != "<clawdline-notice>" {
		t.Fatalf("the notice was not sent alone: %q", c.typed)
	}
	if c.value != draft || c.stash != nil || len(c.lost) != 0 {
		t.Fatalf("the draft did not come back: value %q stash %v lost %q", c.value, c.stash, c.lost)
	}
	if after, _, _ := b.Record(ctx, r.ID); after.Notice.State != NoticeDelivered {
		t.Fatalf("after delivery: %+v", after.Notice)
	}
}

// The one keystroke that would cost somebody a draft: a stash already held.
// Nothing is pressed, and the notice is held as it always was.
func TestAHeldStashIsNeverPressedOver(t *testing.T) {
	older := "the draft stashed an hour ago"
	c := &claudeComposer{value: "a newer draft", stash: &older}
	b, ctx, now := rigStash(t, c)
	r := finished(t, b, ctx, "5a5e0000-0000-4000-8000-000000000003")

	for i := 0; i < 4; i++ {
		b.PumpNotices(ctx)
		*now = now.Add(noticeHoldStep)
	}
	if len(c.pressed) != 0 || len(c.typed) != 0 || len(c.lost) != 0 {
		t.Fatalf("pressed %v, typed %q, lost %q over a held stash", c.pressed, c.typed, c.lost)
	}
	if after, _, _ := b.Record(ctx, r.ID); after.Notice.LastError == nil ||
		after.Notice.LastError.Code != holdComposer.Code {
		t.Fatalf("the hold does not say why: %+v", after.Notice)
	}
}

// Text that changes between two looks is somebody typing; a stash now would take
// half a sentence and leave the rest to land after the notice.
func TestADraftStillBeingWrittenIsNotStashed(t *testing.T) {
	c := &claudeComposer{value: "so about"}
	b, ctx, now := rigStash(t, c)
	finished(t, b, ctx, "5a5e0000-0000-4000-8000-000000000004")

	for _, more := range []string{" the", " landing", ", I", " think"} {
		b.PumpNotices(ctx)
		c.mu.Lock()
		c.value += more
		c.mu.Unlock()
		*now = now.Add(noticeHoldStep)
	}
	if len(c.pressed) != 0 || len(c.typed) != 0 {
		t.Fatalf("pressed %v and typed %q while the draft was moving", c.pressed, c.typed)
	}
}

// Codex has no stash, and a rebound ctrl+s is not the keystroke measured: both
// hold exactly as before stash.go.
func TestNoStashWhereTheKeystrokeMeansSomethingElse(t *testing.T) {
	c := &claudeComposer{value: "so about the landing"}
	b, ctx, now := rigStash(t, c)
	b.StashRebound = func() bool { return true }
	finished(t, b, ctx, "5a5e0000-0000-4000-8000-000000000005")
	for i := 0; i < 3; i++ {
		b.PumpNotices(ctx)
		*now = now.Add(noticeHoldStep)
	}
	if len(c.pressed) != 0 || len(c.typed) != 0 {
		t.Fatalf("rebound ctrl+s: pressed %v, typed %q", c.pressed, c.typed)
	}
	if b.mayStash("codex") {
		t.Fatal("a Codex root would be sent Claude Code's stash key")
	}
}
