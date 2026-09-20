package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	"github.com/sainteye/clawdline-go/internal/domain/snippet"
)

// The snippet routes, as the Swift app serves them (`RemoteServer.swift`, the
// `/v1/snippets` cases), over this daemon's own store:
//
//	GET    /v1/snippets              every snippet, or one session's two groups
//	POST   /v1/snippets              make one
//	POST   /v1/snippets/order        one group's complete order
//	PATCH  /v1/snippets/:id          save one
//	DELETE /v1/snippets/:id          take one away
//
// A read passes the ordinary paired-device door. All four writes pass the same
// switch a schedule's write does — this machine's orchestrator token, or a
// device that may send — and every one of them carries an Idempotency-Key,
// because a snippet is a setting and a retry after a dropped connection must
// not leave a second copy of it. The key is filed in the store's own receipt
// table (D03), not in a map in this process: a retry after a restart is the
// moment a client is least sure its write landed.
//
// **The store decides everything about a snippet and this file decides nothing
// about one.** Validation, both limits, the position a new one lands at and the
// disk brake are `internal/domain/snippet` and `internal/adapters/store`, so no
// second caller can acquire a different idea of what a snippet may be.
//
// What this file does own is the project a session is standing in, because that
// answer needs the icon registry and git, and neither belongs in the store.

// scopeSnippets is these routes' receipt scope (see receipts.go).
const scopeSnippets = "snippets"

// snippetBodyLimit is one snippet's body as JSON: the store's 4,000-byte text
// and 200-byte title, with room for a path, the escaping a client that escapes
// everything outside ASCII does, and a complete order of a hundred ids.
const snippetBodyLimit = 64 << 10

// snippetWire is one snippet on the wire, in the Swift store's field names.
//
// `project` is left out of a global snippet rather than sent as null: the
// console builds its next request out of what it was given, and the store
// refuses a body carrying `project` beside `scope: "global"` at all.
type snippetWire struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Scope     string `json:"scope"`
	Project   string `json:"project,omitempty"`
	Position  int64  `json:"position"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// snippetProjectWire is the project this machine resolved, which the sheet
// groups under and the session header names. A browser never chooses it.
type snippetProjectWire struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type snippetListWire struct {
	Project  *snippetProjectWire `json:"project,omitempty"`
	Snippets []snippetWire       `json:"snippets"`
}

type snippetOrderedWire struct {
	OK       bool          `json:"ok"`
	Scope    string        `json:"scope"`
	Project  string        `json:"project,omitempty"`
	Snippets []snippetWire `json:"snippets"`
}

type snippetDeletedWire struct {
	OK      bool   `json:"ok"`
	Deleted string `json:"deleted"`
}

// snippetRefusalWire is this daemon's own refusal envelope with the numbers a
// code cannot carry beside it.
//
// `error` and `detail` stay two strings exactly where every other refusal on
// this daemon puts them, because that is what a client branches on; `counts` is
// for a person and for a later client, and nothing may be parsed out of
// `detail` instead.
type snippetRefusalWire struct {
	Error  string           `json:"error"`
	Detail string           `json:"detail"`
	Counts map[string]int64 `json:"counts,omitempty"`
}

func snippetOf(r snippet.Record) snippetWire {
	return snippetWire{
		ID: r.ID, Title: r.Title, Body: r.Body, Scope: string(r.Scope),
		Project: r.Project, Position: r.Position,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func snippetsOf(records []snippet.Record) []snippetWire {
	out := make([]snippetWire, 0, len(records))
	for _, r := range records {
		out = append(out, snippetOf(r))
	}
	return out
}

// snippetLimits are the two registered rows as this machine resolved them: an
// override may have lowered either, and the route enforces what it resolved.
func snippetLimits() snippet.Limits {
	return snippet.Limits{
		Total: CapacityLimit(capacity.SnippetsTotal),
		Scope: CapacityLimit(capacity.SnippetsScope),
	}
}

func writeSnippetRefusal(w http.ResponseWriter, ref *snippet.Refusal) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ref.Status)
	_ = json.NewEncoder(w).Encode(snippetRefusalWire{
		Error: ref.Code, Detail: ref.Message, Counts: ref.Extra})
}

// writeSnippetFailure is everything that is not a refusal: a spent disk brake,
// a contended store, a write that did not land.
func writeSnippetFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.SnippetRateLimited):
		writeRefusal(w, http.StatusTooManyRequests, "rate_limited",
			"This machine has taken ten snippet writes in ten minutes.")
	case errors.Is(err, store.ErrBusy):
		writeRefusal(w, http.StatusTooManyRequests, "busy",
			"The store was held by another writer; nothing was done. Retry.")
	default:
		writeRefusal(w, http.StatusInternalServerError, "write_failed",
			"The snippet could not be written.")
	}
}

// snippetsRoute is the list and the create.
func (s *Server) snippetsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.snippetListRoute(w, r)
	case http.MethodPost:
		s.snippetWrite(w, r, func(ctx context.Context, out http.ResponseWriter, body map[string]any) {
			s.snippetCreate(ctx, out, body)
		})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
	}
}

// snippetListRoute answers the list.
//
// Without `session` it is every snippet on this machine, which is what a reader
// with no session open asks for. With one it is that session's two groups —
// this project's snippets first, then the ones that belong to every project —
// and the project this machine resolved beside them, so the browser never has
// to derive a second answer to that question.
func (s *Server) snippetListRoute(w http.ResponseWriter, r *http.Request) {
	records, err := s.store.Snippets(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable",
			"The snippets could not be read.")
		return
	}
	// The query value is used exactly as the parser handed it over. A tmux pane
	// id begins with a per cent sign — `%12` is one — so decoding it a second
	// time turns the id into a control character; every pane from `%10` up once
	// answered 404 that way, while `%1` to `%9` happened to survive it.
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		writeJSON(w, snippetListWire{Snippets: snippetsOf(records)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, sessionID)
	if err != nil || strings.TrimSpace(item.CWD) == "" {
		// The Swift route's 404. This daemon cannot say which project that
		// session is in, and a list filtered under a guessed project would be
		// somebody else's snippets.
		writeRefusal(w, http.StatusNotFound, "not_found", "No session named that")
		return
	}
	project := s.snippetProject(ctx, item.CWD)
	writeJSON(w, snippetListWire{
		Project:  &snippetProjectWire{Key: project.key, Label: project.label},
		Snippets: snippetsOf(snippet.InScope(records, project.key)),
	})
}

// snippetRoute is everything under /v1/snippets/: the order, and one snippet.
//
// One handler for both because Go's mux matches the longest registered prefix,
// so `/v1/snippets/order` arrives here and not at the list's own pattern — and
// `order` is therefore a name no snippet id may be. Every id this daemon mints
// is hyphenated hex, so none is.
func (s *Server) snippetRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/snippets/")
	if rest == "order" {
		s.snippetOrderRoute(w, r)
		return
	}
	// Lower case, as the Swift store canonicalises an id: the ids this daemon
	// mints are lowercase hex, so one typed in capitals finds its snippet
	// rather than being told it does not exist.
	id := strings.ToLower(decodeSegment(rest))
	switch r.Method {
	case http.MethodPatch:
		s.snippetWrite(w, r, func(ctx context.Context, out http.ResponseWriter, body map[string]any) {
			s.snippetUpdate(ctx, out, id, body)
		})
	case http.MethodDelete:
		s.snippetWrite(w, r, func(ctx context.Context, out http.ResponseWriter, _ map[string]any) {
			s.snippetDelete(ctx, out, id)
		})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
	}
}

// snippetOrderRoute writes one group's complete order.
func (s *Server) snippetOrderRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
		return
	}
	s.snippetWrite(w, r, func(ctx context.Context, out http.ResponseWriter, body map[string]any) {
		s.snippetOrder(ctx, out, body)
	})
}

// snippetWrite is the door, the key and the receipt every snippet write passes.
//
// `429` and anything from five hundred up are not filed: they are facts about
// this moment rather than about the request, and filing one would refuse the
// retry it is asking for.
func (s *Server) snippetWrite(w http.ResponseWriter, r *http.Request,
	answer func(ctx context.Context, out http.ResponseWriter, body map[string]any)) {
	if !machineAuthed(r) && !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		writeRefusal(w, http.StatusBadRequest, "idempotency_key_required",
			"Every snippet write carries an Idempotency-Key header (at most 200 characters), "+
				"so a retry is answered, not repeated.")
		return
	}
	raw, body, ok := snippetBody(w, r)
	if !ok {
		return
	}
	principal := "orchestrator"
	if !machineAuthed(r) {
		principal = personPrincipal(r)
	}
	// Not cancelled with the request: a phone that drops between the write and
	// its read-back must not leave a snippet no answer ever named.
	ctx := context.WithoutCancel(r.Context())
	s.receipted(w, r,
		store.ReceiptKey{Scope: scopeSnippets, Actor: principal, Key: key},
		requestDigest([]byte(r.Method), []byte(routePath(r)), raw),
		func(status int) bool { return status != http.StatusTooManyRequests && status < 500 },
		func(out http.ResponseWriter) { answer(ctx, out, body) })
}

// snippetBody reads one write's body as JSON and hands back its bytes as well,
// because the bytes are what the request's receipt is filed under.
//
// A body that is not one JSON object is an empty one, as the Swift route reads
// it: the parser then says which field is missing, in its own sentence, rather
// than this reading inventing a second way to say the same thing. A DELETE
// carries no body at all and that is the same empty object.
func snippetBody(w http.ResponseWriter, r *http.Request) ([]byte, map[string]any, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, snippetBodyLimit+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The body could not be read.")
		return nil, nil, false
	}
	if len(raw) > snippetBodyLimit {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large",
			"A snippet write is at most 64 KiB.")
		return nil, nil, false
	}
	body := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		var parsed map[string]any
		if dec.Decode(&parsed) == nil && parsed != nil {
			body = parsed
		}
	}
	return raw, body, true
}

func (s *Server) snippetCreate(ctx context.Context, w http.ResponseWriter, body map[string]any) {
	fields, ref := s.snippetFields(body)
	if ref != nil {
		s.auditSnippet("snippet.created", "", false, ref.Code)
		writeSnippetRefusal(w, ref)
		return
	}
	made, ref, err := s.store.CreateSnippet(ctx, orchestrator.NewUUID(), fields, snippetLimits(), time.Now())
	if err != nil {
		s.auditSnippet("snippet.created", "", false, snippetWhy(err))
		writeSnippetFailure(w, err)
		return
	}
	if ref != nil {
		s.auditSnippet("snippet.created", "", false, ref.Code)
		writeSnippetRefusal(w, ref)
		return
	}
	s.auditSnippet("snippet.created", made.ID, true, "")
	writeJSON(w, snippetOf(made))
}

func (s *Server) snippetUpdate(ctx context.Context, w http.ResponseWriter, id string, body map[string]any) {
	if id == "" {
		writeSnippetRefusal(w, snippet.NotFound())
		return
	}
	fields, ref := s.snippetFields(body)
	if ref != nil {
		s.auditSnippet("snippet.updated", id, false, ref.Code)
		writeSnippetRefusal(w, ref)
		return
	}
	saved, ref, err := s.store.UpdateSnippet(ctx, id, fields, snippetLimits(), time.Now())
	if err != nil {
		s.auditSnippet("snippet.updated", id, false, snippetWhy(err))
		writeSnippetFailure(w, err)
		return
	}
	if ref != nil {
		s.auditSnippet("snippet.updated", id, false, ref.Code)
		writeSnippetRefusal(w, ref)
		return
	}
	s.auditSnippet("snippet.updated", id, true, "")
	writeJSON(w, snippetOf(saved))
}

func (s *Server) snippetDelete(ctx context.Context, w http.ResponseWriter, id string) {
	if id == "" {
		writeSnippetRefusal(w, snippet.NotFound())
		return
	}
	ref, err := s.store.DeleteSnippet(ctx, id, time.Now())
	if err != nil {
		s.auditSnippet("snippet.deleted", id, false, snippetWhy(err))
		writeSnippetFailure(w, err)
		return
	}
	if ref != nil {
		s.auditSnippet("snippet.deleted", id, false, ref.Code)
		writeSnippetRefusal(w, ref)
		return
	}
	s.auditSnippet("snippet.deleted", id, true, "")
	writeJSON(w, snippetDeletedWire{OK: true, Deleted: id})
}

// snippetOrder writes the order of exactly one group.
//
// The body names one scope and — for a project scope, and only for one — the
// project, exactly as a create does, so a group cannot mean two things. The
// order itself may reorder that group's members and may never add or remove
// one, which the store checks against what is actually in it.
func (s *Server) snippetOrder(ctx context.Context, w http.ResponseWriter, body map[string]any) {
	malformed := func(detail string) {
		s.auditSnippet("snippet.ordered", "", false, snippet.CodeMalformed)
		writeSnippetRefusal(w, &snippet.Refusal{Status: http.StatusBadRequest,
			Code: snippet.CodeMalformed, Message: detail})
	}
	known := map[string]bool{"scope": true, "order": true, "project": true}
	for key := range body {
		if !known[key] {
			malformed("A snippet order names one scope, its complete order, and a project for a project scope.")
			return
		}
	}
	scope, _ := body["scope"].(string)
	project, projectIsText := body["project"].(string)
	_, hasProject := body["project"]
	list, isList := body["order"].([]any)
	if !isList || (hasProject && !projectIsText) {
		malformed("A snippet order names one scope, its complete order, and a project for a project scope.")
		return
	}
	ids := make([]string, 0, len(list))
	for _, entry := range list {
		text, ok := entry.(string)
		if !ok {
			malformed("A snippet order contains snippet ids.")
			return
		}
		ids = append(ids, strings.ToLower(text))
	}
	fields := snippet.Fields{
		Scope: snippet.Scope(scope), HasScope: scope != "",
		Project: project, HasProject: hasProject,
	}.Normalized(snippetPathSpelling)
	if !snippet.KnownScope(fields.Scope) || !snippet.ScopeAgrees(fields) {
		s.auditSnippet("snippet.ordered", "", false, snippet.CodeScopeMismatch)
		writeSnippetRefusal(w, &snippet.Refusal{Status: http.StatusBadRequest,
			Code:    snippet.CodeScopeMismatch,
			Message: "Project snippets need one project path; global snippets cannot carry one."})
		return
	}
	ordered, ref, err := s.store.OrderSnippets(ctx, fields.Scope, fields.Project, ids, time.Now())
	if err != nil {
		s.auditSnippet("snippet.ordered", "", false, snippetWhy(err))
		writeSnippetFailure(w, err)
		return
	}
	if ref != nil {
		s.auditSnippet("snippet.ordered", "", false, ref.Code)
		writeSnippetRefusal(w, ref)
		return
	}
	s.auditSnippet("snippet.ordered", "", true, "")
	writeJSON(w, snippetOrderedWire{OK: true, Scope: string(fields.Scope),
		Project: fields.Project, Snippets: snippetsOf(ordered)})
}

// snippetFields reads a write's fields and puts the project path it names into
// one spelling. Every byte bound is taken inside ParseFields, on the values as
// they arrived, before that spelling is applied to any of them.
func (s *Server) snippetFields(body map[string]any) (snippet.Fields, *snippet.Refusal) {
	fields, ref := snippet.ParseFields(body)
	if ref != nil {
		return fields, ref
	}
	return fields.Normalized(snippetPathSpelling), nil
}

// snippetScope is a project as the sheet groups under it: the key snippets are
// filed against, and what to call it on screen.
type snippetScope struct {
	key   string
	label string
}

// snippetProject is the project a session standing in cwd is in, by the Swift
// store's rule and in its order:
//
//  1. the icon registry row cwd falls under, which is how a session in a
//     subdirectory of a registered project is in that project;
//  2. failing that, the checkout an isolated worktree was cut from, asked of
//     the registry again — so a session in a worktree and a session in the
//     checkout it came from agree about the key *and* about the name;
//  3. failing that, cwd itself.
//
// Both sides of every comparison go through one spelling first. One side alone
// is not a comparison: a cwd whose symbolic links have been resolved can never
// match a registered path whose have not, and a session in a registered project
// that is not a repository then silently saw an empty list.
func (s *Server) snippetProject(ctx context.Context, cwd string) snippetScope {
	here := snippetPathSpelling(cwd)
	if scope, ok := s.snippetRegistryScope(here); ok {
		return scope
	}
	// `--git-common-dir`, which `Toplevel` asks, and not `--show-toplevel`: in
	// an isolated worktree the latter is the child's own checkout and the
	// former points at the repository it was cut from.
	if checkout, err := git.New().Toplevel(ctx, here); err == nil && checkout != "" {
		if checkout = snippetPathSpelling(checkout); checkout != here {
			if scope, ok := s.snippetRegistryScope(checkout); ok {
				return scope
			}
			return snippetScope{key: checkout, label: filepath.Base(checkout)}
		}
	}
	return snippetScope{key: here, label: filepath.Base(here)}
}

// snippetRegistryScope is step 1, asked of an already-spelled directory.
//
// The registry's own paths are put through the same spelling, because a row
// written with a symbolic link in it would otherwise never match a resolved
// cwd — and on a Mac a project under a linked directory is the common case.
// The registry's longest-match rule is asked for rather than restated: a second
// almost-identical project matcher here is the thing that drifts.
func (s *Server) snippetRegistryScope(cwd string) (snippetScope, bool) {
	if s.icons == nil {
		return snippetScope{}, false
	}
	path, label, ok := s.icons.MatchSpelled(cwd, snippetPathSpelling)
	if !ok {
		return snippetScope{}, false
	}
	if label == "" {
		label = filepath.Base(path)
	}
	return snippetScope{key: path, label: label}, true
}

// snippetPathSpelling is one spelling for one directory: cleaned, and with its
// symbolic links resolved where they can be.
//
// A path that cannot be resolved — one that no longer exists, which a stored
// project path may well be — keeps its cleaned form rather than becoming empty,
// so a project whose directory has been moved away still matches its own
// snippets instead of losing them.
func snippetPathSpelling(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil || resolved == "" {
		return cleaned
	}
	return resolved
}

// snippetWhy is what an audit line calls a failure that is not a refusal.
func snippetWhy(err error) string {
	switch {
	case errors.Is(err, store.SnippetRateLimited):
		return "rate_limited"
	case errors.Is(err, store.ErrBusy):
		return "busy"
	default:
		return "write_failed"
	}
}

// auditSnippet records one write, as the Swift store's `snippet.*` audit lines
// do. A snippet's text is never in it: what is recorded is which snippet and
// whether it was written, never what it says.
func (s *Server) auditSnippet(event, id string, ok bool, why string) {
	fields := map[string]string{"ok": "0"}
	if ok {
		fields["ok"] = "1"
	}
	if id != "" {
		fields["snippet"] = id
	}
	if why != "" {
		fields["why"] = why
	}
	s.audit(event, fields)
}

// snippetsReading is what the capacity beat measures: how many snippets this
// machine holds, and the fullest single group of them, named.
func (s *Server) snippetsReading(fullest bool) capacity.Reading {
	total, group, name, err := s.store.SnippetCounts(context.Background())
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	if !fullest {
		return capacity.Reading{Known: true, Used: total}
	}
	reading := capacity.Reading{Known: true, Used: group}
	if name != "" {
		reading.Note = "fullest group: " + name
	}
	return reading
}
