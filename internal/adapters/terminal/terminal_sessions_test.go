package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestTerminalSessionsNeverTargetTheOtherServersSamePane(t *testing.T) {
	mainPane, mainLog, main := privateTmux(t, "")
	termPane, termLog, term := privateTmux(t, "")
	if mainPane.ID != termPane.ID {
		t.Fatal("the collision fixture must have the same pane number on both servers")
	}
	main.Terminal = term
	row := termPane
	row.ID = session.TerminalPanePrefix + row.ID
	ctx := context.Background()
	inv, err := main.Inventory(ctx)
	if err != nil || !inv.Complete || len(inv.Sessions) != 2 {
		t.Fatalf("inventory: %+v %v", inv, err)
	}
	if inv.Sessions[0].ID != mainPane.ID || inv.Sessions[1].ID != row.ID || inv.Sessions[0].TTY == inv.Sessions[1].TTY {
		t.Fatalf("servers were not distinguished: %+v", inv.Sessions)
	}
	if managed, _ := main.ManagedInventory(ctx); len(managed.Sessions) != 2 || managed.Sessions[1].DiscoveryExcluded {
		t.Fatalf("turning external discovery off hid a managed Terminal: %+v", managed)
	}
	if err := main.Send(ctx, row, "terminal message"); err != nil {
		t.Fatal(err)
	}
	if err := main.Type(ctx, row, "terminal draft"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		capture, readable := main.Capture(ctx, row)
		screen, screenOK := main.Screen(ctx, row, 10)
		if readable && screenOK && strings.Contains(capture, "terminal draft") && strings.Contains(screen, "terminal draft") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Terminal draft was not captured: %q %q", capture, screen)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := main.Keystroke(ctx, row, []byte{13}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		var submitted []string
		for _, event := range stubEvents(t, termLog) {
			if event.Submit != nil && *event.Submit != "" {
				submitted = append(submitted, *event.Submit)
			}
		}
		if len(submitted) == 2 && submitted[0] == "terminal message" && submitted[1] == "terminal draft" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("submissions: %q", submitted)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if events := stubEvents(t, mainLog); len(events) != 0 {
		t.Fatalf("input reached the other server: %+v", events)
	}
	for _, id := range []string{"terminal:%0;kill-server", "terminal:%", "terminal:0"} {
		bad := row
		bad.ID = id
		if err := main.Keystroke(ctx, bad, []byte("x")); err == nil {
			t.Fatalf("malformed target %q accepted", id)
		}
	}
	main.Terminal = nil
	if err := main.Send(ctx, row, "must not fall back"); err == nil {
		t.Fatal("an unconfigured Terminal fell back to the main server")
	}
}

func TestTerminalReadFailureDoesNotProvePanesAbsent(t *testing.T) {
	_, _, main := privateTmux(t, "")
	// A real executable answering an error other than tmux's no-server sentence.
	main.Terminal = &Tmux{Binary: "/usr/bin/false", Socket: "/unused"}
	inv, _ := main.Inventory(context.Background())
	if inv.Complete || len(inv.Sessions) != 1 || len(inv.Notes) == 0 {
		t.Fatalf("failed Terminal listing was treated as empty: %+v", inv)
	}
}
