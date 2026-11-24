package projectassistant

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCollaborationRequestPolicyAndReplyBoundary(t *testing.T) {
	q := BotMessageRequest{IdempotencyKey: "review", TargetBotID: "b", Text: strings.Repeat("a", MaxCollaborationRequestBytes)}
	if err := CheckCollaborationRequest(q, UserOrigin, 0); err != nil {
		t.Fatal(err)
	}
	q.Text += "a"
	if Code(CheckCollaborationRequest(q, UserOrigin, 0)) != CapacityFull {
		t.Fatal("request overflow accepted")
	}
	q.Text = "Review"
	if Code(CheckCollaborationRequest(q, BotRequestOrigin, 0)) != ScopeDenied {
		t.Fatal("request recursion accepted")
	}
	if Code(CheckCollaborationRequest(q, BotReplyOrigin, MaxCollaborationHops)) != CapacityFull {
		t.Fatal("hop overflow accepted")
	}
	if err := CheckCollaborationRequest(q, BotReplyOrigin, MaxCollaborationHops-1); err != nil {
		t.Fatal(err)
	}
	whole := strings.Repeat("a", MaxCollaborationReplyBytes)
	if CollaborationReply(whole) != whole || ReplyTruncated(whole) {
		t.Fatal("a reply at the bound was changed")
	}
	long := strings.Repeat("界", MaxCollaborationReplyBytes)
	s := CollaborationReply(long)
	if !utf8.ValidString(s) || len(s) > MaxCollaborationReplyBytes || !ReplyTruncated(s) ||
		!strings.Contains(s, "original "+strconv.Itoa(len(long))+" bytes") || !strings.HasPrefix(long, s[:strings.Index(s, "\n\n"+ReplyTruncatedMarker)]) {
		t.Fatalf("invalid reply boundary: %d bytes, tail %q", len(s), s[len(s)-120:])
	}
	raw, _ := json.Marshal(Turn{})
	for _, key := range []string{`"origin"`, `"exchange_id"`, `"hops"`} {
		if strings.Contains(string(raw), key) {
			t.Fatal("empty legacy canonical shape changed", string(raw))
		}
	}
	if ValidateTurnOrigin(Turn{Origin: UserOrigin, ExchangeID: "forged"}) == nil {
		t.Fatal("user acquired exchange authority")
	}
}
