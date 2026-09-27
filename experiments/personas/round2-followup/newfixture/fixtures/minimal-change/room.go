package roombook

import (
	"errors"
	"strings"
)

// Room is a bookable meeting room.
type Room struct {
	ID       string
	Name     string
	Capacity int
	Floor    int
}

var ErrUnknownRoom = errors.New("unknown room")

var rooms = []Room{
	{ID: "A1", Name: "Aurora", Capacity: 4, Floor: 1},
	{ID: "A2", Name: "Borealis", Capacity: 8, Floor: 1},
	{ID: "B1", Name: "Cumulus", Capacity: 12, Floor: 2},
	{ID: "B2", Name: "Nimbus", Capacity: 20, Floor: 2},
}

// findRoom looks a room up by id, ignoring case and surrounding spaces.
func findRoom(id string) (Room, error) {
	id = strings.ToUpper(strings.TrimSpace(id))
	var found Room
	var ok bool = false
	for i := 0; i < len(rooms); i++ {
		if rooms[i].ID == id {
			found = rooms[i]
			ok = true
		}
	}
	if ok == false {
		return Room{}, ErrUnknownRoom
	}
	return found, nil
}

// RoomsWithCapacity returns the rooms that seat at least n people.
func RoomsWithCapacity(n int) []Room {
	out := []Room{}
	for _, r := range rooms {
		if r.Capacity >= n {
			out = append(out, r)
		}
	}
	return out
}
