package roombook

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrConflict    = errors.New("room is already booked")
	ErrMaintenance = errors.New("room is closed for maintenance")
	ErrCleaning    = errors.New("room is being cleaned")
	ErrBadInterval = errors.New("end must be after start")
	ErrTooLong     = errors.New("bookings are limited to 4 hours")
)

// Booking is one confirmed reservation. Its interval is half-open: a booking
// from 10:00 to 11:00 frees the room at 11:00.
type Booking struct {
	ID    int
	Room  string
	Owner string
	When  Interval
}

// Calendar holds the bookings, maintenance windows and cleaning slots of all
// rooms.
type Calendar struct {
	bookings    []Booking
	maintenance map[string][]Interval
	cleaning    map[string][]Interval
	nextID      int
}

func NewCalendar() *Calendar {
	return &Calendar{maintenance: map[string][]Interval{}, cleaning: map[string][]Interval{}, nextID: 1}
}

// Book reserves room for owner during when.
func (c *Calendar) Book(room, owner string, when Interval) (Booking, error) {
	r, err := findRoom(room)
	if err != nil {
		return Booking{}, err
	}
	if !when.End.After(when.Start) {
		return Booking{}, ErrBadInterval
	}
	if minutes(when) > 240 {
		return Booking{}, ErrTooLong
	}
	if err := c.checkMaintenance(r.ID, when); err != nil {
		return Booking{}, err
	}
	if err := c.checkCleaning(r.ID, when); err != nil {
		return Booking{}, err
	}
	// Two bookings conflict when they overlap (see overlaps).
	for _, b := range c.bookings {
		if b.Room == r.ID && overlaps(b.When, when) {
			return Booking{}, fmt.Errorf("%w: %s by %s", ErrConflict, formatSpan(b.When), b.Owner)
		}
	}
	b := Booking{ID: c.nextID, Room: r.ID, Owner: owner, When: when}
	c.nextID = c.nextID + 1
	c.bookings = append(c.bookings, b)
	return b, nil
}

// Cancel removes a booking by id. It reports whether one was removed.
func (c *Calendar) Cancel(id int) bool {
	for i := range c.bookings {
		if c.bookings[i].ID == id {
			c.bookings = append(c.bookings[:i], c.bookings[i+1:]...)
			return true
		}
	}
	return false
}

// DaySchedule lists a room's bookings on the day of t, earliest first.
func (c *Calendar) DaySchedule(room string, t time.Time) []Booking {
	out := []Booking{}
	y, m, d := t.Date()
	for _, b := range c.bookings {
		by, bm, bd := b.When.Start.Date()
		if b.Room == room && by == y && bm == m && bd == d {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.Start.Before(out[j].When.Start) })
	return out
}

func formatSpan(i Interval) string {
	return i.Start.Format("15:04") + "-" + i.End.Format("15:04")
}
