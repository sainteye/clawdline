package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// A notice its root has read carries observed_at and no ACK, with
// next_retry_at 0. That row is not due, ever — read as the due query reads
// state and deadline alone, it would be due on every pass and fill the
// window the unread ones are taken from. A delivered row nobody has read is
// due on its deadline, across a reopen, exactly as a row written before
// observed_at meant anything but an ACK.
func TestAReadNoticeIsNotDueAndAnUnreadOneStillIsAfterAReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	rows := []BrokerNotice{
		{ID: "n-unread", TaskID: "t-unread", State: "delivered", Attempts: 1, CreatedAt: at,
			DeliveredAt: at, NextRetryAt: at.Add(2 * time.Minute)},
		{ID: "n-read", TaskID: "t-read", State: "delivered", Attempts: 1, CreatedAt: at,
			DeliveredAt: at, ObservedAt: at.Add(time.Minute)},
	}
	if err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		for _, n := range rows {
			if err := insertNotice(ctx, tx, n); err != nil {
				return 0, err
			}
		}
		return int64(len(rows)), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	due, err := s.DueBrokerNotices(ctx, at.Add(time.Hour), 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "n-unread" {
		t.Fatalf("due after the reopen: %+v", due)
	}
	if early, _ := s.DueBrokerNotices(ctx, at.Add(time.Minute), 32); len(early) != 0 {
		t.Fatalf("due before its deadline: %+v", early)
	}
	counts, err := s.BrokerNoticeCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Delivered != 2 || !counts.OldestPending.Equal(at) {
		t.Fatalf("counts: %+v", counts)
	}
	read, err := s.BrokerNotice(ctx, "t-read")
	if err != nil || !read.ObservedAt.Equal(at.Add(time.Minute)) || !read.AcknowledgedAt.IsZero() {
		t.Fatalf("the read row as stored: %+v %v", read, err)
	}
}
