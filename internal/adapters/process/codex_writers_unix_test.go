//go:build darwin || linux

package process

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Same shape as the native table: the foreground TUI owns the tty, while
// its app-server launcher and writer are detached descendants. A peer TUI
// has another writer; a code-mode host is not an assistant process.
const managedCodexTable = `
ttys001 101 101 101 1 /synthetic/bin/codex
?? 201 201 -1 101 /synthetic/bin/codex app-server
?? 301 301 -1 201 /synthetic/bin/codex app-server
?? 401 301 -1 301 /synthetic/bin/codex-code-mode-host
ttys002 102 102 102 1 /synthetic/bin/codex
?? 202 202 -1 102 /synthetic/bin/codex app-server
?? 999 999 -1 1 /synthetic/bin/codex app-server
?? 801 801 -1 301 /synthetic/bin/codex exec audit
?? 802 802 -1 801 /synthetic/bin/codex app-server
`

func TestManagedCodexWritersBindOnlyTheirOwnForegroundTUI(t *testing.T) {
	ctx := context.Background()
	for _, narrow := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "selected-tty"}[narrow], func(t *testing.T) {
			calls := 0
			p := &PS{table: func(context.Context) ([]byte, error) { return []byte(managedCodexTable), nil },
				Head: func(path string) (RolloutMeta, bool) {
					switch path {
					case rolloutA:
						return RolloutMeta{Conversation: idA, Thread: idA, CWD: "/code/demo"}, true
					case rolloutB:
						return RolloutMeta{Conversation: idA, Thread: "guardian", ThreadSource: "guardian_review", CWD: "/code/demo"}, true
					}
					return RolloutMeta{}, false
				}, Open: func(_ context.Context, pids []int) (map[int][]string, map[int]string, bool) {
					calls++
					sort.Ints(pids)
					want := []int{101, 102, 201, 202, 301}
					if narrow {
						want = []int{101, 201, 301}
					}
					if !reflect.DeepEqual(pids, want) {
						t.Fatalf("open files asked %v, want %v", pids, want)
					}
					return map[int][]string{301: {rolloutA, rolloutB}, 202: {"/unrelated"}, 999: {rolloutB}}, map[int]string{101: "/code/demo"}, true
				}}
			var inv session.Inventory
			var err error
			if narrow {
				inv, err = p.ScanTTYs(ctx, map[string]bool{"ttys001": true})
			} else {
				inv, err = p.Scan(ctx)
			}
			if err != nil || !inv.Complete || calls != 1 {
				t.Fatalf("scan %+v, %v, calls %d", inv, err, calls)
			}
			row := inv.Sessions[0]
			if row.PID != 101 || row.ConversationID != idA || row.Binding != session.BindingOpenFile || row.CWD != "/code/demo" {
				t.Fatalf("managed TUI did not bind its writer: %+v", row)
			}
			if !narrow && (len(inv.Sessions) != 2 || inv.Sessions[1].ConversationID != "" || inv.Sessions[1].Binding != session.BindingNoRecord) {
				t.Fatalf("another terminal borrowed this conversation: %+v", inv.Sessions)
			}
		})
	}
}

func TestManagedCodexWriterAmbiguityAndFailuresStayExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		read   bool
		direct bool
		want   session.Binding
	}{
		{"two-root-conversations", true, false, session.BindingAmbiguous},
		{"unreadable-open-files", false, false, session.BindingUnreadable},
		{"direct-root-wins", true, true, session.BindingOpenFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &PS{table: func(context.Context) ([]byte, error) { return []byte(managedCodexTable), nil }, Head: heads(nil),
				Open: func(context.Context, []int) (map[int][]string, map[int]string, bool) {
					files := map[int][]string{301: {rolloutA, rolloutB}}
					if tc.direct {
						files[101] = []string{rolloutA}
					}
					return files, nil, tc.read
				}}
			inv, err := p.ScanTTYs(context.Background(), map[string]bool{"ttys001": true})
			if err != nil || len(inv.Sessions) != 1 || inv.Sessions[0].Binding != tc.want {
				t.Fatalf("scan %+v %v", inv, err)
			}
			if tc.direct && inv.Sessions[0].ConversationID != idA {
				t.Fatalf("direct identity changed: %+v", inv.Sessions[0])
			}
			if !tc.direct && inv.Sessions[0].ConversationID != "" {
				t.Fatal("ambiguous or unread evidence named a conversation")
			}
		})
	}
}

func TestManagedCodexWriterDoesNotCrossAnotherTerminalOrAnOrphan(t *testing.T) {
	table := managedCodexTable + `
ttys003 501 501 501 301 /synthetic/bin/codex
?? 601 601 -1 501 /synthetic/bin/codex app-server
?? 701 701 -1 888 /synthetic/bin/codex app-server
?? 301 301 -1 201 /synthetic/bin/codex app-server
`
	got := codexWriterPIDs(table, []session.Session{{PID: 101, TTY: "ttys001", Assistant: session.AssistantCodex}})
	if !reflect.DeepEqual(got[101], []int{201, 301}) {
		t.Fatalf("unowned writer included: %v", got)
	}
}
