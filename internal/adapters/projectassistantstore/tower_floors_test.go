package projectassistantstore

import (
	"fmt"
	"reflect"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestTowerFloorsKeepPlacementsVersionedPerMachine(t *testing.T) {
	_, store, _ := testDB(t)
	empty, err := store.TowerFloors(testContext, "machine-a")
	if err != nil || empty.Version != 0 || len(empty.Floors) != 0 || empty.Floors == nil {
		t.Fatalf("a machine that never placed a Project answers version 0 with an empty list: %+v %v", empty, err)
	}
	first, err := store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "machine-a", ExpectedVersion: 0, Floor: pa.TowerFloor{ProjectID: "p2", Floor: 3}})
	if err != nil || first.Version != 1 || !reflect.DeepEqual(first.Floors, []pa.TowerFloor{{ProjectID: "p2", Floor: 3}}) {
		t.Fatalf("first placement: %+v %v", first, err)
	}
	// Another device still holding version 0 is refused, not merged.
	_, err = store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "machine-a", ExpectedVersion: 0, Floor: pa.TowerFloor{ProjectID: "p1", Floor: 1}})
	requireCode(t, err, pa.StaleVersion)
	// A retried write whose reply was lost finds its own effect.
	again, err := store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "machine-a", ExpectedVersion: 0, Floor: pa.TowerFloor{ProjectID: "p2", Floor: 3}})
	if err != nil || !reflect.DeepEqual(again, first) {
		t.Fatalf("a repeated write is answered with the current document: %+v %v", again, err)
	}
	second, err := store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "machine-a", ExpectedVersion: 1, Floor: pa.TowerFloor{ProjectID: "p1", Floor: 1}})
	if err != nil || second.Version != 2 || !reflect.DeepEqual(second.Floors, []pa.TowerFloor{{ProjectID: "p1", Floor: 1}, {ProjectID: "p2", Floor: 3}}) {
		t.Fatalf("second placement: %+v %v", second, err)
	}
	moved, err := store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "machine-a", ExpectedVersion: 2, Floor: pa.TowerFloor{ProjectID: "p2", Floor: 0}})
	if err != nil || moved.Version != 3 || !reflect.DeepEqual(moved.Floors, []pa.TowerFloor{{ProjectID: "p1", Floor: 1}, {ProjectID: "p2", Floor: 0}}) {
		t.Fatalf("moving a Project: %+v %v", moved, err)
	}
	other, err := store.TowerFloors(testContext, "machine-b")
	if err != nil || other.Version != 0 || len(other.Floors) != 0 {
		t.Fatalf("another machine's placements are its own: %+v %v", other, err)
	}
	for _, bad := range []pa.TowerFloorWrite{
		{MachineID: "machine-a", ExpectedVersion: 3, Floor: pa.TowerFloor{ProjectID: "p3", Floor: pa.MaxTowerFloors}},
		{MachineID: "machine-a", ExpectedVersion: 3, Floor: pa.TowerFloor{ProjectID: "p3", Floor: -1}},
		{MachineID: "machine-a", ExpectedVersion: 3, Floor: pa.TowerFloor{ProjectID: "", Floor: 0}},
		{MachineID: "machine-a", ExpectedVersion: 3, Floor: pa.TowerFloor{ProjectID: "bad\nid", Floor: 0}},
		{MachineID: "", ExpectedVersion: 3, Floor: pa.TowerFloor{ProjectID: "p3", Floor: 0}},
	} {
		_, err = store.SaveTowerFloor(testContext, bad)
		requireCode(t, err, pa.Invalid)
	}
}

func TestTowerFloorsRefuseANewProjectAtTheBound(t *testing.T) {
	db, store, _ := testDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < pa.MaxTowerFloorProjects; i++ {
		if _, err = tx.Exec(`INSERT INTO project_assistant_tower_floors(machine,project,floor) VALUES('m',?,0)`, fmt.Sprintf("p%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(`INSERT INTO project_assistant_tower_floor_versions(machine,version) VALUES('m',7)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "m", ExpectedVersion: 7, Floor: pa.TowerFloor{ProjectID: "new", Floor: 1}})
	requireCode(t, err, pa.CapacityFull)
	var rows, version int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM project_assistant_tower_floors WHERE machine='m'),(SELECT version FROM project_assistant_tower_floor_versions WHERE machine='m')`).Scan(&rows, &version); err != nil || rows != pa.MaxTowerFloorProjects || version != 7 {
		t.Fatalf("a refused placement stores nothing: %d rows, version %d, %v", rows, version, err)
	}
	// A Project already placed can still move at the bound.
	moved, err := store.SaveTowerFloor(testContext, pa.TowerFloorWrite{MachineID: "m", ExpectedVersion: 7, Floor: pa.TowerFloor{ProjectID: "p0000", Floor: 2}})
	if err != nil || moved.Version != 8 || moved.Floors[0] != (pa.TowerFloor{ProjectID: "p0000", Floor: 2}) {
		t.Fatalf("move at the bound: %v %v", moved.Version, err)
	}
}
