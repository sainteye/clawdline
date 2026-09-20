package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// tasksRoute reads the tasks this daemon knows about, or accepts a new one.
// A new one goes to the broker (orchestrator.go), which is the one way work is
// dispatched on this daemon — a schedule's run included (D07).
func (s *Server) tasksRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.brokerDispatch(w, r)
		return
	}
	s.tasksList(w, r)
}

// tasksList publishes dispatched work in the Swift app's shape
// (OrchestratorTaskList.swift): every unfinished task, then one page of
// finished ones, newest first. The rows are the Swift app's own, read from its
// store, joined by the tasks this daemon dispatched; an id appears once.
//
// The copied console needs the finished ones too: a child whose task ended
// still sits under its root while its tab is open, and the detail header names
// the task that opened a session long after it finished.
func (s *Server) tasksList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cursor, _ := strconv.Atoi(q.Get("cursor"))
	limit := 50
	if v, err := strconv.Atoi(q.Get("limit")); err == nil {
		limit = v
	}
	list, err := s.tasksPayload(r.Context(), cursor, limit)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	if state := q.Get("state"); state != "" {
		kept := list.Tasks[:0]
		for _, row := range list.Tasks {
			if string(row.State) == state {
				kept = append(kept, row)
			}
		}
		list.Tasks = kept
	}
	writeJSON(w, list)
}

// tasksPayload builds the task list the route and the stream's `orchestrator`
// frame both publish.
func (s *Server) tasksPayload(ctx context.Context, cursor, limit int) (contract.TaskList, error) {
	own := []contract.TaskRow{}
	// The broker's own tasks, which live in this daemon's store rather than in
	// the Swift app's. Without this the console would show a child this daemon
	// dispatched only while the Swift app also happened to know about it, which
	// it never does.
	//
	// A broker that could not be read is an error, not an empty section: the
	// list used to drop the whole section silently, which on a phone reads as
	// "your children are gone". And a row it could not decode is listed as
	// `unreadable` — unfinished, so it rides on every page — never skipped
	// (D05 ②).
	records, unreadable, err := s.broker.Records(ctx)
	if err != nil {
		return contract.TaskList{}, err
	}
	screen := s.screen(ctx)
	{
		for _, u := range unreadable {
			created := u.CreatedAt.Unix()
			own = append(own, contract.TaskRow{
				ID:         u.ID,
				TaskID:     u.ID,
				Assistant:  contract.Assistant(u.Assistant),
				ProjectDir: u.Project,
				Claims:     []string{},
				State:      contract.TaskState(orchestrator.StateUnreadable),
				Created:    created,
				CreatedAt:  created,
				Dir:        s.broker.Tasks.Path(u.ID),
			})
		}
		for _, t := range records {
			claims := t.Lease()
			created := t.CreatedAt.Unix()
			row := contract.TaskRow{
				ID:             t.ID,
				TaskID:         t.ID,
				Assistant:      contract.Assistant(t.Assistant),
				ProjectDir:     t.ProjectDir,
				Claims:         claims,
				ClaimsDeclared: t.Claims != nil,
				State:          contract.TaskState(t.State),
				Created:        created,
				CreatedAt:      created,
				Dir:            t.Dir,
				Title:          t.Title,
				Kind:           t.Kind,
				ScheduleID:     t.ScheduleID,
			}
			if !t.FinishedAt.IsZero() {
				row.FinishedAt = t.FinishedAt.Unix()
			}
			ownTaskLinks(&row, t, screen)
			own = append(own, row)
		}
	}

	snap := s.swift.Read()
	rows, page := snap.TaskPage(screen, own, cursor, limit)
	return contract.TaskList{
		At:    time.Now().Unix(),
		Tasks: rows,
		Page:  page,
		Store: storeReading(snap),
	}, nil
}

// storeReading says how the Swift store was read for an answer.
func storeReading(snap swiftstore.Snapshot) contract.StoreReading {
	switch {
	case snap.Source == swiftstore.SourceDisabled:
		return contract.StoreReading(swiftstore.SourceDisabled)
	case snap.Source == swiftstore.SourceAbsent:
		return contract.StoreReading(swiftstore.SourceAbsent)
	case !snap.Known:
		return contract.StoreReadingUnknown
	case snap.Stale:
		return contract.StoreReadingStale
	}
	return contract.StoreReadingCurrent
}

// screen is the sessions on screen, for resolving which tab a task's root is
// in. The session list keeps the last one it built; a task list asked for when
// nobody has read the sessions lately reads the machine itself.
func (s *Server) screen(ctx context.Context) []swiftstore.OnScreen {
	if held := s.lastScreen.Load(); held != nil && time.Since(held.at) < screenMaxAge {
		return held.rows
	}
	inv := s.reading(ctx)
	rows := onScreen(inv.Sessions)
	s.lastScreen.Store(&screenReading{at: time.Now(), rows: rows})
	return rows
}

const screenMaxAge = 10 * time.Second

type screenReading struct {
	at   time.Time
	rows []swiftstore.OnScreen
}

func onScreen(items []session.Session) []swiftstore.OnScreen {
	out := make([]swiftstore.OnScreen, 0, len(items))
	for _, item := range items {
		if !item.IsAssistant() {
			continue
		}
		out = append(out, swiftstore.OnScreen{
			TerminalID:     item.ID,
			Assistant:      string(item.Assistant),
			ConversationID: item.ConversationID,
		})
	}
	return out
}

// strings answers with the localisation catalog, from the console bundle.
//
// The catalog ships beside the console rather than inside this binary because
// it belongs to the screen: the same copy is what the Swift app's console
// reads, copied under web/console/public/strings, and the drift guard compares
// them. Serving a second, independently maintained set of words would give the
// two apps different names for the same thing, which is the failure this whole
// replication exists to avoid.
//
// An empty answer is honest and is what a build with no catalog gets: the
// console has built-in English and uses it, which is a visible degradation
// rather than a fault.
//
// This is the one route with no generated type, because its keys are the
// catalog's own and a schema listing them would be the catalog.
func (s *Server) strings(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "zh-Hant"
	}
	// The name is used as a path segment, so anything that is not a plain tag
	// is refused rather than cleaned: a "fixed" path is a path somebody did not
	// ask for.
	for _, ch := range lang {
		if !(ch == '-' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')) {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that is not a language tag")
			return
		}
	}
	root := WebRoot()
	if root == "" {
		writeJSON(w, map[string]string{})
		return
	}
	body, err := os.ReadFile(filepath.Join(root, "strings", lang+".json"))
	if err != nil {
		writeJSON(w, map[string]string{})
		return
	}
	var catalog map[string]any
	if err := json.Unmarshal(body, &catalog); err != nil {
		writeRefusal(w, http.StatusInternalServerError, "catalog_unreadable", err.Error())
		return
	}
	// The page sets its own lang and dir from these, so they travel with the
	// words rather than being guessed at the other end.
	catalog["lang"] = lang
	catalog["dir"] = "ltr"
	writeJSON(w, catalog)
}
