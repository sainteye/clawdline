//go:build darwin || linux

package antigravitysession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestNativeNoticeRequiresAcknowledgedOwnUserTurn(t *testing.T) {
	ctx := context.Background()
	h, a, opts := persistentFixture(t)
	b, err := h.Open(ctx, ports.OpenRequest{Cwd: a.CWD})
	if err != nil {
		t.Fatal(err)
	}
	const wire = `<clawdline-notice>{"protocol":"clawdline.notice","kind":"task_finished","notice_id":"synthetic-notice"}</clawdline-notice>`
	if err := h.Send(ctx, a, wire); err != nil {
		t.Fatal(err)
	}
	if read, err := h.NoticeRead(ctx, a, "synthetic-notice"); err != nil || read {
		t.Fatal("pipe echo claimed model observation", read, err)
	}
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, a)
	if read, err := h.NoticeRead(ctx, a, "synthetic-notice"); err != nil || !read {
		t.Fatal("acknowledged own notice missing", read, err)
	}
	cb, _ := h.lookup(b)
	cb.mu.Lock()
	cb.append(contract.TranscriptEntry{Role: "assistant", Text: wire})
	cb.mu.Unlock()
	if read, err := h.NoticeRead(ctx, b, "synthetic-notice"); err != nil || read {
		t.Fatal("another conversation or assistant copy counted", read, err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	if read, err := h.NoticeRead(ctx, a, "synthetic-notice"); err != nil || !read {
		t.Fatal("offline durable notice was lost", read, err)
	}
	if n, _ := h.Stats(); n != 0 {
		t.Fatal("history evidence launched a process")
	}
}

func TestNativeNoticeFailedTurnCannotBeWashedByLaterSuccess(t *testing.T) {
	for _, reason := range []string{"cancelled", "max_tokens", "refusal"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			h, row, opts := persistentFixture(t)
			reasonPath := filepath.Join(opts.AccountDir, "prompt-stop-reason")
			if err := os.WriteFile(reasonPath, []byte(reason), 0600); err != nil {
				t.Fatal(err)
			}
			const wire = `<clawdline-notice>{"protocol":"clawdline.notice","kind":"task_finished","notice_id":"failed-notice"}</clawdline-notice>`
			if err := h.Send(ctx, row, wire); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			idleHost(t, h, row)
			assertUnread := func() {
				t.Helper()
				if read, err := h.NoticeRead(ctx, row, "failed-notice"); err != nil || read {
					t.Fatal("failed native turn counted as clean", read, err)
				}
			}
			assertUnread()
			if err := os.Remove(reasonPath); err != nil {
				t.Fatal(err)
			}
			if err := h.Send(ctx, row, "Unrelated successful input"); err != nil {
				t.Fatal(err)
			}
			idleHost(t, h, row)
			assertUnread()
			stopHost(t, h)
			h = reopenHost(t, opts)
			assertUnread()
		})
	}
}

func TestNativeNoticeReceiptFailurePreservesUncertainty(t *testing.T) {
	ctx := context.Background()
	h, row, _ := persistentFixture(t)
	if _, err := h.store.db.Exec(`CREATE TRIGGER reject_notice_read BEFORE INSERT ON notice_reads BEGIN SELECT RAISE(FAIL,'synthetic receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	const wire = `<clawdline-notice>{"protocol":"clawdline.notice","kind":"task_finished","notice_id":"write-failed"}</clawdline-notice>`
	if err := h.Send(ctx, row, wire); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	var uncertain bool
	if err := h.store.db.QueryRow(`SELECT uncertain FROM conversations WHERE id=?`, row.ID).Scan(&uncertain); err != nil || !uncertain {
		t.Fatal("receipt failure cleared durable uncertainty", uncertain, err)
	}
	if read, err := h.NoticeRead(ctx, row, "write-failed"); err != nil || read {
		t.Fatal("failed receipt counted as observed", read, err)
	}
}
