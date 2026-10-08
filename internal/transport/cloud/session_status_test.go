package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStatusProjectionCannotCarryContentAndWithholdsUnverifiedTarget(t *testing.T) {
	row := map[string]any{
		"id": "%19", "state": "waiting", "assistant": "claude", "backend": "tmux",
		"execution_generation": "0123456789abcdef0123456789abcdef",
		"line":                 "private prompt", "label": "private label", "cwd": "/private/path",
		"menu":   map[string]any{"question": "private menu"},
		"shells": []any{"private shell"}, "git": "private git",
		"activity":        map[string]any{"known": true, "at": float64(100), "detail": "private activity"},
		"acceptance":      map[string]any{"state": "pending", "phase": "done", "title": "private delivery"},
		"attention_count": float64(1),
		"source":          map[string]any{"provenance": "tmux", "observed_at": float64(100), "freshness": "current"},
		"closeability":    map[string]any{"state": "blocked", "source": map[string]any{"freshness": "current"}, "reasons": []any{"private obligation"}},
		"agents":          []any{map[string]any{"state": "failed", "what": "private agent"}},
		"agents_reading":  map[string]any{"state": "complete"},
	}
	got := ProjectSessionStatus("mac_a", "%19", row, true, 1900)
	if got.ExecutionGeneration == "" || !got.InventoryComplete || got.Source.ObservedAt != 100 ||
		got.LastMovementAt != 100 || got.NoProgressAfterMS != 1800000 || got.NoMovement == nil || !*got.NoMovement ||
		!got.CompletedUnconfirmed || !got.AttentionRequired || got.CloseBlocked == nil || !*got.CloseBlocked || got.FailedAgentCount == nil || *got.FailedAgentCount != 1 {
		t.Fatalf("current status: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private", "cwd", "menu", "shells", "git", "label", "line"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("status leaked %s: %s", secret, encoded)
		}
	}
	row["source"].(map[string]any)["freshness"] = "unverified"
	stale := ProjectSessionStatus("mac_a", "%19", row, false, 102)
	if stale.ExecutionGeneration != "" || stale.Source.Freshness != "unverified" || stale.CompletedUnconfirmed || stale.CloseBlocked != nil || stale.FailedAgentCount != nil {
		t.Fatalf("stale status: %+v", stale)
	}
	missing := ProjectSessionStatus("mac_a", "%19", nil, false, 103)
	if missing.ExecutionGeneration != "" || missing.State != "unknown" || missing.Source.Freshness != "missing" {
		t.Fatalf("missing status: %+v", missing)
	}
}

func TestStatusDoesNotCallDeployingCompletedOrInventSessionFailure(t *testing.T) {
	row := map[string]any{
		"state": "working", "source": map[string]any{"freshness": "current"},
		"acceptance":   map[string]any{"state": "pending", "phase": "deploying"},
		"closeability": map[string]any{"state": "blocked", "source": map[string]any{"freshness": "missing"}},
		"agents":       []any{}, "agents_reading": map[string]any{"state": "complete"},
	}
	got := ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if got.State != "working" || got.CompletedUnconfirmed || got.CloseBlocked != nil || got.FailedAgentCount == nil || *got.FailedAgentCount != 0 {
		t.Fatalf("deployed work is not confirmed done and stale closeability is not blocked: %+v", got)
	}
	row["closeability"] = map[string]any{"state": "safe", "source": map[string]any{"freshness": "current"}}
	delete(row, "agents") // The full row omits an empty provider-native list.
	got = ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if got.CloseBlocked == nil || *got.CloseBlocked || got.FailedAgentCount == nil || *got.FailedAgentCount != 0 {
		t.Fatalf("complete empty readings must publish measured false and zero: %+v", got)
	}
}

func TestStatusReplySignalRequiresAProvenCurrentQuestion(t *testing.T) {
	row := map[string]any{
		"state": "waiting", "work_person_needed": true,
		"source": map[string]any{"freshness": "current"},
	}
	if got := ProjectSessionStatus("mac_a", "%19", row, true, 100); got.WaitingForReply {
		t.Fatalf("a waiting state and person-needed work invented a question: %+v", got)
	}
	row["screen_reading"] = "read"
	row["menu"] = map[string]any{"question": "private question", "options": []any{map[string]any{"label": "private option"}}}
	got := ProjectSessionStatus("mac_a", "%19", row, true, 100)
	if !got.WaitingForReply {
		t.Fatalf("current captured menu lost its reply signal: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private") {
		t.Fatalf("reply signal copied menu content: %s", encoded)
	}
	row["screen_reading"] = "unavailable"
	row["menu"].(map[string]any)["source"] = "transcript"
	if got := ProjectSessionStatus("mac_a", "%19", row, true, 100); !got.WaitingForReply {
		t.Fatalf("open single-question transcript menu lost its signal: %+v", got)
	}
	row["menu"].(map[string]any)["source"] = ""
	if got := ProjectSessionStatus("mac_a", "%19", row, true, 100); got.WaitingForReply {
		t.Fatalf("an unavailable screen without transcript proof invented a question: %+v", got)
	}
	row["source"].(map[string]any)["freshness"] = "unverified"
	row["screen_reading"] = "read"
	if got := ProjectSessionStatus("mac_a", "%19", row, false, 101); got.WaitingForReply {
		t.Fatalf("a carried old question remained actionable: %+v", got)
	}
}

func TestStatusRelayFailureDoesNotBlockLegacyInventory(t *testing.T) {
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}}
	p.Publish = func(_ context.Context, out Outbound) error {
		if strings.HasPrefix(out.Channel, "ss/") {
			return errors.New("relay does not accept ss yet")
		}
		return nil
	}
	reading := sessionReading{sessions: []map[string]any{{"id": "%19", "state": "idle"}}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	if p.unsent != 0 || p.published["status_snapshot"] != ([32]byte{}) {
		t.Fatalf("status failure blocked legacy inventory: unsent=%d published=%v", p.unsent, p.published)
	}
}

func TestStatusInventoryFollowsTheRows(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	reading := sessionReading{sessions: []map[string]any{{"id": "%19", "state": "idle"}}, at: json.RawMessage(`100`), complete: true}
	p.publishStatuses(context.Background(), reading, []string{"%19"})
	channels := c.channels()
	if len(channels) != 2 || channels[0] != "ss/mac_a/%2519" || channels[1] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("status publication order: %v", channels)
	}
	marker := c.payload(t, channels[1])
	row := c.payload(t, channels[0])
	if marker["at"] != float64(100) || marker["complete"] != true ||
		marker["snapshot_generation"] == "" || marker["snapshot_generation"] != row["snapshot_generation"] {
		t.Fatalf("status marker: %v", marker)
	}
}

func TestStatusInventoryPublishesAnEmptySet(t *testing.T) {
	c := &collector{}
	p := &Publisher{MachineID: "mac_a", published: map[string][32]byte{}, sent: map[string]time.Time{}, Publish: c.publish}
	p.publishStatuses(context.Background(), sessionReading{at: json.RawMessage(`100`), complete: true}, nil)
	channels := c.channels()
	if len(channels) != 1 || channels[0] != "ss/mac_a/"+InventorySessionID {
		t.Fatalf("empty status publication: %v", channels)
	}
	inventory, ok := c.payload(t, channels[0])["inventory"].(map[string]any)
	if !ok {
		t.Fatal("empty marker has no inventory")
	}
	if sessions, ok := inventory["sessions"].([]any); !ok || len(sessions) != 0 {
		t.Fatalf("empty snapshot was not a proved empty set: %v", inventory["sessions"])
	}
}
