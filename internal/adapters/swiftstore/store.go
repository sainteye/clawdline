package swiftstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// Store is the Swift app's store directory, read-only.
type Store struct {
	dir          string
	orchestrator *file[orchestratorFile]
	coordinator  *file[Coordinator]
	config       *file[configFile]
}

// orchestratorFile adds the one collection that is not a record list the
// projections share: each terminal's turn clock.
type orchestratorFile struct {
	Orchestrator
	SessionActivity []sessionActivity `json:"session_activity"`
}

type sessionActivity struct {
	TerminalID string `json:"terminal_id"`
	Generation int64  `json:"generation"`
}

// configFile declares one key of config.json and nothing else: the names a
// person gave sessions. The rest of that file is settings this daemon has no
// business holding.
type configFile struct {
	SessionTitles []SessionTitle `json:"session_titles"`
}

// SessionTitle is a name typed in Clawdline (or, when Automatic, one a model
// chose for a Claude conversation that had none yet).
type SessionTitle struct {
	Title              string   `json:"title"`
	TerminalID         string   `json:"terminal_id"`
	SessionID          *string  `json:"session_id"`
	Automatic          bool     `json:"automatic"`
	StartedAt          *Seconds `json:"started_at"`
	SeenCustomTitle    *string  `json:"seen_custom_title"`
	SeenTranscriptPath *string  `json:"seen_transcript_path"`
}

// Dir is where the Swift app keeps its store. It is fixed in that app
// (`~/.config/clawdline`, see HookBridge.swift); CLAWDLINE_SWIFT_DIR moves it
// for a test or for a second installation.
func Dir() string {
	if v := os.Getenv("CLAWDLINE_SWIFT_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clawdline")
}

// Open prepares a reader. It opens nothing yet: the files are read on demand,
// and a directory that does not exist is an answer ("unknown"), not a startup
// failure, because this daemon runs on machines with no Swift app.
func Open(dir string) *Store {
	return &Store{
		dir:          dir,
		orchestrator: newFile[orchestratorFile](filepath.Join(dir, "orchestrator.json")),
		coordinator:  newFile[Coordinator](filepath.Join(dir, "coordinator.json")),
		config:       newFile[configFile](filepath.Join(dir, "config.json")),
	}
}

// Snapshot is one reading of everything the session list needs from the store.
type Snapshot struct {
	Orchestrator
	// Known is false when orchestrator.json could not be read and there is no
	// earlier reading to carry. Nothing projected from a snapshot that is not
	// known may be drawn as settled.
	Known bool
	// Stale is true when the newest read failed and an earlier one is carried.
	Stale bool
	Err   error

	// Activity is each terminal's turn clock, by terminal id.
	Activity map[string]int64

	// Coordinator is the registered Clawdfather, or nil when there is none or
	// the record could not be read. CoordinatorKnown says which.
	Coordinator      *Coordinator
	CoordinatorKnown bool

	// Titles are config.json's session_titles, in file order (the last one
	// that matches wins, as in Config.swift).
	Titles      []SessionTitle
	TitlesKnown bool
}

// Read takes one reading. Each file is decoded only when it changed since the
// last call.
func (s *Store) Read() Snapshot {
	if s == nil {
		return Snapshot{Err: errors.New("no Swift store configured")}
	}
	var out Snapshot

	orch := s.orchestrator.read()
	switch {
	case orch.Missing:
		out.Err = errors.New("orchestrator.json is not there")
	case !orch.Known:
		out.Err = orch.Err
	default:
		out.Orchestrator = orch.Value.Orchestrator
		out.Known = true
		out.Stale = orch.Stale
		out.Err = orch.Err
		out.Activity = make(map[string]int64, len(orch.Value.SessionActivity))
		for _, a := range orch.Value.SessionActivity {
			out.Activity[a.TerminalID] = a.Generation
		}
	}

	coord := s.coordinator.read()
	switch {
	case coord.Missing:
		// No file is no coordinator, and that is a known answer.
		out.CoordinatorKnown = true
	case coord.Known:
		out.CoordinatorKnown = true
		c := coord.Value
		// Coordinator.swift accepts version 1 only; anything else is corrupt
		// or newer, and in both cases nobody is Clawdfather here.
		if c.Version == 1 && c.SessionID != "" {
			out.Coordinator = &c
		}
	}

	conf := s.config.read()
	if conf.Known {
		out.TitlesKnown = true
		out.Titles = conf.Value.SessionTitles
	}
	return out
}

// tasksNewestFirst is the order Orchestrator.records() returns: created,
// newest first, with the id as a tie-break so two readings agree.
func tasksNewestFirst(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// tasksOldestFirst is the order the closeability index walks them in.
func tasksOldestFirst(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}
