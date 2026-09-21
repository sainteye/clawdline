package http

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

// The schedule routes, as the Swift app serves them (`RemoteServer.swift`, the
// `/v1/orchestrator/schedules` cases), over this daemon's own store:
//
//	GET    /v1/orchestrator/schedules            the list
//	POST   /v1/orchestrator/schedules            make one
//	GET    /v1/orchestrator/schedules/:id        one, in full
//	PATCH  /v1/orchestrator/schedules/:id        save one (the whole file)
//	DELETE /v1/orchestrator/schedules/:id        take one away
//	POST   /v1/orchestrator/schedules/:id/run    run one now
//
// and three this daemon adds for moving schedules in and out, all behind this
// machine's orchestrator token:
//
//	POST   /v1/orchestrator/schedule-webhooks/bind   the Cloud bind command's local half
//	POST   /v1/orchestrator/schedule-imports         files from the Swift app, byte for byte
//	GET    /v1/orchestrator/schedule-exports         every stored file, byte for byte
//
// Writes have two doors, as there: a device that may send, with an
// Idempotency-Key, or this machine's orchestrator token — which makes, changes
// and removes only a schedule that runs once (`MachineRefusal`).

// scheduleBooks holds one book per state directory, shared by the routes and
// the clock, so the lane a manual run takes is the lane the timer takes.
var scheduleBooks sync.Map

func (s *Server) scheduleBook() *app.ScheduleBook {
	if b, ok := scheduleBooks.Load(s.cfg.Dir); ok {
		return b.(*app.ScheduleBook)
	}
	dir := s.cfg.Dir
	book := &app.ScheduleBook{
		Store:  s.store,
		Broker: s.broker,
		Places: func(ctx context.Context) []app.SchedulePlace {
			list := s.projectReaders().places.List(s.liveDirectories(ctx), 40)
			out := make([]app.SchedulePlace, 0, len(list))
			for _, p := range list {
				out = append(out, app.SchedulePlace{ID: p.ID, Label: p.Label, Path: p.Path})
			}
			return out
		},
		IsDirectory: func(p string) bool {
			info, err := os.Stat(p)
			return err == nil && info.IsDir()
		},
		DispatchEnabled: func() bool {
			v, err := nextconfig.Open(dir).Read()
			if err != nil {
				return true
			}
			if on, ok := v.Bool("orchestrator_enabled"); ok {
				return on
			}
			return true
		},
		ImportsEnabled: func() bool {
			v, err := nextconfig.Open(dir).Read()
			if err != nil {
				return false
			}
			on, ok := v.Bool("schedule_imports_enabled")
			return ok && on
		},
		Audit: s.audit,
		Notify: func(ctx context.Context, title, body, tag string) {
			// A push that could not be sent is logged by the sender; the run
			// it is about has already been decided and recorded.
			_, _ = s.PushSend(ctx, title, body, "", tag, "")
		},
	}
	actual, loaded := scheduleBooks.LoadOrStore(dir, book)
	if !loaded {
		// Rows the interval model left behind cannot be read as schedule files.
		// Said once, so a machine that had some learns why they are gone from
		// the list rather than finding out on the morning one does not run.
		if n, err := s.store.LegacyScheduleRows(context.Background()); err == nil && n > 0 {
			log.Printf("schedules: %d row(s) in the retired interval-model table are not read; "+
				"remake them as schedules (docs/schedules.md)", n)
		}
	}
	return actual.(*app.ScheduleBook)
}

// StartScheduleWebhooks attaches the Cloud activation method to the same book
// used by HTTP and the clock, then starts the durable claim/receipt loop.
func (s *Server) StartScheduleWebhooks(ctx context.Context, identity schedulewebhook.Identity, cloud schedulewebhook.Cloud, build string) {
	book := s.scheduleBook()
	book.Activate = cloud.Activate
	runtime := &app.ScheduleWebhookRuntime{
		Cloud: cloud, Identity: identity, Store: s.store, Book: book, Build: build,
		Log: func(format string, args ...any) { log.Printf(format, args...) },
	}
	go runtime.Run(ctx)
	log.Printf("schedule webhooks: delivery poller started")
}

func writeScheduleReply(w http.ResponseWriter, reply app.ScheduleReply) {
	if reply.OK() {
		writeJSON(w, reply.Body)
		return
	}
	if reply.Extra != nil {
		// A broker refusal, carried whole: the same envelope, with what it
		// says about the blocking task or the retry inside `error`.
		writeBrokerRefusal(w, orchestrator.Refusal{
			Status: reply.Status, Code: reply.Code, Message: reply.Message, Extra: reply.Extra})
		return
	}
	writeAuthRefusal(w, reply.Status, reply.Code, reply.Message)
}

// scheduleRaw is the bytes a schedule write sent, read once: they are part of
// what its Idempotency-Key names, so the receipt and the parser must see the
// same request rather than two reads of one body.
func scheduleRaw(r *http.Request) []byte {
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 256<<10))
	if err != nil {
		return nil
	}
	return raw
}

// scheduleBody is those bytes as a JSON object with exact numbers. Bytes that
// are not one are an empty object, as the Swift route reads them: the parser
// then says which field is missing, in its own sentence.
func scheduleBody(raw []byte) map[string]any {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var body map[string]any
	if dec.Decode(&body) != nil || body == nil {
		return map[string]any{}
	}
	return body
}

// schedules is the list and the create.
func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rows, err := s.scheduleBook().List(r.Context())
		if err != nil {
			writeAuthRefusal(w, http.StatusInternalServerError, "store_unreadable", "The schedules could not be read.")
			return
		}
		writeJSON(w, map[string]any{"schedules": rows, "at": time.Now().Unix()})
	case http.MethodPost:
		// Not cancelled with the request: a phone that drops between the write
		// and its read-back must not leave a schedule the answer says was removed.
		ctx := context.WithoutCancel(r.Context())
		s.scheduleWriting(w, r, true, func(machine bool, body map[string]any) app.ScheduleReply {
			return s.scheduleBook().Create(ctx, body, machine)
		})
	default:
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
	}
}

// scheduleRoute is everything under one schedule id.
func (s *Server) scheduleRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/orchestrator/schedules/")
	run := strings.HasSuffix(rest, "/run")
	id := decodeSegment(strings.TrimSuffix(rest, "/run"))
	book := s.scheduleBook()
	ctx := context.WithoutCancel(r.Context())
	switch {
	case run && r.Method == http.MethodPost:
		// A manual run through this machine's token needs no key, as there;
		// a device's does.
		s.scheduleWriting(w, r, !machineAuthed(r),
			func(bool, map[string]any) app.ScheduleReply { return book.Run(ctx, id) })
	case run:
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No such route")
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		record, ok, err := book.Detail(r.Context(), id)
		if err != nil {
			writeAuthRefusal(w, http.StatusInternalServerError, "store_unreadable", "The schedule could not be read.")
			return
		}
		if !ok {
			writeAuthRefusal(w, http.StatusNotFound, "not_found", "No schedule named that")
			return
		}
		writeJSON(w, map[string]any{"schedule": record})
	case r.Method == http.MethodPatch:
		s.scheduleWriting(w, r, true, func(machine bool, body map[string]any) app.ScheduleReply {
			return book.Update(ctx, id, body, machine)
		})
	case r.Method == http.MethodDelete:
		s.scheduleWriting(w, r, true, func(machine bool, _ map[string]any) app.ScheduleReply {
			return book.Delete(ctx, id, machine)
		})
	default:
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
	}
}

// scopeSchedules is the receipt scope of a schedule write.
const scopeSchedules = "schedules"

// scheduleWriteFiled is which answers a key keeps. `429` and anything from
// five hundred up are not filed — they are facts about this moment, not about
// the request, and filing them would refuse the retry they ask for.
func scheduleWriteFiled(status int) bool {
	return status != http.StatusTooManyRequests && status < 500
}

// scheduleWriting is `schedulingWrite`: this machine's token, or a device that
// may send; the key; and the first answer to that key, for the whole of the
// key's window.
//
// **The answer is filed in the store's receipts, not in this process** (D03,
// G15). It used to be a map here for ten minutes, which is the wrong half of
// the problem: the moment a caller is least sure a schedule was made is after
// the daemon restarted under it, and that map was gone. Across Clawdline Cloud
// it is worse again — the answer can be lost after this Mac wrote the file,
// and "try again" was then a second schedule with a second id. So a key names
// one request, and a request is a method, a route and the exact body; a retry
// under it is answered with the first answer, and the same key with another
// body is refused. It is the spelling a session's writes already have
// (actions.go), which is the point: one idempotency, not three.
func (s *Server) scheduleWriting(w http.ResponseWriter, r *http.Request, needKey bool,
	answer func(machine bool, body map[string]any) app.ScheduleReply) {
	machine := machineAuthed(r)
	if !machine && !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	raw := scheduleRaw(r)
	run := func(w http.ResponseWriter) { writeScheduleReply(w, answer(machine, scheduleBody(raw))) }
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || s.store == nil {
		if needKey {
			writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
			return
		}
		run(w)
		return
	}
	who := accessOf(r).verdict.Device
	if machine {
		who = "orchestrator"
	}
	k := store.ReceiptKey{Scope: scopeSchedules, Actor: who, Key: key}
	s.receipted(w, r, k, requestDigest([]byte(r.Method), []byte(routePath(r)), raw),
		scheduleWriteFiled, run)
}

// scheduleWebhookBindRoute is the local half of the Cloud's
// `schedule-webhook-bind-v1` command. The Swift app answers that command in
// process; this daemon's Cloud line reaches local capabilities through routes,
// so the command's own route name (`CloudLocalRoute`) is served here, to this
// machine's token only.
func (s *Server) scheduleWebhookBindRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "That needs the orchestrator token.")
		return
	}
	body := scheduleBody(scheduleRaw(r))
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	requestID, _ := body["request_id"].(string)
	hookID, _ := body["hook_id"].(string)
	scheduleID, _ := body["schedule_id"].(string)
	var replace *string
	replaceRaw, hasReplace := body["replace_hook_id"]
	if v, ok := replaceRaw.(string); ok {
		replace = &v
	}
	if strings.Join(keys, ",") != "hook_id,replace_hook_id,request_id,schedule_id" || requestID == "" ||
		hookID == "" || scheduleID == "" || !hasReplace || (replaceRaw != nil && replace == nil) {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "Could not read that request")
		return
	}
	status, answer := s.scheduleBook().BindWebhook(r.Context(), requestID, hookID, scheduleID, replace, "local")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(answer)
}

// scheduleImportRoute stores schedule files handed over whole: `{"files":
// {"<schedule-id>.json": "<the file's bytes>"}}`. The bytes are kept exactly,
// so an export afterwards can be compared with the source file by hash.
func (s *Server) scheduleImportRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "That needs the orchestrator token.")
		return
	}
	var body struct {
		Files map[string]string `json:"files"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, scheduleImportBodyLimit))
	if err != nil || json.Unmarshal(raw, &body) != nil || len(body.Files) == 0 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", `Send {"files": {"<schedule-id>.json": "<file contents>"}}.`)
		return
	}
	names := make([]string, 0, len(body.Files))
	for name := range body.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]app.ImportFile, 0, len(names))
	for _, name := range names {
		files = append(files, app.ImportFile{Name: name, Body: []byte(body.Files[name])})
	}
	book := s.scheduleBook()
	results, refusal := book.Import(context.WithoutCancel(r.Context()), files)
	if refusal != nil {
		writeScheduleReply(w, *refusal)
		return
	}
	rows, _ := book.List(r.Context())
	writeJSON(w, map[string]any{"ok": true, "results": results, "schedules": rows, "at": time.Now().Unix()})
}

// scheduleExportRoute is every stored schedule file, byte for byte.
func (s *Server) scheduleExportRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "No such route")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "That needs the orchestrator token.")
		return
	}
	files, err := s.scheduleBook().Export(r.Context())
	if err != nil {
		writeAuthRefusal(w, http.StatusInternalServerError, "store_unreadable", "The schedules could not be read.")
		return
	}
	writeJSON(w, map[string]any{"files": files, "at": time.Now().Unix()})
}
