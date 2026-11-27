package http

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (m *paMatrix) list(t *testing.T, sender, cursor string, size int) contract.ProjectAssistantListResponse {
	t.Helper()
	a, inner := m.word(t, sender, "list", map[string]any{"machine_id": m.key.MachineID, "query": "", "cursor": cursor, "page_size": size})
	if !a.OK() {
		t.Fatalf("%s list: %d %s", sender, a.Status, a.Payload)
	}
	var out contract.ProjectAssistantListResponse
	if err := json.Unmarshal(inner, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (m *paMatrix) summary(t *testing.T, sender string) contract.ProjectAssistantBotSummary {
	t.Helper()
	for _, item := range m.list(t, sender, "", 10).Items {
		if item.Bot.Key.BotID == m.key.BotID {
			return item
		}
	}
	t.Fatalf("%s roster lacks the Bot", sender)
	return contract.ProjectAssistantBotSummary{}
}

func (m *paMatrix) tcpSummary(t *testing.T, token string) contract.ProjectAssistantBotSummary {
	t.Helper()
	status, raw := m.tcp(t, "GET", "/v1/project-assistants?machine_id="+m.key.MachineID+"&query=&cursor=&page_size=10", token, nil, nil)
	if status != 200 {
		t.Fatalf("TCP list %d %s", status, raw)
	}
	var out contract.ProjectAssistantListResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, item := range out.Items {
		if item.Bot.Key.BotID == m.key.BotID {
			return item
		}
	}
	t.Fatal("TCP roster lacks the Bot")
	return contract.ProjectAssistantBotSummary{}
}

func (m *paMatrix) historyItems(t *testing.T, sender string) []contract.ProjectAssistantMessage {
	t.Helper()
	g := m.get(t, m.local)
	a, inner := m.word(t, sender, "history", map[string]any{"key": map[string]any{"machine_id": m.key.MachineID, "bot_id": m.key.BotID}, "conversation_id": g.Conversation.ID, "before_seq": 0, "page_size": 100})
	if !a.OK() {
		t.Fatalf("%s history: %d %s", sender, a.Status, a.Payload)
	}
	var out contract.ProjectAssistantHistoryResponse
	if err := json.Unmarshal(inner, &out); err != nil {
		t.Fatal(err)
	}
	// A history page is newest first; the matrix reads it in conversation order.
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Seq < out.Items[j].Seq })
	return out.Items
}

func (m *paMatrix) markRead(t *testing.T, sender, id string, seq int64) contract.ProjectAssistantReadCursor {
	t.Helper()
	a, inner := m.word(t, sender, "read", m.readBody(t, id, seq))
	if !a.OK() {
		t.Fatalf("%s read %d: %d %s", sender, seq, a.Status, a.Payload)
	}
	return paMatrixCursor(t, inner)
}

// Matrix rows C1-C5: two independent viewer instances continue one
// conversation, with exact command receipt identity and per-reader frontiers.
func TestProjectAssistantCloudResumeMatrixTwoViewersContinueOneConversation(t *testing.T) {
	m := newPAMatrix(t)
	m.pair(t, "owner-phone", adaptercloud.ScopeOperate, false)
	m.pair(t, "owner-laptop", adaptercloud.ScopeOperate, false)
	m.pair(t, "guest", adaptercloud.ScopeOperate, true)

	// C1: the phone sends; the laptop asks for the same command after an
	// unknown reply and receives the identical receipt without a new prompt.
	first := m.sendBody(t, "turn-1", "First synthetic input")
	a, inner := m.word(t, "owner-phone", "send", first)
	if !a.OK() {
		t.Fatalf("C1 phone send: %d %s", a.Status, a.Payload)
	}
	r1 := paMatrixReceipt(t, inner)
	routineHumanIdle(t, m.paServerFixture)
	a, inner = m.word(t, "owner-laptop", "send", first)
	if !a.OK() {
		t.Fatalf("C1 laptop replay: %d %s", a.Status, a.Payload)
	}
	if r := paMatrixReceipt(t, inner); r.ID != r1.ID || r.State != r1.State || r.ResourceID != r1.ResourceID || r.Command.ContentHash != r1.Command.ContentHash {
		t.Fatalf("C1 receipt identity differs: %+v vs %+v", r, r1)
	}
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatalf("C1 replay prompted again: %d", sends)
	}
	// C1: the same key with a different intent is a conflict, not a prompt.
	changed := first
	changed.Text = "Different intent"
	if a, inner = m.word(t, "owner-laptop", "send", changed); a.OK() || paMatrixCode(a, inner) != string(pa.IdempotencyConflict) {
		t.Fatalf("C1 changed intent: %d %s", a.Status, a.Payload)
	}

	// C2: both owner devices read the same durable history.
	phone, laptop := m.historyItems(t, "owner-phone"), m.historyItems(t, "owner-laptop")
	if len(phone) != 2 || len(laptop) != 2 || phone[0].ID != laptop[0].ID || phone[1].ID != laptop[1].ID || phone[0].Kind != contract.ProjectAssistantMessageKindUser || phone[1].Kind != contract.ProjectAssistantMessageKindAssistant {
		t.Fatalf("C2 history differs: %+v / %+v", phone, laptop)
	}

	// C3: the laptop continues the same conversation from its own snapshot,
	// on the same native binding.
	a, inner = m.word(t, "owner-laptop", "send", m.sendBody(t, "turn-2", "Second synthetic input"))
	if !a.OK() {
		t.Fatalf("C3 laptop continue: %d %s", a.Status, a.Payload)
	}
	r2 := paMatrixReceipt(t, inner)
	routineHumanIdle(t, m.paServerFixture)
	if r2.ID == r1.ID {
		t.Fatal("C3 new command reused an old receipt")
	}
	items := m.historyItems(t, "owner-phone")
	if len(items) != 4 || items[0].Fence.ConversationID != items[3].Fence.ConversationID || items[2].Text != "Second synthetic input" {
		t.Fatalf("C3 conversation did not continue: %+v", items)
	}
	sends, opens := m.p.counts()
	x := m.export(t)
	if sends != 2 || len(x.Turns) != 2 || x.Turns[0].Fence.ConversationID != x.Turns[1].Fence.ConversationID {
		t.Fatalf("C3 sends=%d turns=%d", sends, len(x.Turns))
	}
	m.p.mu.Lock()
	last := m.p.opens[len(m.p.opens)-1]
	m.p.mu.Unlock()
	t.Logf("C3 provider opens=%d resume=%v native=%q generations=%d,%d", opens, last.Resume, last.Binding.NativeSessionID, x.Turns[0].Fence.Generation, x.Turns[1].Fence.Generation)
	if opens > 1 && (!last.Resume || last.Binding.NativeSessionID != "synthetic-matrix-session") {
		t.Fatalf("C3 continuation did not resume the native session: %+v", last.Binding)
	}

	// C4: account-paired devices share the owner's frontier by design;
	// an invited viewer's frontier is its own.
	tail := items[3].Seq
	if c := m.markRead(t, "owner-phone", "phone-read", 2); c.MessageSeq != 2 {
		t.Fatalf("C4 phone frontier %+v", c)
	}
	// Only the final replies to the person's messages past the frontier are
	// unread.
	var unread int64
	for _, item := range items {
		if item.Seq > 2 && pa.MessageKind(item.Kind) == pa.AssistantMessage && strings.HasPrefix(item.ID, "reply-") {
			unread++
		}
	}
	if s := m.summary(t, "owner-laptop"); s.ReadCursor.MessageSeq != 2 || s.UnreadCount != unread || unread == 0 {
		t.Fatalf("C4 laptop owner frontier %+v unread=%d", s.ReadCursor, s.UnreadCount)
	}
	guest := m.summary(t, "guest")
	if guest.ReadCursor.MessageSeq != 0 || guest.ReadCursor.ReaderID == m.summary(t, "owner-phone").ReadCursor.ReaderID {
		t.Fatalf("C4 guest shares owner frontier: %+v", guest.ReadCursor)
	}
	if c := m.markRead(t, "guest", "guest-read", tail); c.MessageSeq != tail {
		t.Fatalf("C4 guest frontier %+v", c)
	}
	if s := m.summary(t, "owner-phone"); s.ReadCursor.MessageSeq != 2 {
		t.Fatalf("C4 guest moved owner frontier: %+v", s.ReadCursor)
	}

	// C5: two TCP device clients keep separate frontiers too.
	status, raw := m.tcp(t, "POST", "/v1/project-assistants/"+m.key.BotID+"/read", m.readonly, m.readBody(t, "lan-reader-read", 1), nil)
	if status != 200 || paMatrixCursor(t, raw).MessageSeq != 1 {
		t.Fatalf("C5 reader mark %d %s", status, raw)
	}
	reader, sender := m.tcpSummary(t, m.readonly), m.tcpSummary(t, m.lan)
	if reader.ReadCursor.MessageSeq != 1 || sender.ReadCursor.MessageSeq != 0 || reader.ReadCursor.ReaderID == sender.ReadCursor.ReaderID {
		t.Fatalf("C5 TCP frontiers %+v / %+v", reader.ReadCursor, sender.ReadCursor)
	}
	if sends, _ := m.p.counts(); sends != 2 {
		t.Fatal("C4/C5 read metadata prompted")
	}
}

// Matrix rows D1-D4: fresh re-pair and grant changes against old cursors and
// in-flight replies, ordered by barriers.
func TestProjectAssistantCloudResumeMatrixRePairInvalidatesCursorsWithoutReplay(t *testing.T) {
	m := newPAMatrix(t)
	other := m.create()
	other.Command.Key.BotID = "synthetic-bot-two"
	other.Command.IdempotencyKey = "create-two"
	paServerOK(t, m.request(t, "POST", "/v1/project-assistants", m.local, other))
	m.pair(t, "guest", adaptercloud.ScopeOperate, true)

	// D1: a roster page cursor is bound to the grant revision.
	page := m.list(t, "guest", "", 1)
	if page.NextCursor == "" || len(page.Items) != 1 {
		t.Fatalf("D1 no second page: %+v", page)
	}
	readerBefore := m.grant(t, "guest").ReaderID
	m.pair(t, "guest", adaptercloud.ScopeOperate, true)
	a, inner := m.word(t, "guest", "list", map[string]any{"machine_id": m.key.MachineID, "query": "", "cursor": page.NextCursor, "page_size": 1})
	if a.OK() || paMatrixCode(a, inner) != string(pa.CursorExpired) {
		t.Fatalf("D1 old roster cursor after re-pair: %d %s", a.Status, a.Payload)
	}
	if fresh := m.list(t, "guest", "", 1); fresh.NextCursor == "" || m.grant(t, "guest").ReaderID != readerBefore {
		t.Fatal("D1 re-pair lost the reader or a fresh page")
	}

	// D2: an accepted turn whose reply is still in flight across a re-pair is
	// completed once; the replay under the fresh grant is the same receipt.
	hold := make(chan struct{})
	started := make(chan string, 1)
	m.p.mu.Lock()
	m.p.hold, m.p.signal = hold, started
	m.p.mu.Unlock()
	inflight := m.sendBody(t, "in-flight", "Reply crosses a re-pair")
	a, inner = m.word(t, "guest", "send", inflight)
	if !a.OK() {
		t.Fatalf("D2 accept: %d %s", a.Status, a.Payload)
	}
	accepted := paMatrixReceipt(t, inner)
	if text := <-started; text != "Reply crosses a re-pair" {
		t.Fatalf("D2 provider got %q", text)
	}
	m.pair(t, "guest", adaptercloud.ScopeOperate, true)
	// The replay while the reply is held is the accepted receipt, not a prompt.
	a, inner = m.word(t, "guest", "send", inflight)
	if !a.OK() || paMatrixReceipt(t, inner).ID != accepted.ID {
		t.Fatalf("D2 replay during reply: %d %s", a.Status, a.Payload)
	}
	m.p.mu.Lock()
	m.p.hold, m.p.signal = nil, nil
	m.p.mu.Unlock()
	close(hold)
	routineHumanIdle(t, m.paServerFixture)
	a, inner = m.word(t, "guest", "send", inflight)
	if !a.OK() || paMatrixReceipt(t, inner).ID != accepted.ID {
		t.Fatalf("D2 replay after reply: %d %s", a.Status, a.Payload)
	}
	items := m.historyItems(t, "guest")
	replies := 0
	for _, item := range items {
		if item.Kind == contract.ProjectAssistantMessageKindAssistant {
			replies++
		}
	}
	if sends, _ := m.p.counts(); sends != 1 || len(items) != 2 || replies != 1 {
		t.Fatalf("D2 sends=%d items=%+v", sends, items)
	}

	// D3: a read view packed under one grant is not published after a fresh
	// re-pair interleaves before publication; a fresh request succeeds.
	repaired := 0
	m.s.projectAssistants.beforeViewPublish = func() {
		m.s.projectAssistants.beforeViewPublish = nil
		repaired++
		m.pair(t, "guest", adaptercloud.ScopeOperate, true)
	}
	a, inner = m.word(t, "guest", "view", m.viewFields(t, contract.ProjectAssistantReadViewModeSnapshot, "", 1))
	if repaired != 1 || a.OK() || paMatrixCode(a, inner) != string(pa.Revoked) {
		t.Fatalf("D3 publication across re-pair: %d %s", a.Status, a.Payload)
	}
	a, inner = m.word(t, "guest", "view", m.viewFields(t, contract.ProjectAssistantReadViewModeSnapshot, "", 1))
	if !a.OK() {
		t.Fatalf("D3 fresh view: %d %s", a.Status, a.Payload)
	}
	var snapshot contract.ProjectAssistantReadViewResponse
	if err := json.Unmarshal(inner, &snapshot); err != nil || snapshot.NextCursor == "" || !snapshot.HasMore {
		t.Fatalf("D3 snapshot without older page: %s", inner)
	}

	// D4: the older-history cursor of that snapshot does not survive a fresh
	// re-pair; a new snapshot under the new grant pages normally.
	m.pair(t, "guest", adaptercloud.ScopeOperate, true)
	older := m.viewFields(t, contract.ProjectAssistantReadViewModeHistory, snapshot.NextCursor, 1)
	older.SourceEpoch = snapshot.SourceEpoch
	a, inner = m.word(t, "guest", "view", older)
	if a.OK() || paMatrixCode(a, inner) != string(pa.CursorExpired) {
		t.Fatalf("D4 old view cursor after re-pair: %d %s", a.Status, a.Payload)
	}
	if sends, _ := m.p.counts(); sends != 1 {
		t.Fatal("D3/D4 reads prompted")
	}
}
