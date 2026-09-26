package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Starting a session in a place, and picking a recorded one back up — the
// Swift app's three routes under /v1/places/, with its rules:
//
//   - The body is never read. The place, the assistant, the model and the
//     conversation are all path segments, each resolved against something this
//     machine built or a closed list, so there is nowhere on these routes a
//     directory or a command could be written.
//   - Reading what has been said in a place is read-level. Starting and
//     resuming need a device that may send, and an Idempotency-Key: a retried
//     POST must not be a second tab.
//   - Opening a terminal is serialised, and the queue in front of it is
//     bounded; a request that finds it full is told so rather than waiting
//     behind an unbounded line of Apple Events.

// pastTitles remembers each transcript's title against its size and time, as
// the session list's own reader does.
var pastTitles = transcript.NewTitles()

var (
	// opening admits one terminal opening at a time and three more waiting.
	opening   = make(chan struct{}, 1)
	openQueue = make(chan struct{}, 4)
)

// recorder keeps what a handler wrote so it can be filed under its key.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

func writePlaceRefusal(w http.ResponseWriter, status int, code, message, app string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(contract.PlaceRefusal{Error: contract.PlaceError{
		Code: code, Message: message, RequestID: requestID(), App: app}})
}

// placeRoute answers everything under /v1/places/.
func (s *Server) placeRoute(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(routePath(r), "/v1/places/")
	var parts []string
	for _, p := range strings.Split(raw, "/") {
		v, err := url.PathUnescape(p)
		if err != nil {
			writePlaceRefusal(w, http.StatusNotFound, "not_found", "No such route", "")
			return
		}
		parts = append(parts, v)
	}
	if len(parts) < 2 || parts[0] == "" {
		writePlaceRefusal(w, http.StatusNotFound, "not_found", "No such route", "")
		return
	}
	switch {
	case r.Method == http.MethodPost && parts[1] == "start" && len(parts) <= 4:
		assistant := projects.AssistantClaude
		if len(parts) >= 3 {
			assistant = parts[2]
		}
		if !projects.KnownAssistant(assistant) {
			writePlaceRefusal(w, http.StatusNotFound, "not_found", "No assistant named that", "")
			return
		}
		model := ""
		if len(parts) == 4 {
			model = parts[3]
			if !projects.KnownStartModel(model) {
				writePlaceRefusal(w, http.StatusNotFound, "not_found", "No model named that", "")
				return
			}
		}
		s.writing(w, r, func(w http.ResponseWriter) { s.startPlace(w, r, parts[0], assistant, model) })
	case r.Method == http.MethodPost && parts[1] == "resume" && (len(parts) == 3 || len(parts) == 4):
		assistant, conversation := projects.AssistantClaude, parts[2]
		if len(parts) == 4 {
			assistant, conversation = parts[2], parts[3]
			if !projects.KnownAssistant(assistant) {
				writePlaceRefusal(w, http.StatusNotFound, "not_found", "No such route", "")
				return
			}
		}
		s.writing(w, r, func(w http.ResponseWriter) { s.resumePlace(w, r, parts[0], assistant, conversation) })
	case r.Method == http.MethodGet && parts[1] == "sessions" && (len(parts) == 2 || len(parts) == 3):
		assistant := projects.AssistantClaude
		if len(parts) == 3 {
			assistant = parts[2]
			if !projects.KnownAssistant(assistant) {
				writePlaceRefusal(w, http.StatusNotFound, "not_found", "No such route", "")
				return
			}
		}
		s.pastRoute(w, r, parts[0], assistant)
	default:
		writePlaceRefusal(w, http.StatusNotFound, "not_found", "No such route", "")
	}
}

// writing is RemoteServer.writing for these routes: a device that may send, a
// key, and the first answer to that key for the key's window (D03). A second
// request with a key still being answered waits for that answer rather than
// opening its own tab.
func (s *Server) writing(w http.ResponseWriter, r *http.Request, body func(http.ResponseWriter)) {
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
		return
	}
	// **The key names one request, and a request is a method and a route.**
	// A key is the caller's word for "this thing I am asking for once"; the
	// same word for `POST /v1/places/A/start` and for
	// `POST /v1/places/B/resume/<id>` is two different askings, and answering
	// the second with the first's body is a session that never opened
	// reported as one that did. So the route is the request's digest, and a
	// key reused for another route is refused rather than answered (D03).
	//
	// The answer is filed in the store's receipts, not in this process: a
	// retry after a restart must not open a second tab (G15).
	k := store.ReceiptKey{Scope: scopePlaces, Actor: accessOf(r).verdict.Device, Key: key}
	s.receipted(w, r, k, requestDigest([]byte(r.Method), []byte(routePath(r))),
		// A full queue is a fact about this moment, not about the request,
		// and filing it would refuse the same key's retry.
		func(status int) bool { return status != http.StatusTooManyRequests },
		body)
	log.Printf("remote: %s %s by %s", r.Method, r.URL.Path, accessOf(r).verdict.Device)
}

// startReading is one look at the machine for one request: the places list's
// live directories, and which recorded conversations are open now.
type startReading struct {
	live       []string
	openClaude map[string]bool
	openCodex  map[string]bool
}

func (s *Server) readForStart(ctx context.Context) startReading {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return startReadingFrom(s.freshReading(ctx))
}

// startReadingFrom is readForStart over a reading the caller already took.
func startReadingFrom(inv session.Inventory) startReading {
	home, _ := os.UserHomeDir()
	out := startReading{openClaude: map[string]bool{}, openCodex: map[string]bool{}}
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		if item.CWD != "" {
			out.live = append(out.live, item.CWD)
		}
		if item.ConversationID == "" {
			continue
		}
		switch item.Assistant {
		case session.AssistantClaude:
			if item.CWD == "" {
				continue
			}
			path := transcript.ClaudePath(home, item.CWD, item.ConversationID)
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				path = resolved
			}
			out.openClaude[path] = true
		case session.AssistantCodex:
			out.openCodex[item.ConversationID] = true
		}
	}
	return out
}

func (s *Server) starter(reading startReading) app.Starter {
	readers := s.projectReaders()
	return app.Starter{
		Places: func(ctx context.Context) []projects.Place { return readers.places.List(reading.live, 40) },
		Terminal: func() projects.TerminalChoice {
			values, err := nextconfig.Open(s.cfg.Dir).Read()
			if err != nil {
				return projects.TerminalAuto
			}
			choice, _ := values.String("terminal")
			return projects.ParseTerminalChoice(choice)
		},
		Launcher: terminal.NewLauncher(),
		Past: func(ctx context.Context, place projects.Place, assistant string) []projects.Past {
			return s.past(ctx, place, assistant, reading, 200)
		},
	}
}

// past is StartPoints.past(in:assistant:limit:). A Codex index that cannot be
// asked is an empty list, as it is in the Swift app.
func (s *Server) past(ctx context.Context, place projects.Place, assistant string, reading startReading, limit int) []projects.Past {
	rootTitles := map[string]string{}
	queriedRootTitle := map[string]bool{}
	loggedRootTitleError := false
	orchestratorTitle := func(id string) string {
		if queriedRootTitle[id] {
			return rootTitles[id]
		}
		queriedRootTitle[id] = true
		titles, err := s.boardTitles(ctx, place.Path, assistant, []string{id}, nil)
		if err != nil && !loggedRootTitleError {
			log.Printf("places: Root Assignment titles for %s: %v", place.ID, err)
			loggedRootTitleError = true
		}
		rootTitles[id] = titles[id]
		return titles[id]
	}
	if assistant == projects.AssistantCodex {
		rows, err := projects.CodexPast(ctx, place, reading.openCodex,
			projects.PastTitles{Orchestrator: orchestratorTitle}, limit)
		if err != nil {
			log.Printf("places: codex history for %s: %v", place.ID, err)
			return []projects.Past{}
		}
		return rows
	}
	snap := s.swift.Read()
	titles := projects.PastTitles{
		Recorded:     pastTitles.Read,
		Orchestrator: orchestratorTitle,
		Manual: func(id, custom string) string {
			if !snap.TitlesKnown {
				return ""
			}
			return manualConversationTitle(snap.Titles, id, custom)
		},
		Automatic: func(id string) string {
			if !snap.TitlesKnown {
				return ""
			}
			found := ""
			for _, row := range snap.Titles {
				if row.Automatic && row.SessionID != nil && *row.SessionID == id {
					found = row.Title
				}
			}
			return found
		},
	}
	return projects.ClaudePast(place, reading.openClaude, titles, limit, 400)
}

type swiftTitle = swiftstore.SessionTitle

// manualConversationTitle is Config.sessionTitle(conversationID:): the last
// typed name for that conversation, unless a `/rename` has superseded it.
func manualConversationTitle(rows []swiftTitle, id, custom string) string {
	var hit *swiftTitle
	for i := range rows {
		if !rows[i].Automatic && rows[i].SessionID != nil && *rows[i].SessionID == id {
			hit = &rows[i]
		}
	}
	if hit == nil {
		return ""
	}
	if hit.SeenTranscriptPath != nil {
		seen := ""
		if hit.SeenCustomTitle != nil {
			seen = *hit.SeenCustomTitle
		}
		if seen != custom {
			return ""
		}
	}
	return hit.Title
}

func (s *Server) startPlace(w http.ResponseWriter, r *http.Request, id, assistant, model string) {
	release, ok := admitOpening(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	starter := s.starter(s.readForStart(ctx))
	place, ok := starter.Place(ctx, id)
	if !ok {
		log.Printf("audit place.start place=%.64s ok=0 why=not_found", id)
		writePlaceRefusal(w, http.StatusNotFound, "not_found", "No place named that", "")
		return
	}
	made, err := starter.Start(ctx, place, assistant, model, "")
	if err != nil {
		code := writeStartRefusal(w, err)
		log.Printf("audit place.start place=%s cwd=%q assistant=%s ok=0 why=%s", place.ID, place.Path, assistant, code)
		return
	}
	log.Printf("audit place.start place=%s cwd=%q assistant=%s ok=1 id=%s", place.ID, place.Path, assistant, made.ID)
	writeJSON(w, contract.PlaceStarted{OK: true, ID: made.ID, Backend: contract.Backend(made.Backend),
		Assistant: assistant, Model: model, Place: place.ID, CWD: place.Path, Attach: made.Attach,
		At: time.Now().Unix()})
}

func (s *Server) resumePlace(w http.ResponseWriter, r *http.Request, id, assistant, conversation string) {
	release, ok := admitOpening(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	starter := s.starter(s.readForStart(ctx))
	place, ok := starter.Place(ctx, id)
	if !ok {
		log.Printf("audit place.resume place=%.64s ok=0 why=not_found", id)
		writePlaceRefusal(w, http.StatusNotFound, "not_found", "No place named that", "")
		return
	}
	made, err := starter.Resume(ctx, place, conversation, assistant)
	if err != nil {
		code := writeStartRefusal(w, err)
		log.Printf("audit place.resume place=%s cwd=%q assistant=%s session=%.64s ok=0 why=%s",
			place.ID, place.Path, assistant, conversation, code)
		return
	}
	log.Printf("audit place.resume place=%s cwd=%q assistant=%s session=%s ok=1 id=%s",
		place.ID, place.Path, assistant, conversation, made.ID)
	writeJSON(w, contract.PlaceResumed{OK: true, ID: made.ID, Backend: contract.Backend(made.Backend),
		Assistant: assistant, Place: place.ID, CWD: place.Path, Session: conversation,
		Attach: made.Attach, At: time.Now().Unix()})
}

func (s *Server) pastRoute(w http.ResponseWriter, r *http.Request, id, assistant string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	reading := s.readForStart(ctx)
	starter := s.starter(reading)
	place, ok := starter.Place(ctx, id)
	if !ok {
		writePlaceRefusal(w, http.StatusNotFound, "not_found", "No place named that", "")
		return
	}
	// One more than is sent, so the answer can say whether there were more.
	const limit = 200
	found := s.past(ctx, place, assistant, reading, limit+1)
	out := contract.PastSessionList{At: time.Now().Unix(), Place: place.ID, Assistant: assistant,
		More: len(found) > limit, Sessions: []contract.PastSession{}}
	if len(found) > limit {
		found = found[:limit]
	}
	for _, p := range found {
		out.Sessions = append(out.Sessions, contract.PastSession{ID: p.ID, Title: p.Title,
			At: p.At.Unix(), Live: p.Live})
	}
	writeJSON(w, out)
}

// admitOpening takes the one opening slot, waiting in a bounded queue.
func admitOpening(w http.ResponseWriter, r *http.Request) (func(), bool) {
	select {
	case openQueue <- struct{}{}:
	default:
		writePlaceRefusal(w, http.StatusTooManyRequests, "terminal_busy",
			"Other sessions are already being opened on this machine; try again shortly.", "")
		return nil, false
	}
	select {
	case opening <- struct{}{}:
		return func() { <-opening; <-openQueue }, true
	case <-r.Context().Done():
		<-openQueue
		return nil, false
	}
}

// writeStartRefusal writes a Starter refusal and answers its code.
func writeStartRefusal(w http.ResponseWriter, err error) string {
	var refusal app.StartRefusal
	if errors.As(err, &refusal) {
		writePlaceRefusal(w, refusal.Status, refusal.Code, refusal.Message, refusal.App)
		return refusal.Code
	}
	// Not `err.Error()`. Everything typed has already been answered above;
	// what is left is this daemon's own plumbing, whose words are file paths,
	// command lines and library sentences. The machine's log is where a person
	// diagnoses that, and it is the one place with nobody else reading.
	log.Printf("start: refused: %v", err)
	writePlaceRefusal(w, http.StatusInternalServerError, "internal",
		"This machine could not open that session.", "")
	return "internal"
}
