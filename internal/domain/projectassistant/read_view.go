package projectassistant

// ReadViewMode selects one source-owned read projection. No mode executes a
// provider, grants a native permission or substitutes for current authority.
type ReadViewMode string

const (
	ReadViewSnapshot    ReadViewMode = "snapshot"
	ReadViewHistory     ReadViewMode = "history"
	ReadViewEvents      ReadViewMode = "events"
	ReadViewPermissions ReadViewMode = "permissions"
	ReadViewTraces      ReadViewMode = "traces"
	ReadViewProgress    ReadViewMode = "progress"
	ReadViewDetail      ReadViewMode = "detail"
	// Cursor bytes bound a stateless signed position/reference, not retained
	// history. Oversized positions are refused rather than truncated.
	MaxReadViewCursorBytes = 4 << 10
	// MaxExchangeExcerptRunes is how much of an exchange's request, reply
	// and notice text a chat page carries when it asks for excerpts. The
	// chat shows a shorter summary; the exchange panel reads the full text.
	MaxExchangeExcerptRunes = 1000
)

type ReadViewRequest struct {
	Key             BotKey
	ConversationID  string
	Mode            ReadViewMode
	SourceEpoch     string
	Cursor          string
	AfterSeq        int64
	PageSize        int
	TeamCards       bool
	DeferTraceCount bool
	// ExchangeExcerpts shortens exchange text in timeline rows to
	// MaxExchangeExcerptRunes. Only snapshot and history pages have them.
	ExchangeExcerpts bool
}

func CheckReadViewRequest(q ReadViewRequest) error {
	if q.Key.MachineID == "" || q.Key.BotID == "" || q.ConversationID == "" {
		return fail(Invalid, "read_view_key")
	}
	switch q.Mode {
	case ReadViewSnapshot, ReadViewHistory, ReadViewEvents, ReadViewPermissions, ReadViewTraces, ReadViewProgress, ReadViewDetail:
	default:
		return fail(Invalid, "read_view_mode")
	}
	if len(q.Cursor) > MaxReadViewCursorBytes {
		return fail(CapacityFull, "read_view_cursor_bytes")
	}
	if err := CheckPage(q.PageSize, q.Mode == ReadViewEvents); err != nil {
		return err
	}
	if q.AfterSeq < 0 {
		return fail(Invalid, "after_seq")
	}
	if q.ExchangeExcerpts && q.Mode != ReadViewSnapshot && q.Mode != ReadViewHistory {
		return fail(Invalid, "exchange_excerpts")
	}
	if q.Mode == ReadViewSnapshot {
		if q.SourceEpoch != "" || q.Cursor != "" || q.AfterSeq != 0 {
			return fail(Invalid, "read_view_snapshot")
		}
		return nil
	}
	if q.SourceEpoch == "" {
		return fail(Invalid, "source_epoch")
	}
	if q.Mode == ReadViewEvents {
		if q.Cursor != "" {
			return fail(Invalid, "read_view_events")
		}
		return nil
	}
	if q.AfterSeq != 0 {
		return fail(Invalid, "after_seq")
	}
	if (q.Mode == ReadViewHistory || q.Mode == ReadViewDetail) && q.Cursor == "" {
		return fail(Invalid, "read_view_cursor")
	}
	if q.Mode == ReadViewDetail && q.PageSize != 1 {
		return fail(Invalid, "page_size")
	}
	return nil
}

// ExcerptExchange shortens an exchange notice's text, request and reply to
// MaxExchangeExcerptRunes each. Other messages are unchanged.
func ExcerptExchange(m *Message) {
	if m.Exchange == nil {
		return
	}
	x := *m.Exchange
	m.Text, x.Request, x.Reply = excerptRunes(m.Text), excerptRunes(x.Request), excerptRunes(x.Reply)
	m.Exchange = &x
}

func excerptRunes(s string) string {
	n := 0
	for i := range s {
		if n == MaxExchangeExcerptRunes {
			return s[:i]
		}
		n++
	}
	return s
}
