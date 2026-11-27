package store

import (
	"database/sql"
	"log"
	"strconv"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A Bot turn's process — streamed reply and thought chunks, tool steps — and
// the receipts of the events it published are needed only while that turn is
// its Bot's active turn; the Bot store deletes them in the commit that ends
// the turn, and keeps one roster journal row per Bot
// (internal/adapters/projectassistantstore/process_retention.go). Stores
// written before that kept every chunk three times for good: on 2026-11-26
// one machine's store.db passed its 1 GiB limit with 271,244 chunk events,
// 277,723 receipts and 200,118 roster rows holding 47 MB of text.
//
// Open deletes what those stores left behind for up to
// BotProcessOpenBudgetLimit, and gives the space back with VACUUM once nothing
// is left; the maintenance clock (MaintainEvery) deletes the rest without
// VACUUM, so a later Open gives that space back.
// The Bot store depends on this package, so the backlog is cleared here with
// the same rules; a store with no Bot tables has nothing to clear.
const (
	// BotProcessPruneBatchLimit is how many rows one delete of the backlog
	// removes, so each transaction holds the write lock for a moment.
	BotProcessPruneBatchLimit = 5_000
	// BotProcessVacuumFreeLimit is how many free bytes in the database file
	// make Open give the space back with VACUUM. The database has auto_vacuum
	// off, so deleted rows only leave free pages behind; below this a VACUUM
	// costs more time than the space it would return.
	BotProcessVacuumFreeLimit = 64 << 20
	// BotProcessOpenBudgetLimit is how long Open spends deleting before it
	// leaves the rest to the maintenance clock, so a restart stays well inside
	// the updater's health wait (60 s on macOS, 30 s for the Linux deploy).
	// On the 1.1 GB store above, deleting took 27 s and VACUUM 3.5 s.
	BotProcessOpenBudgetLimit = 15 * time.Second
)

// botProcessVacuumFree is BotProcessVacuumFreeLimit, which a test raises.
var botProcessVacuumFree int64 = BotProcessVacuumFreeLimit

var botProcessRetained = "'" + strings.Join(pa.RetainedTraceCategories, "','") + "'"

// botProcessBacklog lists the deletes: process traces of turns that are not
// their Bot's active turn (never the machine's newest event, so no reader's
// frontier moves back), the receipts of those turns' runtime events, and
// every roster journal row but each Bot's newest.
var botProcessBacklog = []struct{ what, statement string }{
	{"process traces", `DELETE FROM project_assistant_events WHERE seq IN (
 SELECT e.seq FROM project_assistant_events e LEFT JOIN project_assistant_bots b ON b.machine=e.machine AND b.bot=e.bot
 WHERE json_extract(CAST(e.data AS TEXT),'$.kind')='trace_updated'
 AND coalesce(json_extract(CAST(e.data AS TEXT),'$.trace.category'),'') NOT IN (` + botProcessRetained + `)
 AND coalesce(json_extract(CAST(e.data AS TEXT),'$.trace.turn_id'),'') IS NOT coalesce(json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id'),'')
 AND e.seq<(SELECT max(seq) FROM project_assistant_events m WHERE m.machine=e.machine)
 LIMIT ?)`},
	{"runtime event receipts", `DELETE FROM project_assistant_receipts WHERE rowid IN (
 SELECT r.rowid FROM project_assistant_receipts r LEFT JOIN project_assistant_bots b ON b.machine=r.machine AND b.bot=r.bot
 WHERE r.operation='send' AND substr(r.command_key,1,` + strconv.Itoa(len(pa.RuntimeEventCommandPrefix)) + `)='` + pa.RuntimeEventCommandPrefix + `'
 AND (coalesce(json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id'),'')=''
  OR substr(r.command_key,1,` + strconv.Itoa(len(pa.RuntimeEventCommandPrefix)) + `+length(json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id'))+1)
   <>'` + pa.RuntimeEventCommandPrefix + `'||json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id')||'-')
 LIMIT ?)`},
	{"roster journal rows", `DELETE FROM project_assistant_roster_changes WHERE seq IN (
 SELECT c.seq FROM project_assistant_roster_changes c
 WHERE c.seq<(SELECT max(seq) FROM project_assistant_roster_changes n WHERE n.machine=c.machine AND n.bot=c.bot)
 LIMIT ?)`},
}

// BotProcessCleanup is what one backlog cleanup did.
type BotProcessCleanup struct {
	Deleted map[string]int64
	// Finished is whether nothing was left to delete.
	Finished  bool
	FreeBytes int64
	Vacuumed  bool
	VacuumErr error
	Took      time.Duration
}

// clearBotProcessBacklog deletes the backlog in batches until it is gone or
// budget (when positive) has passed. With vacuum, a finished cleanup then
// runs VACUUM when at least BotProcessVacuumFreeLimit bytes are free. Like the
// picture move's VACUUM that runs only in Open, before anything else in this
// process uses the store; a CLI opening the file meanwhile waits busy_timeout
// and is told the store is busy. A failed VACUUM keeps the space and is only
// logged.
func clearBotProcessBacklog(db *sql.DB, budget time.Duration, vacuum bool) (BotProcessCleanup, error) {
	out := BotProcessCleanup{Deleted: map[string]int64{}}
	start := time.Now()
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN
 ('project_assistant_events','project_assistant_receipts','project_assistant_roster_changes','project_assistant_bots')`).Scan(&tables); err != nil {
		return out, err
	}
	if tables != 4 {
		return out, nil
	}
	out.Finished = true
	for _, step := range botProcessBacklog {
		for {
			if budget > 0 && time.Since(start) > budget {
				out.Finished = false
				break
			}
			res, err := db.Exec(step.statement, BotProcessPruneBatchLimit)
			if err != nil {
				return out, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return out, err
			}
			out.Deleted[step.what] += n
			if n < BotProcessPruneBatchLimit {
				break
			}
		}
	}
	if !out.Finished || !vacuum {
		out.Took = time.Since(start)
		return out, nil
	}
	var pages, size int64
	if err := db.QueryRow(`PRAGMA freelist_count`).Scan(&pages); err != nil {
		return out, err
	}
	if err := db.QueryRow(`PRAGMA page_size`).Scan(&size); err != nil {
		return out, err
	}
	out.FreeBytes = pages * size
	if out.FreeBytes >= botProcessVacuumFree {
		if _, err := db.Exec(`VACUUM`); err != nil {
			out.VacuumErr = err
		} else {
			out.Vacuumed = true
			_, _ = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		}
	}
	out.Took = time.Since(start)
	return out, nil
}

// openBotProcessBacklog never stops Open: a failed cleanup leaves rows that
// nothing reads and is tried again at the next Open.
// openUpkeep is what Open does to the database after its schema: move the
// pictures still in it out to files, then clear finished Bot turns' process.
func openUpkeep(db *sql.DB, images *imageFiles) error {
	if err := openReferenceImages(db, images); err != nil {
		return err
	}
	openBotProcessBacklog(db)
	return nil
}

func openBotProcessBacklog(db *sql.DB) {
	logBotProcessCleanup(clearBotProcessBacklog(db, BotProcessOpenBudgetLimit, true))
}

// maintainBotProcessBacklog deletes what Open's budget left, without VACUUM.
func (s *Store) maintainBotProcessBacklog() {
	logBotProcessCleanup(clearBotProcessBacklog(s.db, 0, false))
}

func logBotProcessCleanup(out BotProcessCleanup, err error) {
	if err != nil {
		log.Printf("store: clearing finished Bot turns' process failed: %v", err)
	}
	var deleted int64
	for _, n := range out.Deleted {
		deleted += n
	}
	if deleted > 0 || out.Vacuumed || out.VacuumErr != nil {
		log.Printf("store: cleared finished Bot turns' process: %d trace(s), %d receipt(s), %d roster row(s); finished=%v, %d bytes free, vacuumed=%v in %s",
			out.Deleted["process traces"], out.Deleted["runtime event receipts"], out.Deleted["roster journal rows"], out.Finished, out.FreeBytes, out.Vacuumed, out.Took.Round(time.Millisecond))
	}
	if out.VacuumErr != nil {
		log.Printf("store: the database keeps the space finished Bot turns took: VACUUM: %v", out.VacuumErr)
	}
}
