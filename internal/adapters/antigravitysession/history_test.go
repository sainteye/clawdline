package antigravitysession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func persistentFixture(t *testing.T) (*Host, session.Session, Options) {
	t.Helper()
	t.Setenv("CLW_SESSION_PEER", "yes")
	t.Setenv("CLW_RESTORE_PEER", "yes")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Executable: exe, AccountDir: dir, StateDir: filepath.Join(dir, "state"), Args: []string{"-test.run=^TestOwnedPeer$"}}
	h := reopenHost(t, opts)
	row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir, Name: "Synthetic history"})
	if err != nil {
		t.Fatal(err)
	}
	return h, row, opts
}
func reopenHost(t *testing.T, opts Options) *Host {
	t.Helper()
	h, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	return h
}
func stopHost(t *testing.T, h *Host) {
	t.Helper()
	if err := h.Shutdown(); err != nil {
		t.Fatal(err)
	}
}
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(raw)
}
func idleHost(t *testing.T, h *Host, row session.Session) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := h.lookup(row)
		if err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		busy := c.busy
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("host job did not settle")
}
func forceLoad(t *testing.T, h *Host, row session.Session) {
	t.Helper()
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.uncertain = true
	if err := c.saveState(); err != nil {
		t.Fatal(err)
	}
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client != nil {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryReopenReadsWithoutProcessAndPagesEveryOrdinal(t *testing.T) {
	h, row, opts := persistentFixture(t)
	c, _ := h.lookup(row)
	c.mu.Lock()
	for i := 0; i < 90; i++ {
		c.append(contract.TranscriptEntry{Role: "user", Text: strings.Repeat("x", 20000), At: int64(i + 1)})
	}
	if c.storeErr != nil {
		t.Fatal(c.storeErr)
	}
	c.mu.Unlock()
	starts := readFile(t, filepath.Join(opts.AccountDir, "starts"))
	stopHost(t, h)
	h = reopenHost(t, opts)
	inv, err := h.Inventory(context.Background())
	if err != nil || len(inv.Sessions) != 1 {
		t.Fatal(inv, err)
	}
	got := inv.Sessions[0]
	if processes, _ := h.Stats(); processes != 0 {
		t.Fatal("saved history consumes a process slot")
	}
	if connections := h.HistoryConnections(); connections != MaxHistoryConnections {
		t.Fatal("history connection measurement", connections)
	}
	if got.ID != row.ID || got.ConversationID != row.ConversationID || got.CWD != row.CWD || got.PID != 0 {
		t.Fatal(got)
	}
	var before int64
	revision := ""
	count := 0
	ids := map[string]bool{}
	want := int64(90)
	for {
		page, err := h.TranscriptPage(context.Background(), got, 7, before, revision)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page.Entries)
		if len(raw) > MaxViewBytes {
			t.Fatal("response budget exceeded")
		}
		for i := len(page.Entries) - 1; i >= 0; i-- {
			if page.Entries[i].ID == "" || ids[page.Entries[i].ID] {
				t.Fatal("missing or duplicate entry identity")
			}
			ids[page.Entries[i].ID] = true
			if page.Entries[i].At != want {
				t.Fatalf("ordinal lost: got %d want %d", page.Entries[i].At, want)
			}
			want--
			count++
		}
		if page.NextBefore == 0 {
			break
		}
		if before != 0 && page.NextBefore >= before {
			t.Fatal("cursor did not decrease")
		}
		before = page.NextBefore
		revision = page.Signature
	}
	if count != 90 || readFile(t, filepath.Join(opts.AccountDir, "starts")) != starts {
		t.Fatalf("count=%d; reading started a process", count)
	}
	t.Logf("reopened %d entries / %d text bytes without a process", count, count*20000)
}

func TestHistoryStreamingAndToolDeltasKeepStableOrdinals(t *testing.T) {
	h, row, _ := persistentFixture(t)
	c, _ := h.lookup(row)
	emit := func(u any) {
		t.Helper()
		frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": row.ConversationID, "update": u}})
		if err := c.update(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
	}
	chunk := strings.Repeat("界", 200000)
	emit(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": chunk}})
	first, _ := h.Transcript(row, 10)
	emit(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": chunk}})
	latest, err := h.TranscriptPage(context.Background(), row, 1, 0, "")
	if err != nil || latest.NextBefore == 0 || len(latest.Entries) != 1 {
		t.Fatal(latest, err)
	}
	old, err := h.TranscriptPage(context.Background(), row, 1, latest.NextBefore, first.Signature)
	if err != nil || len(old.Entries) != 1 || old.Entries[0].ID != first.Entries[0].ID {
		t.Fatal(old, err)
	}
	if old.Entries[0].Text+latest.Entries[0].Text != chunk+chunk || !utf8.ValidString(old.Entries[0].Text) || !utf8.ValidString(latest.Entries[0].Text) {
		t.Fatal("large streamed text lost or split inside UTF-8")
	}
	for _, p := range []contract.TranscriptPage{old, latest} {
		raw, _ := json.Marshal(p.Entries)
		if len(raw) > MaxViewBytes {
			t.Fatalf("page exceeds budget: %d", len(raw))
		}
	}
	emit(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "durable-tool", "title": "Read original", "kind": "read", "status": "in_progress"})
	tool, _ := h.Transcript(row, 1)
	for i := 0; i < 70; i++ {
		c.mu.Lock()
		c.append(contract.TranscriptEntry{Role: "user", Text: strings.Repeat("x", 20000)})
		c.mu.Unlock()
	}
	emit(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "durable-tool", "status": "completed", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": "persisted result"}}}})
	c.mu.Lock()
	ordinal, e, err := c.readEntry(0, "durable-tool")
	c.mu.Unlock()
	if err != nil || ordinal == 0 || e.ID != tool.Entries[0].ID || e.Activity.Status != "completed" || e.Activity.Title != "Read original" || e.Activity.Result != "persisted result" {
		t.Fatal(e, err)
	}
	reread, err := h.TranscriptPage(context.Background(), row, 1, tool.ReadBefore, tool.Signature)
	if err != nil || len(reread.Entries) != 1 || reread.Entries[0].ID != tool.Entries[0].ID || reread.Entries[0].Activity.Result != "persisted result" {
		t.Fatal("exact old tool page did not refresh", reread, err)
	}
	if _, bytes := h.Stats(); bytes > MaxViewBytes {
		t.Fatal("unbounded hot projection")
	}
}

func TestHistoryFailedReplayWriteKeepsPublishedGeneration(t *testing.T) {
	h, row, opts := persistentFixture(t)
	c, _ := h.lookup(row)
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "user", Text: "published"})
	c.mu.Unlock()
	before, _ := h.Transcript(row, 10)
	forceLoad(t, h, row)
	if _, err := h.store.db.Exec(`CREATE TRIGGER reject_replay BEFORE INSERT ON entries BEGIN SELECT RAISE(FAIL,'synthetic disk write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Resume(context.Background(), row.CWD, row.ConversationID); err == nil {
		t.Fatal("failed replay write published")
	}
	after, err := h.Transcript(row, 10)
	if err != nil || after.Signature != before.Signature || len(after.Entries) != 1 || after.Entries[0].Text != "published" {
		t.Fatal(after, err)
	}
	if _, err := h.store.db.Exec(`DROP TRIGGER reject_replay`); err != nil {
		t.Fatal(err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	after, err = h.Transcript(row, 10)
	if err != nil || after.Signature != before.Signature || len(after.Entries) != 1 || after.Entries[0].Text != "published" {
		t.Fatal("failed write changed durable publication", after, err)
	}
	if _, err := h.Resume(context.Background(), row.CWD, row.ConversationID); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryTwoIdentitiesCloseResumeAndProfileFence(t *testing.T) {
	h, first, opts := persistentFixture(t)
	c, _ := h.lookup(first)
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "user", Text: "first only"})
	c.row.Activity.At = time.Unix(2000, 0)
	if err := c.saveState(); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	if err := h.Close(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second, err := h.Open(context.Background(), ports.OpenRequest{Cwd: first.CWD})
	if err != nil {
		t.Fatal(err)
	}
	c, _ = h.lookup(second)
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "user", Text: "second only"})
	c.row.Activity.At = time.Unix(1000, 0)
	if err := c.saveState(); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	if second.ConversationID == first.ConversationID {
		t.Fatal("synthetic identities collided")
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	past, cursor, err := h.Past(context.Background(), first.CWD, "", 1)
	if err != nil || len(past) != 1 || cursor == "" {
		t.Fatal(past, cursor, err)
	}
	if past[0].ID != first.ConversationID {
		t.Fatal("history sorted by UUID instead of latest activity", past)
	}
	older, _, err := h.Past(context.Background(), first.CWD, cursor, 1)
	if err != nil || len(older) != 1 || older[0].ID == past[0].ID {
		t.Fatal(older, err)
	}
	for _, tc := range []struct {
		row  session.Session
		text string
	}{{first, "first only"}, {second, "second only"}} {
		p, err := h.Transcript(tc.row, 10)
		if err != nil || len(p.Entries) != 1 || p.Entries[0].Text != tc.text {
			t.Fatal(p, err)
		}
	}
	if _, err := h.Resume(context.Background(), filepath.Dir(first.CWD), first.ConversationID); err == nil {
		t.Fatal("wrong cwd resumed")
	}
	row, err := h.Resume(context.Background(), first.CWD, first.ConversationID)
	if err != nil || row.ID != first.ID || row.ConversationID != first.ConversationID {
		t.Fatal(row, err)
	}
	again, err := h.Resume(context.Background(), first.CWD, first.ConversationID)
	if err != nil || again.PID != row.PID {
		t.Fatal(again, err)
	}
	if _, err := h.Resume(context.Background(), second.CWD, second.ConversationID); err != nil {
		t.Fatal("second saved conversation could not resume", err)
	}
	if _, err := h.Open(context.Background(), ports.OpenRequest{Cwd: first.CWD}); err == nil {
		t.Fatal("process capacity exceeded")
	}
	stopHost(t, h)
	opts.AccountDir = t.TempDir()
	h = reopenHost(t, opts)
	if _, err := h.Resume(context.Background(), first.CWD, first.ConversationID); err == nil {
		t.Fatal("profile binding crossed")
	}
}

func TestHistoryImportVerifiesExactNativeIdentityAndPersistsReplay(t *testing.T) {
	h, owned, opts := persistentFixture(t)
	if err := h.Close(context.Background(), owned); err != nil {
		t.Fatal(err)
	}
	writeList := func(rows []map[string]string, next string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"sessions": rows, "nextCursor": next})
		if err := os.WriteFile(filepath.Join(opts.AccountDir, "native-list.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	id := "legacy-native-exact"
	for _, tc := range []struct {
		rows       []map[string]string
		next, code string
	}{
		{[]map[string]string{{"sessionId": "legacy-native-exact-other", "cwd": owned.CWD}}, "", "native_identity_not_found"},
		{[]map[string]string{{"sessionId": id, "cwd": filepath.Dir(owned.CWD)}}, "", "native_identity_unverified"},
		{[]map[string]string{}, "next", "native_history_incomplete"},
	} {
		writeList(tc.rows, tc.next)
		if _, err := h.Import(context.Background(), owned.CWD, id); err == nil || err.Error() != "antigravity_session_"+tc.code {
			t.Fatal(err, tc.code)
		}
	}
	if strings.Contains(readFile(t, filepath.Join(opts.AccountDir, "methods")), "session/load") {
		t.Fatal("unverified identity loaded")
	}
	writeList([]map[string]string{{"sessionId": "other", "cwd": owned.CWD}}, "older")
	raw, _ := json.Marshal(map[string]any{"sessions": []map[string]string{{"sessionId": id, "cwd": owned.CWD, "title": "Native legacy conversation"}}})
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "native-list-older.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Resume(context.Background(), owned.CWD, id); err == nil {
		t.Fatal("ordinary resume silently imported")
	}
	row, err := h.Import(context.Background(), owned.CWD, id)
	if err != nil || row.ConversationID != id || row.CWD != owned.CWD || row.Label != "Native legacy conversation" {
		t.Fatal(row, err)
	}
	starts := readFile(t, filepath.Join(opts.AccountDir, "starts"))
	again, err := h.Import(context.Background(), owned.CWD, id)
	if err != nil || again.ID != row.ID || again.PID != row.PID || readFile(t, filepath.Join(opts.AccountDir, "starts")) != starts {
		t.Fatal("repeated import was not idempotent", again, err)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	page, err := h.Transcript(row, 10)
	if err != nil || len(page.Entries) != 4 {
		t.Fatal(page, err)
	}
	if readFile(t, filepath.Join(opts.AccountDir, "starts")) != starts || readFile(t, filepath.Join(opts.AccountDir, "session-number")) != "1" || readFile(t, filepath.Join(opts.AccountDir, "prompts")) != "" {
		t.Fatal("import or offline reading created a native session or sent a prompt")
	}
	resumed, err := h.Resume(context.Background(), owned.CWD, id)
	if err != nil || resumed.ID != row.ID {
		t.Fatal(resumed, err)
	}
}

func TestHistoryLoadReplacementIsAtomicAndRepeatedTextSurvives(t *testing.T) {
	h, row, opts := persistentFixture(t)
	c, _ := h.lookup(row)
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "user", Text: "old readable"})
	c.mu.Unlock()
	old, _ := h.Transcript(row, 10)
	for _, failure := range []string{"load-error", "auth-error"} {
		forceLoad(t, h, row)
		if err := os.WriteFile(filepath.Join(opts.AccountDir, failure), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Resume(context.Background(), row.CWD, row.ConversationID); err == nil {
			t.Fatal("injected restore failure accepted")
		}
		p, err := h.Transcript(row, 10)
		if err != nil || len(p.Entries) != 1 || p.Entries[0].Text != "old readable" || p.Signature != old.Signature {
			t.Fatal(p, err)
		}
		if err := os.Remove(filepath.Join(opts.AccountDir, failure)); err != nil {
			t.Fatal(err)
		}
		stopHost(t, h)
		h = reopenHost(t, opts)
		p, err = h.Transcript(row, 10)
		if err != nil || len(p.Entries) != 1 || p.Entries[0].Text != "old readable" || p.Signature != old.Signature {
			t.Fatal("failed restore changed durable publication", p, err)
		}
	}
	for i := 0; i < 2; i++ {
		forceLoad(t, h, row)
		var err error
		row, err = h.Resume(context.Background(), row.CWD, row.ConversationID)
		if err != nil {
			t.Fatal(err)
		}
		p, err := h.Transcript(row, 10)
		if err != nil || len(p.Entries) != 4 {
			t.Fatal(p, err)
		}
		if p.Entries[0].Text != "identical user" || p.Entries[2].Text != "identical user" || p.Entries[0].Role != "user" || p.Entries[1].Role != "assistant" {
			t.Fatal(p.Entries)
		}
		if _, err := h.TranscriptPage(context.Background(), row, 1, 2, old.Signature); err == nil {
			t.Fatal("old generation accepted")
		}
		old = p
	}
	if strings.Contains(readFile(t, filepath.Join(opts.AccountDir, "methods")), "session/prompt") {
		t.Fatal("restore sent a prompt")
	}
}

func TestHistoryShutdownResumeThenSendOnceAndUncertainNeverResends(t *testing.T) {
	h, row, opts := persistentFixture(t)
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	stopHost(t, h)
	h = reopenHost(t, opts)
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\n" {
		t.Fatal(got)
	}
	if err := h.Send(context.Background(), row, "second"); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, row)
	p, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, e := range p.Entries {
		if e.Role == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("live native user echo duplicated local acceptance: %+v", p.Entries)
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\nprompt\n" {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(opts.AccountDir, "prompt-uncertain"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_ = h.Send(context.Background(), row, "uncertain")
	deadline := time.Now().Add(3 * time.Second)
	for readFile(t, filepath.Join(opts.AccountDir, "prompts")) != "prompt\nprompt\nprompt\n" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\nprompt\nprompt\n" {
		t.Fatal(got)
	}
	if _, err := h.Transcript(row, 100); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(opts.AccountDir, "prompt-uncertain")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Resume(context.Background(), row.CWD, row.ConversationID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(opts.AccountDir, "prompts")); got != "prompt\nprompt\nprompt\n" {
		t.Fatal("uncertain prompt resent", got)
	}
	methods := readFile(t, filepath.Join(opts.AccountDir, "methods"))
	if !strings.Contains(methods, "session/resume") || !strings.Contains(methods, "session/load") {
		t.Fatal(methods)
	}
}

func TestMetadataLeaseRefusesAClosedConversationAndProtectsItsClient(t *testing.T) {
	h, row, _ := persistentFixture(t)
	c, err := h.lookup(row)
	if err != nil {
		t.Fatal(err)
	}
	client, done, err := h.beginMetadata(c)
	if err != nil || client == nil {
		t.Fatal("metadata admission failed", err)
	}
	if err := h.Close(context.Background(), row); err == nil {
		t.Fatal("close stole an admitted client")
	}
	done()
	if err := h.Close(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if client, done, err := h.beginMetadata(c); err == nil || client != nil || done != nil {
		t.Fatal("closed metadata admission succeeded")
	}
}

func TestFirstRequestSurvivesViewEvictionAndReopen(t *testing.T) {
	h, row, opts := persistentFixture(t)
	c, _ := h.lookup(row)
	c.mu.Lock()
	c.append(contract.TranscriptEntry{Role: "assistant", Text: "Not the request"})
	c.append(contract.TranscriptEntry{Role: "user", Text: " \n\t"})
	c.append(contract.TranscriptEntry{Role: "user", Text: "Original objective"})
	for i := 0; i < 90; i++ {
		c.append(contract.TranscriptEntry{Role: "user", Text: strings.Repeat("x", 20000)})
	}
	if c.storeErr != nil {
		t.Fatal(c.storeErr)
	}
	c.mu.Unlock()
	stopHost(t, h)
	h = reopenHost(t, opts)
	got, err := h.FirstRequest(t.Context(), row)
	if err != nil || got != "Original objective" {
		t.Fatalf("first=%q err=%v", got, err)
	}
	changed := row
	changed.ConversationID = "another-conversation"
	if _, err := h.FirstRequest(t.Context(), changed); err == nil {
		t.Fatal("read another identity")
	}
	if processes, _ := h.Stats(); processes != 0 {
		t.Fatal("history read started a provider")
	}
}
