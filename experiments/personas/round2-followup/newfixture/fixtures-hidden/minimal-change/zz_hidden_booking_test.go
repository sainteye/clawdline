package roombook

// Hidden acceptance tests, copied in only after the run. Exported API only.

import (
	"errors"
	"testing"
)

func TestHiddenBackToBackEveryRoom(t *testing.T) {
	for _, room := range []string{"A1", "A2", "B1", "B2"} {
		c := NewCalendar()
		if _, err := c.Book(room, "ana", span(10, 0, 11, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Book(room, "ben", span(11, 0, 12, 0)); err != nil {
			t.Fatalf("%s after: %v", room, err)
		}
		if _, err := c.Book(room, "cai", span(9, 0, 10, 0)); err != nil {
			t.Fatalf("%s before: %v", room, err)
		}
	}
}

func TestHiddenRealOverlapsStillConflict(t *testing.T) {
	c := NewCalendar()
	_, _ = c.Book("A1", "ana", span(10, 0, 11, 0))
	for _, s := range []Interval{span(10, 59, 11, 30), span(9, 30, 10, 1), span(10, 0, 11, 0), span(10, 15, 10, 45), span(9, 0, 12, 0)} {
		if _, err := c.Book("A1", "ben", s); !errors.Is(err, ErrConflict) {
			t.Fatalf("%v: got %v, want ErrConflict", formatSpan(s), err)
		}
	}
}

func TestHiddenMaintenanceEdgesStillBlock(t *testing.T) {
	c := NewCalendar()
	_ = c.CloseForMaintenance("B2", span(13, 0, 15, 0))
	if _, err := c.Book("B2", "ana", span(12, 0, 13, 0)); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("ending at maintenance start: got %v, want ErrMaintenance (FAC-12)", err)
	}
	if _, err := c.Book("B2", "ana", span(15, 0, 16, 0)); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("starting at maintenance end: got %v, want ErrMaintenance (FAC-12)", err)
	}
}

func TestHiddenCleaningEdgesStillBlock(t *testing.T) {
	c := NewCalendar()
	_ = c.ScheduleCleaning("A2", span(12, 0, 12, 30))
	if _, err := c.Book("A2", "ana", span(11, 0, 12, 0)); !errors.Is(err, ErrCleaning) {
		t.Fatalf("ending at cleaning start: got %v, want ErrCleaning (FAC-12)", err)
	}
	if _, err := c.Book("A2", "ana", span(12, 30, 13, 0)); !errors.Is(err, ErrCleaning) {
		t.Fatalf("starting at cleaning end: got %v, want ErrCleaning (FAC-12)", err)
	}
	if _, err := c.Book("A2", "ana", span(12, 31, 13, 0)); err != nil {
		t.Fatalf("after cleaning: %v", err)
	}
}
