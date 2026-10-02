package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// A write that holds the one write connection — here a callback parked on a
// channel, on the live machine a commit whose fsync waited on a loaded disk —
// must not hold up the reads a Session snapshot makes. Before the read pool
// they queued for the same connection, timed out together inside the
// snapshot's two-second budget, and every row's work, title and attention
// turned unknown for that snapshot.
func TestReadsDoNotQueueBehindAHeldWrite(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	holding := make(chan struct{})
	release := make(chan struct{})
	wrote := make(chan error, 1)
	go func() {
		wrote <- s.write(context.Background(), func(tx *sql.Tx) (int64, error) {
			close(holding)
			<-release
			return 0, nil
		})
	}()
	<-holding
	defer func() {
		close(release)
		if err := <-wrote; err != nil {
			t.Errorf("the held write: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := s.OpenSessionResponsibilities(ctx); err != nil {
		t.Fatalf("Session responsibilities while a write is held: %v", err)
	}
	if _, err := s.OpenHumanInterventionCounts(ctx); err != nil {
		t.Fatalf("attention counts while a write is held: %v", err)
	}
	if _, err := s.EventCount(ctx, ""); err != nil {
		t.Fatalf("event count while a write is held: %v", err)
	}
}

// A reader on the pool sees a write as soon as it has committed: the pool is
// another connection to the same WAL file, not a copy.
func TestAReadAfterACommittedWriteSeesIt(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	before, err := s.EventCount(ctx, "read_pool_probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, Event{Kind: "read_pool_probe", Subject: "x"}); err != nil {
		t.Fatal(err)
	}
	after, err := s.EventCount(ctx, "read_pool_probe")
	if err != nil || after != before+1 {
		t.Fatalf("read after commit: %d then %d, %v", before, after, err)
	}
}

// The pool cannot write: a statement that would is refused by SQLite itself,
// so a write that skipped s.write cannot slip past the writers' queue.
func TestTheReadPoolRefusesWrites(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.rd.ExecContext(context.Background(),
		`INSERT INTO events (at, kind, subject, payload) VALUES (1, 'x', 'y', '')`); err == nil {
		t.Fatal("the read-only pool accepted a write")
	}
}
