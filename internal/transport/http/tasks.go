package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline-go/internal/contract"
)

// tasksList publishes the tasks this daemon knows about.
func (s *Server) tasksList(w http.ResponseWriter, r *http.Request) {
	live, err := s.store.LiveTasks(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	rows := make([]contract.TaskRow, 0, len(live))
	for _, t := range live {
		rows = append(rows, contract.TaskRow{
			TaskID:     t.ID,
			Assistant:  contract.Assistant(t.Assistant),
			ProjectDir: t.ProjectDir,
			Claims:     t.Claims,
			State:      contract.TaskState(t.State),
			CreatedAt:  t.CreatedAt.Unix(),
		})
	}
	writeJSON(w, contract.TaskList{Tasks: rows})
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
