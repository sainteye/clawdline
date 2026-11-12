package capacity

// Rows of the browser bridge (internal/app/browserbridge, docs/browser.md).
const (
	BrowserPendingCommands = "browser.pending_commands"
	BrowserAuditRows       = "browser.audit_rows"
	BrowserArgBytes        = "browser.command_arg_bytes"
	BrowserResultBytes     = "browser.result_bytes"
	BrowserHostMessage     = "browser.host_message_bytes"
)

// browserBounds is every registered row named browser.*.
func browserBounds() []Entry {
	return []Entry{
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
