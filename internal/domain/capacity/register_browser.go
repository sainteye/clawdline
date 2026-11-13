package capacity

// Rows of the browser bridge (internal/app/browserbridge, docs/browser.md).
const (
	BrowserSessionChoices    = "browser.session_choices"
	BrowserProfileStateBytes = "browser.profile_state_bytes"
	BrowserCloseReceipts     = "browser.close_receipt_rows"
	BrowserCloseReceiptBytes = "browser.close_receipt_bytes"
	BrowserPendingCommands   = "browser.pending_commands"
	BrowserAuditRows         = "browser.audit_rows"
	BrowserArgBytes          = "browser.command_arg_bytes"
	BrowserResultBytes       = "browser.result_bytes"
	BrowserHostMessage       = "browser.host_message_bytes"
	BrowserAuditText         = "browser.audit_text_characters"
	BrowserStdinText         = "browser.stdin_text_bytes"
	BrowserActivityRows      = "browser.activity_picture_rows"
	BrowserActivityBytes     = "browser.activity_picture_bytes"
)

// browserBounds is every registered row named browser.*.
func browserBounds() []Entry {
	return []Entry{
		// Durable choice receipts prevent a later action from being treated as a new selection.
		{Name: BrowserSessionChoices, Class: Idempotency, Unit: Rows, Limit: 256, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/app/browserbridge.MaxSessionBrowsers"}},
		{Name: BrowserProfileStateBytes, Class: Idempotency, Unit: Bytes, Limit: 64 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/app/browserbridge.MaxProfileStateBytes"}},
		{Name: BrowserCloseReceipts, Class: Buffer, Unit: Rows, Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.maxBrowserCloseReceipts"}},
		{Name: BrowserCloseReceiptBytes, Class: Buffer, Unit: Bytes, Limit: 6 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.maxBrowserCloseReceiptBytes"}},
		{
			// The viewer serializes native keyboard input and coalesces text.
			// A full buffer pauses input visibly, never retries unknown edits.
			// browser_keyboard_test.go links these frontend bounds to the register.
			Name: "browser.keyboard_pending_edits", Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
		},
		{
			Name: "browser.keyboard_pending_bytes", Class: Buffer, Unit: Bytes,
			Limit: 16384, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
		},
		{
			// Commands waiting for the native messaging host. One more is
			// refused browser_queue_full and the agent is told to retry.
			Name: BrowserPendingCommands, Class: Buffer, Unit: Rows,
			Limit: 32, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/browserbridge.MaxPendingCommands"},
		},
		{
			// What agents did in the browser, kept for the person to review.
			// The oldest row goes first and the route reports how many went.
			Name: BrowserAuditRows, Class: Journal, Unit: Rows,
			Limit: 2000, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon, Routine: true,
			Sources: []string{"internal/app/browserbridge.MaxAuditRows"},
		},
		{
			// Text one audit row keeps of what an agent typed. Longer text is
			// cut to this and the row says so; its full length is kept.
			Name: BrowserAuditText, Class: Buffer, Unit: Characters,
			Limit: 2000, AtLimit: Coalesce,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/browserbridge.MaxAuditTextRunes"},
		},
		{
			// What `clawdline browser type --stdin` reads, as a Secret Handoff
			// arrives; more is refused before anything is typed.
			Name: BrowserStdinText, Class: Buffer, Unit: Bytes,
			Limit: 16 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.maxStdinText"},
		},
		{
			// Recent actions that keep a small picture of the page after them,
			// in memory only; the oldest picture goes first.
			Name: BrowserActivityRows, Class: Observation, Unit: Rows,
			Limit: 40, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon, Routine: true,
			Sources: []string{"internal/app/browserbridge.MaxActivityThumbs"},
		},
		{
			// One such picture; a larger one is not kept.
			Name: BrowserActivityBytes, Class: Buffer, Unit: Bytes,
			Limit: 96 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/app/browserbridge.MaxThumbBytes"},
		},
		{
			Name: BrowserArgBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/browserbridge.MaxArgBytes"},
		},
		{
			// One result the host posts; a screenshot is the largest.
			Name: BrowserResultBytes, Class: Buffer, Unit: Bytes,
			Limit: 6 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/app/browserbridge.MaxResultBytes"},
		},
		{
			// Chrome's own bound on one message from a native host to an extension.
			Name: BrowserHostMessage, Class: Buffer, Unit: Bytes,
			Limit: 1 << 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"cmd/clawdline.nativeMessageLimit"},
		},
	}
}
