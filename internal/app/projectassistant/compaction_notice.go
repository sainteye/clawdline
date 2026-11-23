package projectassistant

import (
	"strconv"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// compactionNoticePrefix names the persistent chat notice for a turn during
// which the agent compacted its own context. One per turn: the ID is stable.
const compactionNoticePrefix = "compacted-"

// compactionNotice reads only what the live native session reported for this
// turn. Without a reported compaction there is no notice.
func compactionNotice(session ProviderSession, t pa.Turn, at time.Time) *pa.Message {
	if t.Room != nil {
		return nil
	}
	reader, ok := session.(NativeContextReader)
	if !ok {
		return nil
	}
	state := reader.NativeContext()
	if state.Fence != t.Fence || state.CompactedTurnID != t.ID || state.Compactions <= 0 {
		return nil
	}
	// The console renders the key in its reader's language; Text is only the
	// English fallback for a reader that does not know the key.
	text, ref := compactionNoticeContent(state)
	return &pa.Message{ID: compactionNoticePrefix + t.ID, Fence: t.Fence, TurnID: t.ID, Kind: pa.NoticeMessage, Text: text, Notice: ref, At: at}
}

func compactionNoticeContent(state NativeContextState) (string, *pa.NoticeRef) {
	ref := &pa.NoticeRef{Key: pa.NoticeCompacted}
	text := "Earlier context was summarized to make room"
	if state.CompactedBefore > 0 && state.CompactedAfter > 0 && state.CompactedAfter < state.CompactedBefore {
		ref.TokensBefore, ref.TokensAfter = state.CompactedBefore, state.CompactedAfter
		text += ": " + compactTokens(state.CompactedBefore) + " → " + compactTokens(state.CompactedAfter)
	}
	return text, ref
}

func compactTokens(n int64) string {
	if n >= 1000 {
		return strconv.FormatInt((n+500)/1000, 10) + "K"
	}
	return strconv.FormatInt(n, 10)
}

// compactedBeforeTurn reports whether the most recently started earlier turn
// on this binding recorded a compaction notice. Any later started turn
// disarms it, so a compaction is re-delivered once; the record is the
// persisted notice, so the guard survives restarts.
func compactedBeforeTurn(x Export, t pa.Turn, bindingID string) bool {
	var last *pa.Turn
	for i := range x.Turns {
		p := &x.Turns[i]
		if p.ID == t.ID || p.StartedAt == nil || p.Room != nil || p.Fence.BindingID != bindingID {
			continue
		}
		if last == nil || p.StartedAt.After(*last.StartedAt) {
			last = p
		}
	}
	if last == nil || last.State != pa.TurnSucceeded {
		return false
	}
	for _, m := range x.Messages {
		if m.ID == compactionNoticePrefix+last.ID && m.Kind == pa.NoticeMessage && m.TurnID == last.ID {
			return true
		}
	}
	return false
}
