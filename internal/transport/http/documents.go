package http

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/documents"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/contract"
)

// The documents route: a listing at `/v1/sessions/{id}/documents`, and the
// bytes below it. It is the Swift app's `documentsRoute` (ProjectArtifact.swift).
//
// **This is a route that reads files off this machine's disk because a paired
// device asked for them by name, so the path rules are the feature.** They live
// in internal/adapters/documents, which says what each one is for; this file
// does the two things a transport must not get wrong either:
//
//   - **The route is matched as a whole shape rather than by `contains`**, for
//     the reason `isDocumentsReading` gives there: a route recognised by a
//     substring is a route a deeper path can impersonate.
//   - **Percent-decoding happens after the split, and that is deliberate.** A
//     segment holding `%2F` decodes to something containing a separator and it
//     stays one segment here, and `documents.File` then splits the joined path
//     again and applies every segment rule to what it finds — so `..%2F..` is
//     refused by the same clause that refuses `../..`. Reading `r.URL.Path`
//     instead would be the opposite order, because net/http has already decoded
//     it, so the raw `EscapedPath` is what is split.
//
// Not carried across: the Swift route's managed `DurableReportStore` namespace
// (a receipt-addressed reserved prefix under `project`). This daemon has no
// durable report authority, so there is nothing to serve from it and no 503 to
// answer for it; a file in the project's own artifacts directory with such a
// name is listed and served as the ordinary document it is here.

// withDocuments puts this route in front of the mux.
//
// **Not a `mux.HandleFunc`, and that is the point.** `http.ServeMux` cleans a
// path before it dispatches, so `…/documents/project/sub/../notes.md` never
// reaches a handler at all: the mux answers a redirect to the cleaned address
// and the browser follows it to the document. Nothing escapes that way — the
// cleaned path can only stay inside the mount or rise above it into a route
// that refuses — but "a path holding `..` is refused" is a rule this boundary
// states out loud, and a rule that quietly becomes a redirect is one no test
// can hold. In front of the mux the raw spelling arrives, and `..`, `.` and an
// empty segment are answered by the clause that exists for them.
//
// The gate is still outside this: `Handler` wraps the result, so an
// unauthenticated caller is refused here exactly as everywhere else.
func (s *Server) withDocuments(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isDocumentsReading(r.URL.EscapedPath()) {
			s.documentsRoute(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isDocumentsReading reports whether this path is the documents route.
func isDocumentsReading(path string) bool {
	rest, ok := strings.CutPrefix(path, "/v1/sessions/")
	if !ok {
		return false
	}
	parts := strings.Split(rest, "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] == "documents"
}

// documentsRoute answers both halves of it.
func (s *Server) documentsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "Documents are read with GET.")
		return
	}
	// The raw path, split before anything is decoded. `EscapedPath` gives back
	// the request's own spelling when it is a valid encoding of the decoded
	// path, which is exactly the string the original splits.
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/v1/sessions/"), "/")
	sessionID := decodeSegment(parts[0])
	parts = parts[2:] // drop the id and "documents"
	decoded := make([]string, len(parts))
	for i, part := range parts {
		decoded[i] = decodeSegment(part)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, sessionID)
	if err != nil || item.CWD == "" {
		// One refusal for "no such session", "no such task" and "no such file".
		// A device that may read this session's documents learns nothing from
		// telling those apart, and a device that may not learns which ids exist.
		documentRefusal(w, documents.NotFound)
		return
	}
	cwd := item.CWD

	if len(decoded) == 0 {
		rows, cut := s.documentsPayload(ctx, cwd, sessionID)
		if cut.Any() {
			// A listing shorter than what is there says so (limits N28). In a
			// header, because the body is the Swift app's one-key object and
			// both of its readers refuse a second key.
			w.Header().Set(documents.TruncatedHeader, cut.Header())
		}
		writeJSON(w, contract.DocumentList{Documents: rows})
		return
	}
	switch decoded[0] {
	case "project":
		s.serveDocument(w, documents.ProjectRoot(cwd, s.containsProjectRoot()), strings.Join(decoded[1:], "/"))
	case "task":
		if len(decoded) < 3 {
			documentRefusal(w, documents.NotFound)
			return
		}
		directory := s.taskDocumentDirectory(ctx, decoded[1], cwd)
		if directory == "" {
			documentRefusal(w, documents.NotFound)
			return
		}
		s.serveDocument(w, documents.TaskRoot(directory), strings.Join(decoded[2:], "/"))
	default:
		documentRefusal(w, documents.NotFound)
	}
}

// containsProjectRoot is whether `<session cwd>/artifacts -> elsewhere` must
// stay inside the session's own directory (documents.ProjectRoot).
//
// Off by default, which is the behaviour of the app being replicated and the
// one this machine's own checkouts rely on; on, a project root that leaves the
// session's directory is no root at all. Read per request rather than at
// startup, so turning it on is a file edit and not a restart. It is this
// daemon's setting and not the console's: the wire carries no such field in
// the app being replicated, and a switch over what a paired device may read
// should not be reachable from a paired device.
func (s *Server) containsProjectRoot() bool {
	v, err := nextconfig.Open(s.cfg.Dir).Read()
	if err != nil {
		return false
	}
	on, ok := v.Bool("documents_contain_project_root")
	return ok && on
}

// decodeSegment percent-decodes one path segment, keeping the spelling as
// written when it is not a valid encoding — the original's
// `removingPercentEncoding ?? raw`. A segment that does not decode is then
// refused by the segment rules like any other name, rather than answering a
// different question.
func decodeSegment(segment string) string {
	if out, err := url.PathUnescape(segment); err == nil {
		return out
	}
	return segment
}

// serveDocument is one document's bytes, or its typed refusal.
func (s *Server) serveDocument(w http.ResponseWriter, root, path string) {
	_, mediaType, body, err := documents.Read(root, path)
	if err != nil {
		documentRefusal(w, err)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-store")
	// Without this a browser is free to decide a document full of angle
	// brackets is HTML, and the whole reason only three extensions are served
	// is that none of them is a program.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// documentRefusal answers in the envelope the copied page reads — the Swift
// app's `{"error":{"code","message","request_id"}}` — because
// `net/live.js`'s `document()` branches on `body.error.code` and would
// otherwise report every refusal as `http_404`.
func documentRefusal(w http.ResponseWriter, err error) {
	refusal, ok := err.(documents.Refusal)
	if !ok {
		refusal = documents.NotFound
	}
	writeAuthRefusal(w, refusal.Status(), string(refusal), refusal.Message())
}

// documentsPayload is everything this project has written down, in the order
// somebody would want it: the project's own artifacts first, then each task's,
// newest task first.
//
// **A route rather than rows on the session list**, for the reason the Swift
// app gives: the list goes out on the event stream every time anything moves,
// and walking two directory trees per session per beat is a filesystem walk a
// second for a menu nobody has opened.
func (s *Server) documentsPayload(ctx context.Context, cwd, sessionID string) ([]contract.DocumentRow, documents.Cut) {
	out := []contract.DocumentRow{}
	var cut documents.Cut
	rows := func(found []documents.Document, walk documents.Cut, address, source string, task *contract.DocumentTask) {
		cut = cut.Add(walk)
		for _, document := range found {
			out = append(out, contract.DocumentRow{
				Source:   contract.DocumentSource(source),
				Path:     document.Path,
				Label:    document.Path,
				Bytes:    document.Bytes,
				Modified: document.Modified,
				// The session id goes in as it arrived, unescaped, because that
				// is what the Swift route writes — `"/v1/sessions/\(sessionID)/…"`
				// — and a tmux pane is `%86`, so the address in a row is not a
				// valid URL there either. It is the original's quirk and it costs
				// nothing: `localDocumentListing` checks only that the string
				// starts with `/` and then drops it, because the page builds its
				// own address from the locator.
				URL: "/v1/sessions/" + sessionID + "/documents/" +
					address + "/" + documents.Escaped(document.Path),
				Task: task,
			})
		}
	}
	found, walk := documents.Walk(ctx, documents.ProjectRoot(cwd, s.containsProjectRoot()))
	rows(found, walk, "project", "project", nil)
	records, omitted := s.documentTaskRecordsCut(ctx, cwd)
	cut.Tasks = omitted
	for _, record := range records {
		root := documents.TaskRoot(record.Dir)
		if root == "" {
			continue
		}
		task := contract.DocumentTask{ID: record.ID, Title: record.Title}
		found, walk := documents.Walk(ctx, root)
		rows(found, walk, "task/"+documents.Escaped(record.ID), "task", &task)
	}
	if len(out) > documents.MaximumListed {
		cut.Listed += len(out) - documents.MaximumListed
		out = out[:documents.MaximumListed]
	}
	return out, cut
}

// taskRecord is one task whose deliverables belong to a project.
type taskRecord struct {
	ID    string
	Dir   string
	Title string
}

// documentTaskRecords is the tasks whose deliverables belong to the project
// this session is in, newest first.
//
// **The registry is what authorises a task directory, not the filesystem.**
// `/tmp/.clawdline` holds a directory per task on this Mac, and a caller naming
// one that no registry here dispatched gets the same refusal as a caller naming
// a file that does not exist. Two registries are read, because this machine has
// two brokers: the Swift app's store (read-only, as everything else here reads
// it) and this daemon's own. An id in both is one record, the Swift one, since
// that is the one whose directory the child was briefed with.
func (s *Server) documentTaskRecords(ctx context.Context, cwd string) []taskRecord {
	records, _ := s.documentTaskRecordsCut(ctx, cwd)
	return records
}

// documentTaskRecordsCut is documentTaskRecords and how many older tasks were
// left off it.
func (s *Server) documentTaskRecordsCut(ctx context.Context, cwd string) ([]taskRecord, int) {
	type dated struct {
		taskRecord
		created int64
	}
	found := []dated{}
	seen := map[string]bool{}
	add := func(id, dir, title string, created int64) {
		if id == "" || dir == "" || seen[id] || !isTaskID(id) {
			return
		}
		seen[id] = true
		found = append(found, dated{taskRecord{ID: id, Dir: dir, Title: title}, created})
	}
	snap := s.swift.Read()
	for _, t := range snap.Tasks {
		if !ownsProject(t, cwd) {
			continue
		}
		add(t.ID, filepath.Join(swiftTaskRoot, t.ID), t.Title, t.Created.Unix())
	}
	if live, err := s.broker.LiveRecords(ctx); err == nil {
		for _, t := range live {
			if t.ProjectDir != cwd {
				continue
			}
			add(t.ID, s.broker.Tasks.Path(t.ID), t.Title, t.CreatedAt.Unix())
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].created > found[j].created })
	omitted := 0
	if len(found) > documents.MaximumTasksListed {
		omitted = len(found) - documents.MaximumTasksListed
		found = found[:documents.MaximumTasksListed]
	}
	out := make([]taskRecord, 0, len(found))
	for _, row := range found {
		out = append(out, row.taskRecord)
	}
	return out, omitted
}

// swiftTaskRoot is where the Swift app puts a task's directory. The literal is
// that app's, repeated here as it is repeated in
// internal/adapters/swiftstore/tasklist.go, because it is a fact about the other
// program rather than a setting of this one.
const swiftTaskRoot = "/tmp/.clawdline"

// isTaskID is the shape an id must have before it is allowed to name a
// directory.
//
// **It is not the Swift app's rule**, which is exactly 36 characters of
// `[0-9a-f-]` because every id that app mints is a UUID. This daemon's own
// broker mints eight hex characters, and applying that rule here would have
// dropped every task this daemon dispatched out of its own listing — a
// difference that looks like "no documents" rather than like a refusal. So the
// rule kept is the one the guard is actually for: hex and hyphens only, which
// admits both spellings and refuses a separator, a dot and everything else that
// could leave the directory the id is joined to. Whether the id *is* a task is
// answered by the registry above, not by its spelling.
func isTaskID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '-' {
			continue
		}
		return false
	}
	return true
}

// ownsProject is the Swift app's own test: the task was dispatched for this
// directory, or this directory *is* the isolated worktree it was given.
func ownsProject(t swiftstore.Task, cwd string) bool {
	if t.ProjectDir == cwd {
		return true
	}
	return t.Worktree != nil && t.Worktree.Path == cwd
}

// taskDocumentDirectory is one task's directory, once a registry agrees it is
// this project's task.
func (s *Server) taskDocumentDirectory(ctx context.Context, id, cwd string) string {
	if !isTaskID(id) {
		return ""
	}
	for _, record := range s.documentTaskRecords(ctx, cwd) {
		if record.ID == id {
			return record.Dir
		}
	}
	return ""
}
