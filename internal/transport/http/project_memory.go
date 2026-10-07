package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/contract"
)

// A Project's shared memory (docs/project-memory.md): GET and POST
// /v1/projects/{place}/memory list and add; GET, PUT and DELETE
// /v1/projects/{place}/memory/{name} show, update and forget. A session of
// either assistant reaches them with this machine's orchestrator credential
// through `clawdline memory`; a device reads them, and writes only with send.

// memoryRequestBodyLimit is the largest add or update: an entry's body bound
// and room for its other fields and their JSON escaping.
const memoryRequestBodyLimit = 2*memory.EntryBodyLimit + 4<<10

// memoryGroupRequestBodyLimit is the largest group set: a description at its
// bound, JSON-escaped, with room to spare. Past it the body does not parse.
const memoryGroupRequestBodyLimit = 4 << 10

var (
	memoryStoresMu sync.Mutex
	// memoryStores is one store per state directory, so every request and
	// every launch on one daemon writes through the same lock. A test server
	// has a state directory of its own.
	memoryStores = map[string]*memory.Store{}
)

func (s *Server) memoryStore() *memory.Store {
	memoryStoresMu.Lock()
	defer memoryStoresMu.Unlock()
	if st, ok := memoryStores[s.cfg.Dir]; ok {
		return st
	}
	st := memory.New(s.cfg.Dir)
	memoryStores[s.cfg.Dir] = st
	return st
}

// memoryLaunchText is what a session launched in dir is given. An index
// that could not be read launches the session without it and says so in the
// log: a launch is not refused for a memory it could not read, and the
// session is not told the Project has none.
func (s *Server) memoryLaunchText(dir string) string {
	text, err := s.memoryStore().LaunchText(dir)
	if err != nil {
		log.Printf("memory: the shared memory of %s could not be read; the session starts without it: %v", dir, err)
		return ""
	}
	return text
}

func (s *Server) projectMemoryRoute(w http.ResponseWriter, r *http.Request, placeID, name string) {
	if r.URL.RawQuery != "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Memory routes take no query fields.")
		return
	}
	one := name != ""
	switch {
	case !one && r.Method != http.MethodGet && r.Method != http.MethodPost,
		one && r.Method != http.MethodGet && r.Method != http.MethodPut && r.Method != http.MethodDelete:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"List or add with GET or POST on …/memory; show, update or forget with GET, PUT or DELETE on …/memory/<name>.")
		return
	}
	if r.Method != http.MethodGet && !machineAuthed(r) && !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may read the Project memory but cannot change it.")
		return
	}
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	key, ok := memory.KeyFor(project.Path)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "The Project's path is not one memory can be filed under.")
		return
	}
	store := s.memoryStore()
	var body []byte
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, memoryRequestBodyLimit+1))
		if err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The body could not be read.")
			return
		}
		if len(body) > memoryRequestBodyLimit {
			writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "A memory entry's body is at most 64 KiB.")
			return
		}
	} else if r.ContentLength > 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "This route takes no body.")
		return
	}
	switch {
	case !one && r.Method == http.MethodGet:
		l, err := store.Listing(key)
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		writeJSON(w, MemoryListAnswer(key, l))
	case one && r.Method == http.MethodGet:
		e, err := store.Get(key, name)
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		writeJSON(w, contract.ProjectMemoryEntry{Name: e.Name, Description: e.Description,
			Type: contract.ProjectMemoryType(e.Type), Group: e.Group, Body: e.Body})
	case one && r.Method == http.MethodDelete:
		outcome, err := store.Forget(key, name)
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		writeJSON(w, contract.ProjectMemoryWriteAnswer{Name: name, Outcome: contract.ProjectMemoryOutcome(outcome)})
	default:
		var in contract.ProjectMemoryEntry
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF {
			writeRefusal(w, http.StatusBadRequest, "bad_request",
				`Send {"name", "description", "type", "group", "body"} as one JSON object; "group" may be left out.`)
			return
		}
		if one && in.Name != name {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "The entry's name must be the one in the path.")
			return
		}
		e := memory.Entry{Name: in.Name, Description: in.Description, Type: memory.Type(in.Type), Group: in.Group, Body: in.Body}
		var outcome memory.Outcome
		var err error
		if one {
			outcome, err = store.Update(key, e)
		} else {
			outcome, err = store.Add(key, e)
		}
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		if outcome == memory.Created {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(contract.ProjectMemoryWriteAnswer{Name: e.Name, Outcome: contract.ProjectMemoryOutcomeCreated})
			return
		}
		writeJSON(w, contract.ProjectMemoryWriteAnswer{Name: e.Name, Outcome: contract.ProjectMemoryOutcome(outcome)})
	}
}

// MemoryListAnswer is a store listing as GET …/memory answers it.
func MemoryListAnswer(key string, l memory.Listing) contract.ProjectMemoryList {
	out := contract.ProjectMemoryList{ProjectKey: key, Entries: []contract.ProjectMemorySummary{},
		Groups: []contract.ProjectMemoryGroup{}, Index: l.Index, IndexCut: l.Cut}
	for _, e := range l.Entries {
		out.Entries = append(out.Entries, contract.ProjectMemorySummary{Name: e.Name,
			Description: e.Description, Type: contract.ProjectMemoryType(e.Type), Group: e.Group})
	}
	for _, g := range l.Groups {
		out.Groups = append(out.Groups, contract.ProjectMemoryGroup{Slug: g.Slug, Description: g.Description, Entries: int64(g.Entries)})
	}
	return out
}

// projectMemoryGroupRoute is PUT /v1/projects/{place}/memory-groups/{slug}:
// create a group or change its description. Groups are listed by GET
// …/memory.
func (s *Server) projectMemoryGroupRoute(w http.ResponseWriter, r *http.Request, placeID, slug string) {
	if r.URL.RawQuery != "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Memory routes take no query fields.")
		return
	}
	if r.Method != http.MethodPut {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"Set a group with PUT on …/memory-groups/<slug>; list groups with GET on …/memory.")
		return
	}
	if !machineAuthed(r) && !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may read the Project memory but cannot change it.")
		return
	}
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	key, ok := memory.KeyFor(project.Path)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "The Project's path is not one memory can be filed under.")
		return
	}
	var in contract.ProjectMemoryGroupSet
	dec := json.NewDecoder(io.LimitReader(r.Body, memoryGroupRequestBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF {
		writeRefusal(w, http.StatusBadRequest, "bad_request", `Send {"description"} as one JSON object of at most 4 KiB.`)
		return
	}
	outcome, err := s.memoryStore().SetGroup(key, slug, in.Description)
	if err != nil {
		writeMemoryRefusal(w, err)
		return
	}
	if outcome == memory.Created {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(contract.ProjectMemoryWriteAnswer{Name: slug, Outcome: contract.ProjectMemoryOutcomeCreated})
		return
	}
	writeJSON(w, contract.ProjectMemoryWriteAnswer{Name: slug, Outcome: contract.ProjectMemoryOutcome(outcome)})
}

// writeMemoryRefusal answers a store error by its type. Anything the store
// did not type is "could not check" (500 memory_unreadable), never "not
// found".
func writeMemoryRefusal(w http.ResponseWriter, err error) {
	detail := err.Error()
	switch {
	case errors.Is(err, memory.ErrInvalid):
		writeRefusal(w, http.StatusBadRequest, "memory_entry_invalid", detail)
	case errors.Is(err, memory.ErrDuplicate):
		writeRefusal(w, http.StatusConflict, "memory_entry_exists", detail)
	case errors.Is(err, memory.ErrNotFound):
		writeRefusal(w, http.StatusNotFound, "memory_entry_not_found", detail)
	case errors.Is(err, memory.ErrNoGroup):
		writeRefusal(w, http.StatusBadRequest, "memory_group_not_found", detail)
	case errors.Is(err, memory.ErrFull):
		writeRefusal(w, http.StatusConflict, "memory_full", detail)
	case errors.Is(err, os.ErrPermission):
		writeRefusal(w, http.StatusForbidden, "file_permission", "The memory directory could not be written with this machine's permissions.")
	default:
		writeRefusal(w, http.StatusInternalServerError, "memory_unreadable", "The Project memory could not be read or written: "+detail)
	}
}
