package store

import (
	"context"
	"testing"
	"time"
)

func TestProjectAssistantReaderBorrowsExistingQueryOnlyPool(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reader := s.ProjectAssistantReader()
	if reader != s.rd || s.ProjectAssistantDatabase() != s.db || reader == s.db {
		t.Fatal("feature opened or borrowed the wrong pool")
	}
	if reader.Stats().MaxOpenConnections != ReadConnectionsLimit {
		t.Fatal("feature changed pool bound")
	}
	if _, err = reader.Exec(`CREATE TABLE forbidden_feature_reader_write (id INTEGER)`); err == nil {
		t.Fatal("borrowed reader accepted SQL write")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var before int
	if err = tx.QueryRow(`SELECT count(*) FROM events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(ctx, Event{Kind: "feature_reader_probe", Subject: "synthetic"}); err != nil {
		t.Fatalf("reader blocked writer commit: %v", err)
	}
	var same int
	if err = tx.QueryRow(`SELECT count(*) FROM events`).Scan(&same); err != nil || same != before {
		t.Fatalf("reader snapshot changed: %d/%d %v", before, same, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var after int
	if err = reader.QueryRowContext(ctx, `SELECT count(*) FROM events`).Scan(&after); err != nil || after != before+1 {
		t.Fatalf("reader did not see committed writer: %d/%d %v", before, after, err)
	}
}
