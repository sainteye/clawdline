// Package app composes the ports into the readings and commands the transports
// expose. It depends on the domain and on the port interfaces, never on a
// platform implementation.
package app

import (
	"context"
	"sort"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type Inventory struct {
	Process   ports.ProcessHost
	Terminals []ports.TerminalHost
	Identity  ports.IdentityHost
	Agents    ports.AgentHost
	// Screen is one capture, now, on the caller's own clock. It is what
	// answers a menu (Actions.Key) and what the broker reads before it types a
	// briefing into a child — both of which act on what is drawn now, and a
	// screen from eight seconds ago is not that.
	Screen ports.ScreenHost
	// Held is what the list reads instead: a screen kept and refreshed behind
	// the answer, bounded in how many captures it may have in flight and left
	// alone after a failure (screen_held.go). Nil falls back to Screen, which
	// is what a hand-built Inventory in a test wants.
	Held *HeldScreens
	// Activity is the bound on how many rows one reading may ask an activity
	// time of, and the record of what the last one spent
	// (activity_reads.go). Nil is the register's default with nothing
	// measured, which is what a hand-built Inventory in a test wants.
	Activity *ActivityReads
}

// screens is the reader the list uses: the held one where there is one.
func (in Inventory) screens() ports.ScreenHost {
	if in.Held != nil {
		return in.Held
	}
	return in.Screen
}

// Read takes one reading of the machine from every source and merges them.
//
// An assistant running inside a tmux pane is seen twice: once by the process
// table, which knows it is running, and once by tmux, which also knows its
// pane id, title and working directory. The tmux row wins on the merge because
// it carries more, and the two are recognised as one session by their tty.
//
// Completeness is the AND of its sources: one unreadable source makes the whole
// reading non-authoritative, because what it casts doubt on is whether the list
// is complete at all — not one row in it. Each source's own answer is kept
// beside the AND (Sources), because "is this tmux pane gone" is a question
// only tmux can answer, and the AND would let an iTerm2 that cannot be asked
// veto it for ever — or, read the other way, let a reading with iTerm2 rows
// in it stand in for a tmux listing that failed.
//
// The named regions a source could not read (session.Gap) are merged with the
// same rule and for the same reason one rung further down: a source that is
// incomplete because of one window it cannot open has told a reader something
// much narrower than "this source is unreliable", and Inventory.ProvesAbsence
// is where the narrower thing is read.
func (in Inventory) Read(ctx context.Context) session.Inventory {
	// The identity source is refreshed here rather than by whoever calls this.
	// It used to be the caller's job, and of the six places that read an
	// inventory only two remembered — so the board, the coordinator, the
	// session actions and the usage report were all reading identities from an
	// earlier moment, which shows up as sessions that have no conversation id
	// and therefore no record to read. A precondition that every caller must
	// remember is a precondition that will be forgotten.
	if h, ok := in.Identity.(interface{ Refresh() }); ok {
		h.Refresh()
	}

	merged := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "merged",
		Complete:   true,
		Sources:    map[string]bool{},
	}

	byTTY := map[string]session.Session{}
	order := []string{}

	add := func(src session.Inventory) {
		if !src.Complete {
			merged.Complete = false
		}
		// Two sources with one name are one source: either failing makes it
		// incomplete.
		if prev, seen := merged.Sources[src.Provenance]; seen {
			merged.Sources[src.Provenance] = prev && src.Complete
		} else {
			merged.Sources[src.Provenance] = src.Complete
		}
		merged.Notes = append(merged.Notes, src.Notes...)
		// A gap keeps its own source's name, so a merged reading can still
		// say which part of which source was not read — which is the whole
		// point of carrying it rather than a single flag.
		merged.Gaps = append(merged.Gaps, src.Gaps...)
		for _, s := range src.Sessions {
			key := s.TTY
			if key == "" {
				key = s.ID
			}
			existing, seen := byTTY[key]
			if !seen {
				order = append(order, key)
				byTTY[key] = s
				continue
			}
			byTTY[key] = richer(existing, s)
		}
	}

	if in.Process != nil {
		inv, _ := in.Process.Scan(ctx)
		add(inv)
	}
	for _, t := range in.Terminals {
		inv, _ := t.Inventory(ctx)
		add(inv)
	}

	sort.Strings(order)
	// One budget for the whole reading, so the bound is on what looking at
	// this machine costs and not on what one row does.
	budget := in.Activity.budget()
	var read, unread int64
	if h, ok := in.Agents.(interface{ Begin() }); ok {
		h.Begin()
	}
	for _, key := range order {
		row := in.enrich(ctx, byTTY[key])
		if in.Agents != nil {
			row = in.Agents.ForSession(row)
		}
		row = in.readScreen(ctx, row)
		row = in.readShells(ctx, row)
		row = in.readActivity(ctx, row, &budget, &read, &unread)
		merged.Sessions = append(merged.Sessions, row)
	}
	in.Activity.spent(read, unread)
	return merged
}

// readActivity asks when this session last moved (session.Activity).
//
// The answer comes from the identity source when it can give one, because the
// file that answers is the assistant's own record and that source is the one
// that knows where it is. A source that cannot say, and a row past this
// reading's budget, both get a named nothing rather than a time — which is the
// difference between a row a reader is shown and a row a failure buried.
func (in Inventory) readActivity(ctx context.Context, s session.Session, budget, read, unread *int64) session.Session {
	if !s.IsAssistant() {
		return s
	}
	h, ok := in.Identity.(interface {
		LastActivity(context.Context, session.Session) session.Activity
	})
	if !ok {
		s.Activity = session.Activity{
			Reason: session.ActivityUnsupported,
			Detail: "this daemon's identity source keeps no activity times",
		}
		return s
	}
	if *budget <= 0 {
		*unread++
		s.Activity = session.Activity{
			Reason: session.ActivityUnread,
			Detail: "this reading had already read as many records as it may (sessions.activity_reads)",
		}
		return s
	}
	*budget--
	*read++
	s.Activity = h.LastActivity(ctx, s)
	return s
}

// enrich asks the assistant's own records about a row the machine reported.
//
// What comes back outranks the process reading, and the row says so: a status
// the assistant wrote about itself is not the same kind of fact as a process
// that merely exists, and a reader has to be able to tell them apart.
//
// An assistant's name comes from the identity source alone. What a terminal
// reported as the row's title — a pane title, a tab name — is dropped here,
// because the Swift app names no session from one: a title is a place a name
// is displayed, anything in the terminal can overwrite it, and a tab renamed
// to `Default` must not rename the row. With no identity there is no name,
// and the console says where the session is instead.
func (in Inventory) enrich(ctx context.Context, s session.Session) session.Session {
	if !s.IsAssistant() {
		return s
	}
	s.Label = ""
	if in.Identity == nil {
		return s
	}
	id, ok := in.Identity.ForSession(ctx, s)
	// How the row was named — or which kind of nothing kept it nameless — is
	// an answer whether or not there was an identity behind it, so it is taken
	// before `ok` is looked at. An empty one leaves what the scan already
	// found: for Codex the scan is the source, and this must not overwrite its
	// answer with silence.
	if id.Binding != "" {
		s.Binding = id.Binding
	}
	if !ok {
		return in.named(s)
	}
	if id.ConversationID != "" {
		s.ConversationID = id.ConversationID
	}
	s.Label = id.Label
	s.Rungs = id.Rungs
	s.CustomTitle = id.CustomTitle
	if s.CWD == "" && id.CWD != "" {
		s.CWD = id.CWD
	}
	if id.Pane != "" {
		s.ID = id.Pane
		s.Backend = session.BackendTmux
	}
	if id.Status != "" {
		s.State = session.StateFromAssistantStatus(id.Status)
		s.Evidence = session.EvidenceRegistry
	}
	return in.named(s)
}

// named gives a row the bottom rung of its name: where the session is.
//
// It is filled here rather than by an identity source because it is the one
// rung no source can supply — it is not something an assistant knows about
// itself. Every rung above it still wins, including the two the console fills
// from the Swift store afterwards, so a task title is never displaced by this.
//
// What it replaces is a row whose label is empty. A fresh Codex has no name
// anywhere on disk until somebody types into it, and an unnamed row in a list
// of sessions reads as a session that is not there — which is exactly the row
// somebody opened a moment ago and is looking for.
func (in Inventory) named(s session.Session) session.Session {
	if !s.IsAssistant() {
		return s
	}
	if s.Rungs.Coordinate == "" {
		s.Rungs.Coordinate = session.Coordinate(s)
	}
	s.Label = session.PreferredLabel(s.Rungs)
	return s
}

// readScreen is the fallback for an assistant that keeps no live record of
// itself. It runs only where nothing better answered, so a session that told us
// what it is doing is never overruled by a guess about its screen.
//
// The live line is a different question and is read either way. A session's
// own record can say it is working; only the screen says what it is working
// on, and the Swift app draws that line under every working row whatever told
// it the state. So a registry-backed session is captured too — but only while
// it is working, which is the only time there is a line to find, or waiting,
// which is the only time there is a menu to find.
//
// **A menu beats a spinner, and beats a registry that says busy.** Claude Code
// draws its dialog below whatever came before it without always erasing the
// spinner line, and the registry can be a beat behind a dialog just drawn; of
// the two ways to be wrong for that beat, only "a waiting session shown as
// working" hides the row somebody has to act on (SessionState.read,
// SessionRegistry.merge). A registry that says waiting opens the parsing gate
// for AskUserQuestion's flush-left caret; nothing else here does, because this
// daemon installs no hooks.
func (in Inventory) readScreen(ctx context.Context, s session.Session) session.Session {
	reader := in.screens()
	if reader == nil || !s.IsAssistant() {
		return s
	}
	registry := s.Evidence == session.EvidenceRegistry
	// A registry word this build does not know has told us nothing, so it is
	// not a record that the screen would overrule: the screen still answers,
	// and only when it holds positive evidence (session.ReadState).
	unread := registry && s.State == session.StateUnknown
	if registry && !unread && s.State != session.StateWorking && s.State != session.StateWaiting {
		return s
	}
	screen, ok := reader.Capture(ctx, s)
	if !ok {
		return s
	}
	gate := registry && s.State == session.StateWaiting
	if menu, found := session.ReadMenu(screen, s.Assistant, gate); found {
		if s.State != session.StateWaiting {
			s.State = session.StateWaiting
			s.Evidence = session.EvidenceScreen
		}
		menu = in.refillMenu(ctx, s, menu)
		s.Menu = &menu
		// The Swift page's transcript revision watches `line`, and a waiting
		// row never draws it, so it carries the menu's revision instead.
		s.Line = session.MenuRevision(menu)
		return s
	}
	if !registry || unread {
		if state, read := session.ReadState(screen, s.Assistant); read {
			s.State = state
			s.Evidence = session.EvidenceScreen
		}
	}
	if s.State == session.StateWorking {
		s.Line = session.WorkingLine(screen, s.Assistant, 25)
	}
	return s
}

// refillMenu gives a menu the words the screen had no room for, from the
// questions the session's own transcript says are open — and only from the one
// the screen proves it is showing (session.RefillMenu).
func (in Inventory) refillMenu(ctx context.Context, s session.Session, menu session.Menu) session.Menu {
	h, ok := in.Identity.(interface {
		OpenQuestions(context.Context, session.Session) ([]session.AskedQuestion, bool)
	})
	if !ok || len(menu.Options) == 0 {
		return menu
	}
	asked, ok := h.OpenQuestions(ctx, s)
	if !ok {
		return menu
	}
	return session.RefillMenu(menu, asked)
}

// readShells asks what the session left running in the background, when the
// identity source can say. It is asked of every assistant row whatever its
// state: an idle row with a build still going is the one this exists for.
func (in Inventory) readShells(ctx context.Context, s session.Session) session.Session {
	h, ok := in.Identity.(interface {
		Shells(context.Context, session.Session) []session.Shell
	})
	if !ok || !s.IsAssistant() {
		return s
	}
	s.Shells = h.Shells(ctx, s)
	return s
}

// richer keeps the row that carries more, field by field, rather than letting
// whichever source answered last overwrite what another source proved.
func richer(a, b session.Session) session.Session {
	out := a
	if out.CWD == "" {
		out.CWD = b.CWD
	}
	if out.Label == "" {
		out.Label = b.Label
	}
	if out.PID == 0 {
		out.PID = b.PID
	}
	if out.Assistant == "" {
		out.Assistant = b.Assistant
	}
	// Codex background threads are learned from the process row's open files.
	// A terminal row normally wins the identity merge, but it must not erase
	// that stronger, already-scoped reading.
	if len(out.Agents) == 0 && len(b.Agents) > 0 {
		out.Agents = b.Agents
	}
	if out.AgentReading.State == "" && b.AgentReading.State != "" {
		out.AgentReading = b.AgentReading
	}
	// A pane id survives a restart in a way a tty does not, so it is the better
	// identity when both are present.
	if b.Backend == session.BackendTmux {
		out.ID = b.ID
		out.Backend = b.Backend
	}
	// So is an iTerm2 session id, and it is the one the Swift app lists, the
	// one a start answers with, and the one the iTerm2 adapter finds a tab by.
	// Keeping the tty here listed every iTerm2 tab under a name no action
	// could reach. A tmux pane already on the row keeps its id: under
	// `tmux -CC` the iTerm2 mirror has no tty and never reaches this merge.
	if b.Backend == session.BackendITerm && b.ID != "" && b.ID != b.TTY && out.Backend != session.BackendTmux {
		out.ID = b.ID
		out.Backend = b.Backend
	}
	return out
}
