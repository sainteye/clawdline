package swiftstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// Store is the Swift app's store directory, read-only.
type Store struct {
	dir string
	// disabled is CLAWDLINE_NEXT_LEGACY_STORE=off, taken when the reader was
	// opened: a reader that is off opens nothing for the life of the process.
	disabled     bool
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
// and a directory that does not exist is an answer ("absent"), not a startup
// failure, because this daemon runs on machines with no Swift app. With the
// legacy switch off (legacy.go) the reader never opens anything at all.
func Open(dir string) *Store {
	if Disabled() {
		return &Store{dir: dir, disabled: true}
	}
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
	// Source says how orchestrator.json contributed: current, stale,
	// unreadable, absent or disabled. Absent and disabled are known answers —
	// the store holds nothing — and unreadable is the only one that is not.
	Source Source

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
		return Snapshot{Err: errors.New("no Swift store configured"), Source: SourceUnreadable}
	}
	if s.disabled {
		// Nothing is opened. No coordinator and no titles are known answers
		// here, as they are on a machine without the Swift app.
		return Snapshot{Source: SourceDisabled, CoordinatorKnown: true}
	}
	var out Snapshot

	orch := s.orchestrator.read()
	switch {
	case orch.Missing:
		// No file is no Swift broker on this machine, and that is known: this
		// daemon's own records are the whole answer then.
		out.Source = SourceAbsent
	case !orch.Known:
		out.Err = orch.Err
		out.Source = SourceUnreadable
	default:
		out.Orchestrator = orch.Value.Orchestrator
		out.Known = true
		out.Stale = orch.Stale
		out.Err = orch.Err
		out.Source = SourceCurrent
		if orch.Stale {
			out.Source = SourceStale
		}
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

// Usable reports whether the snapshot's records can be projected as they
// stand: the Swift store was read, or it is known to hold nothing (absent or
// switched off), so whatever this daemon's own records add is the whole
// answer. Only an unreadable store is not usable — then nothing projected from
// it may be drawn as settled.
func (s Snapshot) Usable() bool { return s.Known || s.Source.Settled() }

// Disabled reports whether this reader was opened with the legacy switch off.
func (s *Store) Disabled() bool { return s != nil && s.disabled }

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
