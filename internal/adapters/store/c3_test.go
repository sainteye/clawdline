package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// limits N2: a write the database refuses is counted, turns the store.db row
// failing, and that is the fact /v1/health turns on — where it used to be a
// `_ =` at every caller and nothing anywhere.
//
// The failure is a real one: SQLite's own page ceiling, lowered to the pages
// the database already has, answers the next insert SQLITE_FULL exactly as a
// full disk does, through the same Append every caller uses.
func TestAWriteTheDatabaseRefusesIsCountedAndTurnsTheRowFailing(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	var mu sync.Mutex
	var heard []bool
	s.ObserveFailing(func(failing bool) {
		mu.Lock()
		heard = append(heard, failing)
		mu.Unlock()
	})

	if err := s.Append(ctx, Event{Kind: "c3.before"}); err != nil {
		t.Fatal(err)
	}
	var pages int
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA max_page_count = %d`, pages)); err != nil {
		t.Fatal(err)
	}
	big := Event{Kind: "c3.full", Payload: []byte(`"` + strings.Repeat("x", 256<<10) + `"`)}
	for i := 0; i < 2; i++ {
		if err := s.Append(ctx, big); err == nil {
			t.Fatal("an insert past the page ceiling succeeded; nothing was injected")
		}
	}

	st := s.Stats()
	if st.StorageFailures != 2 || !st.Failing || st.StorageErr == "" {
		t.Fatalf("after two refused writes: %+v", st)
	}
	r := s.Reading(dir)
	if !r.Known || !r.Failing || r.Counters.WriteErrors != 2 {
		t.Fatalf("store.db reads %+v; want known, failing, 2 write errors", r)
	}
	row := entry(t, capacity.StoreDB)
	if !capacity.Exhausted(row, capacity.OK, r) {
		t.Fatal("a failing evidence row does not turn health red")
	}

	// The database has room again: the next write commits and the row stops
	// failing, while the count of what was lost stays.
	if _, err := s.db.ExecContext(ctx, `PRAGMA max_page_count = 1073741823`); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, Event{Kind: "c3.after"}); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.Failing || st.StorageFailures != 2 {
		t.Fatalf("after a write committed again: %+v", st)
	}
	if r := s.Reading(dir); r.Failing || r.Counters.WriteErrors != 2 {
		t.Fatalf("after recovery store.db reads %+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 2 || !heard[0] || heard[1] {
		t.Fatalf("the observer heard %v; want the turn to failing and the turn back, once each", heard)
	}
}

// A writer that lost a race wrote nothing and may try again; that is
// contention, not a disk that cannot be written, and it must not turn
// /v1/health red.
func TestContentionIsNotAStorageFailure(t *testing.T) {
	for _, err := range []error{ErrBusy, ErrConflict, ErrNestedWrite, context.Canceled,
		fmt.Errorf("wrapped: %w", ErrBusy), errors.New("a store's own refusal")} {
		if storageFailure(err) {
			t.Errorf("%v counted as a storage failure", err)
		}
	}
}

func entry(t *testing.T, name string) capacity.Entry {
	t.Helper()
	for _, e := range capacity.Register() {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("%s is not registered", name)
	return capacity.Entry{}
}
