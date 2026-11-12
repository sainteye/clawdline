package projectassistant

import (
	"strings"
	"testing"
)

func TestReadViewRequestSeparatesSnapshotsPagesAndReplay(t *testing.T) {
	base := ReadViewRequest{Key: BotKey{"machine", "bot"}, ConversationID: "conversation", PageSize: MaxPageRows}
	for _, mode := range []ReadViewMode{ReadViewSnapshot, ReadViewHistory, ReadViewEvents, ReadViewPermissions, ReadViewTraces, ReadViewDetail} {
		q := base
		q.Mode = mode
		if mode != ReadViewSnapshot {
			q.SourceEpoch = "reader-epoch"
		}
		if mode == ReadViewHistory || mode == ReadViewDetail {
			q.Cursor = "signed-position"
		}
		if mode == ReadViewDetail {
			q.PageSize = 1
		}
		if mode == ReadViewEvents {
			q.AfterSeq = 602
			q.PageSize = MaxEventPageRows
		}
		if err := CheckReadViewRequest(q); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	tests := []struct {
		name string
		edit func(*ReadViewRequest)
	}{
		{"foreign shape", func(q *ReadViewRequest) { q.Key.BotID = "" }},
		{"unknown mode", func(q *ReadViewRequest) { q.Mode = "export" }},
		{"snapshot cannot reuse epoch", func(q *ReadViewRequest) { q.SourceEpoch = "old" }},
		{"snapshot cannot start with an event cursor", func(q *ReadViewRequest) { q.AfterSeq = 602 }},
		{"snapshot cannot reuse position", func(q *ReadViewRequest) { q.Cursor = "old" }},
		{"page without epoch", func(q *ReadViewRequest) { q.Mode = ReadViewPermissions }},
		{"history without position", func(q *ReadViewRequest) { q.Mode = ReadViewHistory; q.SourceEpoch = "epoch" }},
		{"negative replay", func(q *ReadViewRequest) { q.Mode = ReadViewEvents; q.SourceEpoch = "epoch"; q.AfterSeq = -1 }},
		{"replay cannot take row position", func(q *ReadViewRequest) { q.Mode = ReadViewEvents; q.SourceEpoch = "epoch"; q.Cursor = "row" }},
		{"summary is not replay", func(q *ReadViewRequest) { q.Mode = ReadViewPermissions; q.SourceEpoch = "epoch"; q.AfterSeq = 1 }},
		{"detail is indivisible", func(q *ReadViewRequest) {
			q.Mode = ReadViewDetail
			q.SourceEpoch = "epoch"
			q.Cursor = "ref"
			q.PageSize = 2
		}},
		{"history page bound", func(q *ReadViewRequest) { q.PageSize = MaxPageRows + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q := base
			q.Mode = ReadViewSnapshot
			test.edit(&q)
			if Code(CheckReadViewRequest(q)) != Invalid {
				t.Fatal("invalid query accepted")
			}
		})
	}
	q := base
	q.Mode = ReadViewHistory
	q.SourceEpoch = "epoch"
	q.Cursor = strings.Repeat("x", MaxReadViewCursorBytes)
	if err := CheckReadViewRequest(q); err != nil {
		t.Fatal("maximum byte cursor refused", err)
	}
	q.Cursor += "x"
	if Code(CheckReadViewRequest(q)) != CapacityFull {
		t.Fatal("oversized cursor was not typed capacity refusal")
	}
	q.Cursor = strings.Repeat("界", MaxReadViewCursorBytes/3+1)
	if Code(CheckReadViewRequest(q)) != CapacityFull {
		t.Fatal("cursor budget counted runes instead of bytes")
	}
}
