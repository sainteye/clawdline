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
