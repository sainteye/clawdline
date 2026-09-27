package roombook

import (
	"errors"
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 3, 9, h, m, 0, 0, time.UTC) }

func span(h1, m1, h2, m2 int) Interval { return Interval{Start: at(h1, m1), End: at(h2, m2)} }

func TestBookAndConflict(t *testing.T) {
	c := NewCalendar()
	if _, err := c.Book("A1", "ana", span(9, 0, 10, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Book("a1 ", "ben", span(9, 30, 10, 30)); !errors.Is(err, ErrConflict) {
		t.Fatalf("overlapping booking: got %v, want ErrConflict", err)
	}
	if _, err := c.Book("A2", "ben", span(9, 30, 10, 30)); err != nil {
		t.Fatalf("other room: %v", err)
	}
}

func TestBackToBackBookings(t *testing.T) {
	c := NewCalendar()
	if _, err := c.Book("B1", "ana", span(10, 0, 11, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Book("B1", "ben", span(11, 0, 12, 0)); err != nil {
		t.Fatalf("a meeting that starts when the previous one ends: got %v, want no error", err)
	}
}

func TestMaintenanceBlocks(t *testing.T) {
	c := NewCalendar()
	_ = c.CloseForMaintenance("B2", span(13, 0, 15, 0))
	if _, err := c.Book("B2", "ana", span(14, 0, 14, 30)); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("got %v, want ErrMaintenance", err)
	}
	if _, err := c.Book("B2", "ana", span(15, 30, 16, 0)); err != nil {
		t.Fatalf("after maintenance: %v", err)
	}
}

func TestUtilisation(t *testing.T) {
	c := NewCalendar()
	_, _ = c.Book("A1", "ana", span(9, 0, 10, 0))
	_, _ = c.Book("A1", "ben", span(13, 0, 15, 0))
	if got := c.Utilisation("A1", at(0, 0)); got != 30 {
		t.Fatalf("utilisation = %d, want 30", got)
	}
}

func TestRejects(t *testing.T) {
	c := NewCalendar()
	if _, err := c.Book("Z9", "ana", span(9, 0, 10, 0)); !errors.Is(err, ErrUnknownRoom) {
		t.Fatalf("got %v", err)
	}
	if _, err := c.Book("A1", "ana", span(10, 0, 9, 0)); !errors.Is(err, ErrBadInterval) {
		t.Fatalf("got %v", err)
	}
	if _, err := c.Book("A1", "ana", span(8, 0, 12, 30)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("got %v", err)
	}
}
