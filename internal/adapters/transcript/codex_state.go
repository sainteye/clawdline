package transcript

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// codexStateNames reads thread names from Codex's own state database for the
// given ids only. Codex 0.160 can keep a name there without appending it to
// session_index.jsonl, so the database fills names the index lacks.
//
// The database belongs to a running Codex that writes it through WAL, so it is
// opened read-only and query-only, and never created. Any failure — no file,
// a schema without `threads.name`, a corrupt file — answers no names, which
// leaves the session index as the only source, exactly as before.
func codexStateNames(home string, ids []string) map[string]string {
	out := map[string]string{}
	path := codexStatePath(home)
	if path == "" || len(ids) == 0 {
		return out
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String() +
		"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(200)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return out
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.Query(`SELECT id, name FROM threads WHERE name IS NOT NULL AND name != '' AND id IN (?`+
		strings.Repeat(",?", len(ids)-1)+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) != nil {
			return map[string]string{}
		}
		out[id] = name
	}
	if rows.Err() != nil {
		return map[string]string{}
	}
	return out
}

// codexStatePath is the highest-versioned ~/.codex/state_<n>.sqlite. Codex
// bumps the number when it changes the schema and may leave the old file
// behind, so the newest one is the one being written. A name that does not
// parse as a version is not a state database.
func codexStatePath(home string) string {
	matches, _ := filepath.Glob(filepath.Join(home, ".codex", "state_*.sqlite"))
	best, version := "", -1
	for _, path := range matches {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "state_"), ".sqlite"))
		if err != nil || n <= version {
			continue
		}
		best, version = path, n
	}
	return best
}
