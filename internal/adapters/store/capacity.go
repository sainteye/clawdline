package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// DBFile is the store's database, under the state directory.
const DBFile = "clawdline.sqlite3"

// DBReading is the `store.db` row of the capacity register: the database with
// its -wal and -shm beside it, and the free space on the disk it is on.
//
// Three stats and a statfs; nothing is opened. The database file itself must
// be there — a store that is open has one — so its absence is an unknown
// reading, not an empty store. A -wal or -shm that is not there is zero bytes:
// SQLite removes both on a clean close.
//
// The row is evidence (docs/limits.md §4.2): the broker's task records and
// landings are in this file. Its limit is set where ordinary use never reaches
// it, so full means something is wrong.
func DBReading(dir string) capacity.Reading {
	path := filepath.Join(dir, DBFile)
	var used int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			return capacity.Unmeasured(err.Error())
		}
		if !info.Mode().IsRegular() {
			return capacity.Unmeasured(fmt.Sprintf("%s is not a plain file", path+suffix))
		}
		used += info.Size()
	}
	r := capacity.Reading{Known: true, Used: used}
	if free, err := diskFree(dir); err == nil {
		r.DiskFree, r.HasDiskFree = free, true
	} else {
		r.Note = "free disk space could not be read: " + err.Error()
	}
	return r
}

// Reading is DBReading with what this handle knows about its own writes
// (limits N2): every write the database refused for a storage reason is a
// write error, and the row is failing while the newest write that reached the
// database was one. The file can be measured perfectly well while nothing can
// be written to it — a full disk is exactly that — so the write account is
// added to a reading that is known, and it is the part health turns on.
func (s *Store) Reading(dir string) capacity.Reading {
	r := DBReading(dir)
	st := s.Stats()
	r.Counters.WriteErrors = st.StorageFailures
	if !st.StorageErrAt.IsZero() {
		r.Counters.LastActionAt = st.StorageErrAt
	}
	if st.Failing {
		r.Failing = true
		why := "the newest write was refused: " + st.StorageErr
		if r.Known {
			r.Note = strings.TrimPrefix(r.Note+"; "+why, "; ")
		} else {
			r.Err += "; " + why
		}
	}
	return r
}
