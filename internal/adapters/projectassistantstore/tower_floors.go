package projectassistantstore

import (
	"context"
	"database/sql"
	"errors"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Tower floor placements are a person's view preference, outside every Bot
// aggregate. One version row per machine fences the whole document.
const towerFloorsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_tower_floors (
 machine TEXT NOT NULL, project TEXT NOT NULL,
 floor INTEGER NOT NULL CHECK(floor>=0), PRIMARY KEY(machine,project))`

const towerFloorVersionsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_tower_floor_versions (
 machine TEXT PRIMARY KEY, version INTEGER NOT NULL CHECK(version>0))`

type floorReader interface {
	reader
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readTowerFloors(ctx context.Context, r floorReader, machine string) (pa.TowerFloors, error) {
	out := pa.TowerFloors{Floors: []pa.TowerFloor{}}
	err := r.QueryRowContext(ctx, `SELECT version FROM project_assistant_tower_floor_versions WHERE machine=?`, machine).Scan(&out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	rows, err := r.QueryContext(ctx, `SELECT project,floor FROM project_assistant_tower_floors WHERE machine=? ORDER BY project LIMIT ?`, machine, pa.MaxTowerFloorProjects+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var f pa.TowerFloor
		if err = rows.Scan(&f.ProjectID, &f.Floor); err != nil {
			return out, err
		}
		out.Floors = append(out.Floors, f)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Floors) > pa.MaxTowerFloorProjects {
		return out, failure(pa.CapacityFull, "tower_floors")
	}
	return out, nil
}

// TowerFloors returns this machine's placements; a machine that never placed
// a Project answers version 0 with none.
func (s *Store) TowerFloors(ctx context.Context, machine string) (pa.TowerFloors, error) {
	return readTowerFloors(ctx, s.db, machine)
}

// SaveTowerFloor is a persistence seam, not authentication: the caller checks
// the source's write authority before and after it.
func (s *Store) SaveTowerFloor(ctx context.Context, w pa.TowerFloorWrite) (pa.TowerFloors, error) {
	if err := pa.ValidateTowerFloorWrite(w); err != nil {
		return pa.TowerFloors{}, err
	}
	unlock := s.lock(pa.BotKey{MachineID: w.MachineID})
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.TowerFloors{}, err
	}
	defer tx.Rollback()
	current, err := readTowerFloors(ctx, tx, w.MachineID)
	if err != nil {
		return current, err
	}
	at := -1
	for i, f := range current.Floors {
		if f.ProjectID == w.Floor.ProjectID {
			at = i
		}
	}
	// A repeated write whose reply was lost finds its own effect: answer it
	// rather than calling the person's own change stale.
	if at >= 0 && current.Floors[at].Floor == w.Floor.Floor {
		return current, nil
	}
	if current.Version != w.ExpectedVersion {
		return current, failure(pa.StaleVersion, "tower_floors_version")
	}
	if at < 0 && len(current.Floors) >= pa.MaxTowerFloorProjects {
		return current, failure(pa.CapacityFull, "tower_floors")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_tower_floors(machine,project,floor) VALUES(?,?,?) ON CONFLICT(machine,project) DO UPDATE SET floor=excluded.floor`, w.MachineID, w.Floor.ProjectID, w.Floor.Floor); err != nil {
		return current, err
	}
	// The version moves only from the one this transaction read, so a
	// concurrent writer through another Store cannot be overwritten silently.
	var moved sql.Result
	if current.Version == 0 {
		moved, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_tower_floor_versions(machine,version) VALUES(?,1) ON CONFLICT(machine) DO NOTHING`, w.MachineID)
	} else {
		moved, err = tx.ExecContext(ctx, `UPDATE project_assistant_tower_floor_versions SET version=version+1 WHERE machine=? AND version=?`, w.MachineID, current.Version)
	}
	if err != nil {
		return current, err
	}
	if n, e := moved.RowsAffected(); e != nil || n != 1 {
		return current, errors.Join(failure(pa.StaleVersion, "tower_floors_version"), e)
	}
	next, err := readTowerFloors(ctx, tx, w.MachineID)
	if err != nil {
		return current, err
	}
	if err = tx.Commit(); err != nil {
		return current, err
	}
	return next, nil
}
