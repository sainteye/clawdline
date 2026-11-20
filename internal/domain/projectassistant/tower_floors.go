package projectassistant

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The Bots tower has four floors (4F, 3F, 2F, B1). A person may place each of
// this machine's Projects on one of them; Projects without a placement are
// spread by the console's default rule.
const MaxTowerFloors = 4

// MaxTowerFloorProjects bounds the placements one machine keeps. A removed
// Project keeps its row so that registering it again does not move its Bots,
// which is why the bound is generous rather than the number of live Projects.
const MaxTowerFloorProjects = 512

// MaxTowerFloorProjectBytes bounds a stored Project identifier.
const MaxTowerFloorProjectBytes = 200

// TowerFloor places one Project of this machine on a tower floor, counted
// from the top: 0 is 4F and MaxTowerFloors-1 is B1.
type TowerFloor struct {
	ProjectID string `json:"project_id"`
	Floor     int    `json:"floor"`
}

// TowerFloors is the machine's whole placement document. Version 0 means
// nothing was ever placed; each change raises it by one.
type TowerFloors struct {
	Version int64        `json:"version"`
	Floors  []TowerFloor `json:"floors"`
}

// TowerFloorWrite places one Project, provided the document is still at
// ExpectedVersion. Repeating a write that already took effect is accepted.
type TowerFloorWrite struct {
	MachineID       string
	ExpectedVersion int64
	Floor           TowerFloor
}

func ValidateTowerFloorWrite(w TowerFloorWrite) error {
	if w.MachineID == "" {
		return fail(Invalid, "machine_id")
	}
	if w.ExpectedVersion < 0 {
		return fail(Invalid, "expected_version")
	}
	id := w.Floor.ProjectID
	if id == "" || len(id) > MaxTowerFloorProjectBytes || !utf8.ValidString(id) || strings.ContainsFunc(id, unicode.IsControl) {
		return fail(Invalid, "project_id")
	}
	if w.Floor.Floor < 0 || w.Floor.Floor >= MaxTowerFloors {
		return fail(Invalid, "floor")
	}
	return nil
}
