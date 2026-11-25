package projectassistant

import (
	"strings"
	"testing"
)

func TestOrderQueueMovesOnlyAheadOfHeldAutomaticWork(t *testing.T) {
	q := func(id string, origin TurnOrigin) Turn {
		return Turn{ID: id, State: TurnQueued, Origin: origin, IdempotencyKey: id}
	}
	released := q("released", UserOrigin)
	released.ReleasedRestart = true
	running := q("running", HostNoticeOrigin)
	running.State = TurnRunning
	resume := q("resume", HostNoticeOrigin)
	resume.IdempotencyKey = RestartContinuationPrefix + "old"
	edited := q("edited", HostNoticeOrigin)
	edited.QueueEditID = "edit"
	for _, tc := range []struct {
		name string
		in   []Turn
		want string
	}{
		{"ahead of reports and Bot messages", []Turn{q("report", HostNoticeOrigin), q("request", BotRequestOrigin), q("reply", BotReplyOrigin), released}, "released report request reply"},
		{"never ahead of the person's earlier message", []Turn{q("report", HostNoticeOrigin), q("earlier", UserOrigin), q("later-report", HostNoticeOrigin), released}, "report earlier released later-report"},
		{"never ahead of a running turn", []Turn{running, q("report", HostNoticeOrigin), released}, "running released report"},
		{"never ahead of a restart continuation", []Turn{resume, q("report", HostNoticeOrigin), released}, "resume released report"},
		{"never ahead of a held edit", []Turn{edited, released}, "edited released"},
		{"an unmarked message keeps its place", []Turn{q("report", HostNoticeOrigin), q("plain", UserOrigin)}, "report plain"},
	} {
		var got []string
		for _, turn := range OrderQueue(tc.in) {
			got = append(got, turn.ID)
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%s: %v, want %s", tc.name, got, tc.want)
		}
	}
}
