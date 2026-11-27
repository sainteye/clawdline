package projectassistant

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// MaxNotifySummaryBytes bounds the one line a notify_person message leads
// with; it is also what a push shows. Registered as bots.notify_summary_bytes.
const MaxNotifySummaryBytes = 300

// MaxNotifyDetailBytes bounds the optional detail under the summary.
// Registered as bots.notify_detail_bytes.
const MaxNotifyDetailBytes = 4000

// NotifyPersonRequest is the notify_person tool's input: something the person
// should know, written to the Bot's own main chat as a message that counts as
// unread and is pushed, also from a routine or another Bot's request.
type NotifyPersonRequest struct {
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

// NotifyPersonText validates a request and returns the message text: the
// summary line, then the detail after a blank line.
func NotifyPersonText(q NotifyPersonRequest) (string, error) {
	summary, detail := strings.TrimSpace(q.Summary), strings.TrimSpace(q.Detail)
	if summary == "" || !utf8.ValidString(summary) || strings.ContainsAny(summary, "\r\n") {
		return "", fail(Invalid, "notify_summary")
	}
	if !notifySummaryBytes.admit(len(summary), MaxNotifySummaryBytes) {
		return "", fail(CapacityFull, "notify_summary")
	}
	if !utf8.ValidString(detail) {
		return "", fail(Invalid, "notify_detail")
	}
	if !notifyDetailBytes.admit(len(detail), MaxNotifyDetailBytes) {
		return "", fail(CapacityFull, "notify_detail")
	}
	text := summary
	if detail != "" {
		text += "\n\n" + detail
	}
	return text, ValidateMessage(text)
}

// NotifyPersonID is the stable identity of one notification within a turn,
// so a retried call writes the same message once.
func NotifyPersonID(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "notify-" + hex.EncodeToString(sum[:12])
}

var notifySummaryBytes, notifyDetailBytes boundUsage

// NotifySummaryBytesUsage is the diagnostics reading for bots.notify_summary_bytes.
func NotifySummaryBytesUsage() BoundUsage { return notifySummaryBytes.usage() }

// NotifyDetailBytesUsage is the diagnostics reading for bots.notify_detail_bytes.
func NotifyDetailBytesUsage() BoundUsage { return notifyDetailBytes.usage() }
