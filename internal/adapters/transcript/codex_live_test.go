package transcript

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAManagedCodexTitleMustBeAnExactUniqueMatch(t *testing.T) {
	one := codexLiveIdentity{ID: "c0de0001-0000-4000-8000-000000000001", Name: "Inspect the queue", CWD: "/code/demo"}
	h := &Host{codexLive: map[string]codexLiveIdentity{one.ID: one}}
	ctx := context.Background()
	base := session.Session{Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "Inspect the queue | demo (codex)"}

	if got, ok := codexLiveFor(ctx, h.codexLive, base); !ok || got.ID != one.ID {
		t.Fatalf("exact match = %+v, %v", got, ok)
	}
	working := base
	working.Label = "⠸ " + working.Label
	if got, ok := codexLiveFor(ctx, h.codexLive, working); !ok || got.ID != one.ID {
		t.Fatalf("working title = %+v, %v", got, ok)
	}
	wrongTitle := base
	wrongTitle.Label = "Another title | demo (codex)"
	if _, ok := codexLiveFor(ctx, h.codexLive, wrongTitle); ok {
		t.Fatal("a different terminal title bound the conversation")
	}
	wrongDirectory := base
	wrongDirectory.CWD = "/code/other"
	if _, ok := codexLiveFor(ctx, h.codexLive, wrongDirectory); ok {
		t.Fatal("a different process cwd bound the conversation")
	}
	tmux := base
	tmux.Backend = session.BackendTmux
	if _, ok := codexLiveFor(ctx, h.codexLive, tmux); ok {
		t.Fatal("the iTerm-only title contract bound a tmux row")
	}

	two := codexLiveIdentity{ID: "c0de0002-0000-4000-8000-000000000002", Name: one.Name, CWD: one.CWD}
	h.codexLive[two.ID] = two
	if _, ok := codexLiveFor(ctx, h.codexLive, base); ok {
		t.Fatal("two exact live matches were ranked instead of left unbound")
	}
}

// Codex 0.160 leaves a thread unnamed until something names it, and the
// terminal title is then only "<base(cwd)> (codex)". That title is accepted
// only for an unnamed live root that is alone in its directory.
func TestAnUnnamedManagedCodexThreadBindsOnlyWhenAloneInItsDirectory(t *testing.T) {
	unnamed := codexLiveIdentity{ID: "c0de0003-0000-4000-8000-000000000003", CWD: "/code/demo"}
	h := &Host{codexLive: map[string]codexLiveIdentity{unnamed.ID: unnamed}}
	base := session.Session{Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "demo (codex)"}
	ctx := h.ObserveRows(context.Background(), []session.Session{base})

	if got, ok := codexLiveFor(ctx, h.codexLive, base); !ok || got.ID != unnamed.ID {
		t.Fatalf("unnamed match = %+v, %v", got, ok)
	}
	working := base
	working.Label = "⠸ demo (codex)"
	if got, ok := codexLiveFor(ctx, h.codexLive, working); !ok || got.ID != unnamed.ID {
		t.Fatalf("working unnamed title = %+v, %v", got, ok)
	}
	piped := base
	piped.Label = " | demo (codex)"
	if _, ok := codexLiveFor(ctx, h.codexLive, piped); ok {
		t.Fatal("an empty name segment was accepted as the unnamed title")
	}

	// A named thread in the same directory neither takes the unnamed title
	// nor lets the unnamed thread take its own.
	named := codexLiveIdentity{ID: "c0de0004-0000-4000-8000-000000000004", Name: "Inspect the queue", CWD: "/code/demo"}
	h.codexLive[named.ID] = named
	if got, ok := codexLiveFor(ctx, h.codexLive, base); !ok || got.ID != unnamed.ID {
		t.Fatalf("unnamed title beside a named peer = %+v, %v", got, ok)
	}
	namedTitle := base
	namedTitle.Label = "Inspect the queue | demo (codex)"
	if got, ok := codexLiveFor(ctx, h.codexLive, namedTitle); !ok || got.ID != named.ID {
		t.Fatalf("named title beside an unnamed peer = %+v, %v", got, ok)
	}

	second := codexLiveIdentity{ID: "c0de0005-0000-4000-8000-000000000005", CWD: "/code/demo"}
	h.codexLive[second.ID] = second
	if got, ok := codexLiveFor(ctx, h.codexLive, base); ok {
		t.Fatalf("two unnamed threads in one directory bound %+v", got)
	}
	if got, ok := codexLiveFor(ctx, h.codexLive, namedTitle); !ok || got.ID != named.ID {
		t.Fatalf("unnamed peers disturbed the named match: %+v, %v", got, ok)
	}
}

// A named thread is joined only by its full title, never by the bare
// directory title an unnamed thread would show.
func TestANamedManagedCodexThreadIgnoresTheUnnamedTitle(t *testing.T) {
	named := codexLiveIdentity{ID: "c0de0006-0000-4000-8000-000000000006", Name: "Inspect the queue", CWD: "/code/demo"}
	h := &Host{codexLive: map[string]codexLiveIdentity{named.ID: named}}
	if got, ok := codexLiveFor(context.Background(), h.codexLive, session.Session{Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "demo (codex)"}); ok {
		t.Fatalf("the bare directory title bound a named thread: %+v", got)
	}
}

// A second Codex tab opened in the same directory shows the same bare title
// before its own thread holds a writer lock. Both rows then expect the one
// unnamed live root, so neither is given it; with one such row it binds.
func TestTwoBareCodexTabsNeverShareOneUnnamedThread(t *testing.T) {
	unnamed := codexLiveIdentity{ID: "c0de000a-0000-4000-8000-00000000000a", CWD: "/code/demo"}
	h := &Host{codexLive: map[string]codexLiveIdentity{unnamed.ID: unnamed}}
	first := session.Session{ID: "ITERM-1", Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "demo (codex)"}
	second := first
	second.ID = "ITERM-2"
	second.Label = "⠸ demo (codex)"

	ctx := h.ObserveRows(context.Background(), []session.Session{first})
	if got, ok := codexLiveFor(ctx, h.codexLive, first); !ok || got.ID != unnamed.ID {
		t.Fatalf("one bare tab = %+v, %v; want the unnamed thread", got, ok)
	}
	// A reading that observed no rows binds no unnamed thread, rather than
	// trusting a count another reading took.
	if got, ok := codexLiveFor(context.Background(), h.codexLive, first); ok {
		t.Fatalf("a reading with no observed rows bound %+v", got)
	}

	ctx = h.ObserveRows(context.Background(), []session.Session{first, second})
	for _, row := range []session.Session{first, second} {
		if got, ok := codexLiveFor(ctx, h.codexLive, row); ok {
			t.Fatalf("%s bound %+v while another tab shows the same bare title", row.ID, got)
		}
	}

	// A named tab, another directory's tab and a Claude row are not the same
	// bare title, so they do not stop the one bare tab binding.
	named := first
	named.ID, named.Label = "ITERM-3", "Inspect the queue | demo (codex)"
	elsewhere := first
	elsewhere.ID, elsewhere.CWD, elsewhere.Label = "ITERM-4", "/other/demo", "demo (codex)"
	claude := first
	claude.ID, claude.Assistant = "ITERM-5", session.AssistantClaude
	ctx = h.ObserveRows(context.Background(), []session.Session{first, named, elsewhere, claude})
	if got, ok := codexLiveFor(ctx, h.codexLive, first); !ok || got.ID != unnamed.ID {
		t.Fatalf("bare tab beside unrelated rows = %+v, %v", got, ok)
	}
}

// Codex can prefix an iTerm title while it waits for a person. The title
// still names the same conversation; the prefix is not part of its name.
func TestActionRequiredCodexTitleBindsItsOwnLiveConversation(t *testing.T) {
	wanted := codexLiveIdentity{ID: "c0de0011-0000-4000-8000-000000000011", Name: "Inspect the queue", CWD: "/code/demo"}
	other := codexLiveIdentity{ID: "c0de0012-0000-4000-8000-000000000012", Name: "Review the plan", CWD: "/code/demo"}
	lives := map[string]codexLiveIdentity{wanted.ID: wanted, other.ID: other}
	row := session.Session{Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "[ ! ] Action Required | Inspect the queue | demo (codex)"}
	for _, marker := range []string{"[ ! ]", "[ . ]"} {
		row.Label = marker + " Action Required | Inspect the queue | demo (codex)"
		if got, ok := codexLiveFor(context.Background(), lives, row); !ok || got.ID != wanted.ID {
			t.Fatalf("%s action-required title = %+v, %v; want its own conversation", marker, got, ok)
		}
	}
	row.Label = "[ ! ] Action Required | Unknown title | demo (codex)"
	if got, ok := codexLiveFor(context.Background(), lives, row); ok {
		t.Fatalf("unknown title bound %+v", got)
	}
	row.Label = "[ ! ] Action Required | Inspect the queue | other (codex)"
	if got, ok := codexLiveFor(context.Background(), lives, row); ok {
		t.Fatalf("wrong directory title bound %+v", got)
	}

	// A literal conversation name beginning with the same words gets its
	// exact match before the decorated-title fallback is considered.
	literal := codexLiveIdentity{ID: "c0de0013-0000-4000-8000-000000000013", Name: "[ ! ] Action Required | Inspect the queue", CWD: "/code/demo"}
	lives[literal.ID] = literal
	row.Label = "[ ! ] Action Required | Inspect the queue | demo (codex)"
	if got, ok := codexLiveFor(context.Background(), lives, row); !ok || got.ID != literal.ID {
		t.Fatalf("literal name = %+v, %v; want the exact match", got, ok)
	}
	literal.Name = "[ . ] Action Required | Inspect the queue"
	lives[literal.ID] = literal
	row.Label = "[ . ] Action Required | Inspect the queue | demo (codex)"
	if got, ok := codexLiveFor(context.Background(), lives, row); !ok || got.ID != literal.ID {
		t.Fatalf("literal waiting name = %+v, %v; want the exact match", got, ok)
	}
}

func TestActionRequiredBareCodexTitleStillNeedsOneTerminalAndOneThread(t *testing.T) {
	unnamed := codexLiveIdentity{ID: "c0de0014-0000-4000-8000-000000000014", CWD: "/code/demo"}
	h := &Host{codexLive: map[string]codexLiveIdentity{unnamed.ID: unnamed}}
	row := session.Session{ID: "ITERM-1", Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		CWD: "/code/demo", Label: "[ ! ] Action Required | demo (codex)"}
	ctx := h.ObserveRows(context.Background(), []session.Session{row})
	if got, ok := codexLiveFor(ctx, h.codexLive, row); !ok || got.ID != unnamed.ID {
		t.Fatalf("one decorated bare title = %+v, %v", got, ok)
	}
	row.Label = "[ . ] Action Required | demo (codex)"
	ctx = h.ObserveRows(context.Background(), []session.Session{row})
	if got, ok := codexLiveFor(ctx, h.codexLive, row); !ok || got.ID != unnamed.ID {
		t.Fatalf("one waiting bare title = %+v, %v", got, ok)
	}
	second := row
	second.ID, second.Label = "ITERM-2", "demo (codex)"
	ctx = h.ObserveRows(context.Background(), []session.Session{row, second})
	for _, candidate := range []session.Session{row, second} {
		if got, ok := codexLiveFor(ctx, h.codexLive, candidate); ok {
			t.Fatalf("%s bound %+v with another bare tab", candidate.ID, got)
		}
	}
}
