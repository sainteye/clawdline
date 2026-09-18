package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// W2 (docs/design-decisions.md §6), criterion ①: a second store handle on the
// same file — a CLI, a second daemon — writing the row a first one is writing
// gets a typed answer, and never writes over it without anybody knowing.

func seedTask(t *testing.T, s *Store, id, title string) {
	t.Helper()
	record, _ := json.Marshal(map[string]any{"task_id": id, "title": title})
	if _, err := s.CreateBrokerTask(context.Background(), BrokerRow{
		ID: id, Project: "/p", Assistant: "claude", State: "briefed",
		CreatedAt: time.Now(), SecretHash: "h", Record: record,
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func titleOf(t *testing.T, s *Store, id string) string {
	t.Helper()
	row, err := s.BrokerTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(row.Record, &r)
	return r.Title
}

// A writer holding a copy it read before somebody else wrote is told
// ErrConflict, and the other write stands. The upsert this replaced wrote the
// stale copy over it and said nothing.
func TestASecondHandleWithAStaleCopyIsToldConflict(t *testing.T) {
	dir := t.TempDir()
	one, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	const id = "a1111111-1111-4111-8111-111111111111"
	seedTask(t, one, id, "seed")

	stale, err := two.BrokerTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := one.UpdateBrokerTask(context.Background(), id, func(_ *Tx, row BrokerRow) (*BrokerWrite, error) {
		row.Record, _ = json.Marshal(map[string]any{"task_id": id, "title": "first writer"})
		return &BrokerWrite{Row: row}, nil
	}); err != nil {
		t.Fatal(err)
	}
	stale.Record, _ = json.Marshal(map[string]any{"task_id": id, "title": "stale copy"})
	err = two.SaveBrokerTask(context.Background(), stale, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a save from a stale copy answered %v, want ErrConflict", err)
	}
	if got := titleOf(t, one, id); got != "first writer" {
		t.Fatalf("the row says %q: the stale copy was written over the first writer", got)
	}
}

// A writer that meets another handle's write transaction waits busy_timeout
// and is told ErrBusy — typed, counted — and its change is not applied.
func TestASecondHandleThatMeetsAHeldWriteIsToldBusy(t *testing.T) {
	dir := t.TempDir()
	one, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	const id = "a2222222-2222-4222-8222-222222222222"
	seedTask(t, one, id, "seed")

	holding := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := one.UpdateBrokerTask(context.Background(), id, func(_ *Tx, row BrokerRow) (*BrokerWrite, error) {
			close(holding)
			<-release
			row.Record, _ = json.Marshal(map[string]any{"task_id": id, "title": "holder"})
			return &BrokerWrite{Row: row}, nil
		})
		done <- err
	}()
	<-holding
	began := time.Now()
	_, _, err = two.UpdateBrokerTask(context.Background(), id, func(_ *Tx, row BrokerRow) (*BrokerWrite, error) {
		row.Record, _ = json.Marshal(map[string]any{"task_id": id, "title": "second"})
		return &BrokerWrite{Row: row}, nil
	})
	waited := time.Since(began)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the holder's own write failed: %v", err)
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("the second handle answered %v, want ErrBusy", err)
	}
	if waited < 900*time.Millisecond {
		t.Errorf("gave up after %v; busy_timeout is %dms", waited, busyTimeoutMS)
	}
	if two.Stats().Busy != 1 {
		t.Errorf("busy count is %d, want 1", two.Stats().Busy)
	}
	if got := titleOf(t, one, id); got != "holder" {
		t.Fatalf("the row says %q, want the holder's write alone", got)
	}
}

// A store call from inside a write callback is refused at once. With one
// connection per handle it would otherwise wait for itself for ever.
func TestAStoreCallInsideAWriteIsRefusedNotDeadlocked(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const id = "a3333333-3333-4333-8333-333333333333"
	seedTask(t, s, id, "seed")
	var inner error
	_, _, err = s.UpdateBrokerTask(context.Background(), id, func(_ *Tx, row BrokerRow) (*BrokerWrite, error) {
		_, inner = s.BrokerTask(context.Background(), id)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(inner, ErrNestedWrite) {
		t.Fatalf("a read inside the write answered %v, want ErrNestedWrite", inner)
	}
}

// D03's window: the same key inside it is the stored answer, the key reused
// for another request is refused, a full scope refuses rather than evicts,
// and past the window the key is expired and nothing is run.
func TestReceiptsReplayRefuseFillAndExpire(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now()
	policy := ReceiptPolicy{Limit: 2}
	k := ReceiptKey{Scope: "t", Actor: "a", Key: "k1"}
	claim := func(k ReceiptKey, digest string, at time.Time) ReceiptOutcome {
		t.Helper()
		c, err := s.ClaimReceipt(ctx, k, digest, policy, at)
		if err != nil {
			t.Fatal(err)
		}
		return c.Outcome
	}
	if got := claim(k, "d1", now); got != ReceiptNew {
		t.Fatalf("first claim = %s", got)
	}
	if got := claim(k, "d1", now); got != ReceiptPending {
		t.Fatalf("a claim while the first is being answered = %s, want pending", got)
	}
	if err := s.CompleteReceipt(ctx, k, ReceiptAnswer{Status: 200, Body: []byte(`{"at":1}`)}); err != nil {
		t.Fatal(err)
	}
	c, _ := s.ClaimReceipt(ctx, k, "d1", policy, now)
	if c.Outcome != ReceiptReplay || string(c.Answer.Body) != `{"at":1}` || c.Answer.Status != 200 {
		t.Fatalf("resend = %+v, want the stored answer", c)
	}
	if got := claim(k, "d2", now); got != ReceiptMismatch {
		t.Fatalf("the key with another request = %s, want mismatch", got)
	}
	k2 := ReceiptKey{Scope: "t", Actor: "a", Key: "k2"}
	if got := claim(k2, "d", now); got != ReceiptNew {
		t.Fatalf("second key = %s", got)
	}
	full, _ := s.ClaimReceipt(ctx, ReceiptKey{Scope: "t", Actor: "a", Key: "k3"}, "d", policy, now)
	if full.Outcome != ReceiptFull || full.RetryAfter <= 0 {
		t.Fatalf("third key in a scope of two = %+v, want full with a Retry-After", full)
	}
	if got := claim(ReceiptKey{Scope: "other", Actor: "a", Key: "k3"}, "d", now); got != ReceiptNew {
		t.Fatalf("another scope's limit is its own; got %s", got)
	}
	later := now.Add(ReceiptWindow + time.Minute)
	if got := claim(k, "d1", later); got != ReceiptExpired {
		t.Fatalf("resend past the window = %s, want expired", got)
	}
	if got := claim(k, "d1", later); got != ReceiptExpired {
		t.Fatalf("a tombstone answered %s, want expired again", got)
	}
}

// D25: a store written before the prose table had its instructions and
// summary inside each record. Opening it moves them out, whole, and leaves a
// record that is not JSON exactly as it was.
func TestOpeningAnOldStoreMovesItsProseOut(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	long := "do the whole thing " + time.Now().Format(time.RFC3339Nano)
	old := `{"task_id":"a4","title":"t","instructions":"` + long + `","result":{"status":"success","summary":"` + long + `"}}`
	for id, record := range map[string]string{"a4": old, "a5": `{"task_id": "a5", "instr`} {
		if _, err := s.db.Exec(`INSERT INTO broker_tasks (id, project, assistant, state, created_at, updated_at, secret_hash, record)
		   VALUES (?, '/p', 'claude', 'success', 1, 1, 'h', ?)`, id, record); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	row, err := s.BrokerTask(context.Background(), "a4")
	if err != nil {
		t.Fatal(err)
	}
	if row.Texts[TextInstructions] != long || row.Texts[TextSummary] != long {
		t.Fatalf("texts after the move: %q", row.Texts)
	}
	var r struct {
		Instructions string `json:"instructions"`
		Result       struct {
			Summary string `json:"summary"`
			Status  string `json:"status"`
		} `json:"result"`
	}
	if err := json.Unmarshal(row.Record, &r); err != nil || r.Instructions != "" || r.Result.Summary != "" || r.Result.Status != "success" {
		t.Fatalf("the record after the move: %s", row.Record)
	}
	var broken string
	if err := s.db.QueryRow(`SELECT record FROM broker_tasks WHERE id = 'a5'`).Scan(&broken); err != nil || broken != `{"task_id": "a5", "instr` {
		t.Fatalf("an unreadable record was rewritten: %q %v", broken, err)
	}
}
