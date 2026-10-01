package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal/owned"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/terminal"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// The terminal routes (plan v3 §5): ordinary shells a person opens on this
// machine's own terminal server and types into through one control lease at
// a time. api/v1/terminals.schema.json is their contract.
//
// Who may reach them, in the order it is asked:
//
//  1. Never a request that came in through Clawdline Cloud
//     (terminal_cloud_not_supported). Cloud answers a viewer with this
//     machine's own token, in process (internal/transport/cloud, Router), so
//     without this a Cloud viewer would be judged as the machine itself.
//  2. The gate's token, as every route: no token is 401.
//  3. This machine's own token (`Verdict.Local`), or a paired device that is
//     still paired and holds a terminal grant. Nothing else — not `send`,
//     not `remote_write`, not the orchestrator token — reaches a terminal.
//     Watching needs the grant as typing does; a grant holder types only
//     while it holds the lease.

// terminalState is what the terminal routes keep on the Server.
type terminalState struct {
	once sync.Once
	mu   sync.Mutex
	svc  *terminals.Service
	err  error
	// host replaces the daemon's own terminal server, for tests.
	host ports.OwnedTerminals
	// project resolves a project id to its directory; nil is the work
	// page's project catalog.
	project func(ctx context.Context, id string) (string, bool)
}

// terminalService is the service, made the first time a terminal route is
// asked, or the refusal that says why this machine has none.
func (s *Server) terminalService() (*terminals.Service, error) {
	s.term.once.Do(func() {
		host := s.term.host
		if host == nil {
			h, err := owned.New(s.cfg.Dir)
			if err != nil {
				s.term.mu.Lock()
				s.term.err = err
				s.term.mu.Unlock()
				return
			}
			host = h
		}
		svc := terminals.New(host, s.terminalAccess)
		if g := s.gate(); g.auth != nil {
			// Swept on every change to the devices or the grants. The
			// subscription lives as long as the daemon does.
			changes, _ := g.auth.Changes()
			go func() {
				for range changes {
					svc.Revalidate()
				}
			}()
		}
		s.term.mu.Lock()
		s.term.svc = svc
		s.term.mu.Unlock()
	})
	s.term.mu.Lock()
	defer s.term.mu.Unlock()
	return s.term.svc, s.term.err
}

// terminalServiceIfOpen is the service when a route has already made it.
func (s *Server) terminalServiceIfOpen() *terminals.Service {
	s.term.mu.Lock()
	defer s.term.mu.Unlock()
	return s.term.svc
}

func (s *Server) terminalStats() (leases, streams int) {
	if svc := s.terminalServiceIfOpen(); svc != nil {
		return svc.Stats()
	}
	return 0, 0
}

// terminalDiagnostics is what /v1/diagnostics says about terminals: above
// all, whether the grants file could be read, which is the one reason a
// granted device is refused that nobody could see from outside.
func (s *Server) terminalDiagnostics() *contract.TerminalDiagnostics {
	leases, streams := s.terminalStats()
	out := &contract.TerminalDiagnostics{GrantsOK: true, Leases: int64(leases), Streams: int64(streams)}
	g := s.gate()
	switch {
	case g.grants == nil:
		out.GrantsOK = false
		out.GrantsError = "the device store could not be opened"
	default:
		if err := g.grants.Err(); err != nil {
			out.GrantsOK = false
			out.GrantsError = err.Error()
		}
	}
	return out
}

// terminalAccess is whether p may still see and operate terminals. It is asked
// when a lease or a stream begins, before every frame and beat, and whenever
// the devices or the grants change.
func (s *Server) terminalAccess(p terminals.Principal) error {
	g := s.gate()
	if p.Cloud {
		line := s.cloudTerminalLine()
		if line == nil || !line.TerminalViewerAllowed(p.Device) {
			return terminal.Refuse(terminal.CodeForbidden, "this Cloud device may no longer use terminals")
		}
		if g.grants == nil {
			return terminal.Refuse(terminal.CodeForbidden, "the terminal grants could not be read")
		}
		granted, err := g.grants.Granted(p.Device)
		if err != nil || !granted {
			return terminal.Refuse(terminal.CodeForbidden, "this Cloud device has no readable terminal grant")
		}
		return nil
	}
	if g.auth == nil {
		return terminal.Refuse(terminal.CodeForbidden, "the device store could not be read")
	}
	d, ok := g.auth.Holds(p.Device)
	if !ok {
		return terminal.Refuse(terminal.CodeForbidden, "this device is no longer paired")
	}
	if p.Local && d.Local {
		return nil
	}
	if g.grants == nil {
		return terminal.Refuse(terminal.CodeForbidden, "the terminal grants could not be read")
	}
	granted, err := g.grants.Granted(p.Device)
	if err != nil {
		return terminal.Refuse(terminal.CodeForbidden,
			"the terminal grants could not be read, so no paired device may use a terminal")
	}
	if !granted {
		return terminal.Refuse(terminal.CodeForbidden,
			"this device has not been given access to this machine's terminals")
	}
	return nil
}

// CloudTerminalService is the dedicated Cloud ingress' access to the same
// terminal service and leases used by local viewers. It does not expose an
// HTTP route or mint a local bearer for a Cloud request.
func (s *Server) CloudTerminalService() (*terminals.Service, error) {
	return s.terminalService()
}

// CloudTerminalProject resolves an open request only after the signed Cloud
// ingress has authorized the viewer. It uses the local project catalog.
func (s *Server) CloudTerminalProject(ctx context.Context, id string) (string, bool) {
	return s.terminalProjectDir(ctx, id)
}

// CloudDropTerminalGrant runs only after a local Cloud pin revoke. It removes
// the grant so re-pairing the same device requires a new local decision.
func (s *Server) CloudDropTerminalGrant(device string) error {
	g := s.gate()
	if g.grants == nil {
		return errors.New("terminal grants are unavailable")
	}
	_, err := g.grants.Set(device, false, time.Now())
	return err
}

// terminalPrincipal is who is asking, or the refusal already written.
func (s *Server) terminalPrincipal(w http.ResponseWriter, r *http.Request) (terminals.Principal, bool) {
	if cloud.ViaCloud(r.Context()) {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeCloudNotSupported,
			"terminals are used on this machine's own console or a device paired with it, never through Clawdline Cloud"), nil)
		return terminals.Principal{}, false
	}
	v := accessOf(r).verdict
	if !v.Allowed {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", "This needs a paired device.")
		return terminals.Principal{}, false
	}
	p := terminals.Principal{Device: v.Device, Local: v.Local}
	if g := s.gate(); g.auth != nil {
		if d, ok := g.auth.Holds(v.Device); ok {
			p.Name = d.Name
		}
	}
	return p, true
}

// terminalsRoute is the list and the open.
func (s *Server) terminalsRoute(w http.ResponseWriter, r *http.Request) {
	p, ok := s.terminalPrincipal(w, r)
	if !ok {
		return
	}
	svc, err := s.terminalService()
	if err != nil {
		writeTerminalRefusal(w, err, nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		project := r.URL.Query().Get("project")
		rows, err := svc.List(r.Context(), p, project)
		if err != nil {
			writeTerminalRefusal(w, err, nil)
			return
		}
		out := contract.TerminalList{Terminals: []contract.Terminal{}}
		client := r.URL.Query().Get("client")
		for _, row := range rows {
			out.Terminals = append(out.Terminals, wireTerminal(row, p, client))
		}
		writeJSON(w, out)
	case http.MethodPost:
		var req contract.TerminalOpenRequest
		if !readTerminalBody(w, r, &req) {
			return
		}
		if req.ProjectID == "" || req.Cols <= 0 || req.Rows <= 0 {
			writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "that needs project_id, cols and rows"), nil)
			return
		}
		dir, ok := s.terminalProjectDir(r.Context(), req.ProjectID)
		if !ok {
			writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "this machine has no project with that id"), nil)
			return
		}
		t, err := svc.Open(r.Context(), p, ports.OpenTerminal{ProjectPath: dir, ProjectID: req.ProjectID,
			Cols: int(req.Cols), Rows: int(req.Rows)})
		if err != nil {
			writeTerminalRefusal(w, err, nil)
			return
		}
		writeJSON(w, wireTerminal(terminals.Listed{Terminal: t, Control: svc.Control(t.ID)}, p, ""))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

func (s *Server) terminalProjectDir(ctx context.Context, id string) (string, bool) {
	if s.term.project != nil {
		return s.term.project(ctx, id)
	}
	p, ok := s.workV2Project(ctx, id)
	return p.Path, ok
}

// terminalRoute is everything under one terminal's id.
func (s *Server) terminalRoute(w http.ResponseWriter, r *http.Request) {
	p, ok := s.terminalPrincipal(w, r)
	if !ok {
		return
	}
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/terminals/"), "/")
	for i, part := range parts {
		parts[i] = decodeSegment(part)
	}
	if len(parts) > 2 || parts[0] == "" {
		writeRefusal(w, http.StatusNotFound, "not_found", "No such route")
		return
	}
	id := terminal.ID(parts[0])
	if !id.Valid() {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "that is not a terminal id"), nil)
		return
	}
	verb := ""
	if len(parts) == 2 {
		verb = parts[1]
	}
	svc, err := s.terminalService()
	if err != nil {
		writeTerminalRefusal(w, err, nil)
		return
	}
	switch {
	case verb == "" && r.Method == http.MethodGet:
		row, err := svc.Find(r.Context(), p, id)
		if err != nil {
			writeTerminalRefusal(w, err, nil)
			return
		}
		writeJSON(w, wireTerminal(row, p, r.URL.Query().Get("client")))
	case verb == "" && r.Method == http.MethodDelete:
		var req contract.TerminalCloseRequest
		if !readTerminalBody(w, r, &req) || !validClient(w, req.Client) || !validNumber(w, req.Epoch) {
			return
		}
		if err := svc.Close(r.Context(), p, id, req.Client, uint64(req.Epoch)); err != nil {
			writeTerminalRefusal(w, err, nil)
			return
		}
		writeJSON(w, contract.TerminalClosed{OK: true})
	case verb == "stream" && r.Method == http.MethodGet:
		s.terminalStream(w, r, svc, p, id)
	case verb == "history" && r.Method == http.MethodGet:
		lines := terminal.MaxHistoryLines
		if raw := r.URL.Query().Get("lines"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "lines is a positive number"), nil)
				return
			}
			lines = min(n, terminal.MaxHistoryLines)
		}
		out, err := svc.History(r.Context(), p, id, lines)
		if err != nil {
			writeTerminalRefusal(w, err, nil)
			return
		}
		if out == nil {
			out = []string{}
		}
		writeJSON(w, contract.TerminalHistory{Lines: out})
	case verb == "control" && r.Method == http.MethodPost:
		var req contract.TerminalControlRequest
		if !readTerminalBody(w, r, &req) || !validClient(w, req.Client) {
			return
		}
		c, err := svc.SetControl(r.Context(), p, id, req.Client, terminals.Action(req.Action))
		if err != nil {
			writeTerminalRefusal(w, err, &terminalViewer{p: p, client: req.Client})
			return
		}
		writeJSON(w, wireControl(c, p, req.Client))
	case verb == "input" && r.Method == http.MethodPost:
		var req contract.TerminalInputRequest
		if !readTerminalBody(w, r, &req) || !validClient(w, req.Client) ||
			!validNumber(w, req.Epoch) || !validNumber(w, req.Seq) {
			return
		}
		if base64.StdEncoding.DecodedLen(len(req.Data)) > terminal.MaxInputBytes+3 {
			writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInputTooLarge,
				"one keystroke batch is at most 4 KiB; nothing was typed"), nil)
			return
		}
		data, err := base64.StdEncoding.DecodeString(req.Data)
		if err != nil || len(data) == 0 {
			writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "data is standard base64 of at least one byte"), nil)
			return
		}
		applied, dup, err := svc.Input(r.Context(), p, id, req.Client, uint64(req.Epoch), uint64(req.Seq), data)
		writeTyped(w, applied, dup, err)
	case verb == "paste" && r.Method == http.MethodPost:
		var req contract.TerminalPasteRequest
		if !readTerminalBody(w, r, &req) || !validClient(w, req.Client) ||
			!validNumber(w, req.Epoch) || !validNumber(w, req.Seq) {
			return
		}
		if req.Text == "" {
			writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "text is empty"), nil)
			return
		}
		applied, dup, err := svc.Paste(r.Context(), p, id, req.Client, uint64(req.Epoch), uint64(req.Seq), req.Text)
		writeTyped(w, applied, dup, err)
	case verb == "resize" && r.Method == http.MethodPost:
		var req contract.TerminalResizeRequest
		if !readTerminalBody(w, r, &req) || !validClient(w, req.Client) || !validNumber(w, req.Epoch) {
			return
		}
		if req.Cols <= 0 || req.Rows <= 0 {
			writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "cols and rows are positive"), nil)
			return
		}
		if err := svc.Resize(r.Context(), p, id, req.Client, uint64(req.Epoch), int(req.Cols), int(req.Rows)); err != nil {
			writeTerminalRefusal(w, err, nil)
			return
		}
		writeJSON(w, contract.TerminalClosed{OK: true})
	case verb == "" || verb == "stream" || verb == "history" || verb == "control" || verb == "input" ||
		verb == "paste" || verb == "resize":
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "That method is not answered here.")
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such route")
	}
}

func writeTyped(w http.ResponseWriter, applied uint64, dup bool, err error) {
	if err != nil {
		writeTerminalRefusal(w, err, nil)
		return
	}
	writeJSON(w, contract.TerminalInputResult{AppliedThrough: int64(applied), Duplicate: dup})
}

// terminalStream is the SSE stream (plan v3 D2): control, state and frame
// events as they happen, and a beat every five seconds.
func (s *Server) terminalStream(w http.ResponseWriter, r *http.Request, svc *terminals.Service,
	p terminals.Principal, id terminal.ID) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRefusal(w, http.StatusInternalServerError, "internal", "This connection cannot stream.")
		return
	}
	client := r.URL.Query().Get("client")
	if client != "" && !clientPattern.MatchString(client) {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "client is 1 to 64 of A-Z a-z 0-9 . _ -"), nil)
		return
	}
	watch, err := svc.Watch(r.Context(), p, id, client)
	if err != nil {
		writeTerminalRefusal(w, err, nil)
		return
	}
	defer watch.Stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": terminal\n\n")
	flusher.Flush()
	viewer := terminalViewer{p: p, client: client}
	_ = watch.Run(r.Context(), func(e terminals.Event) error {
		name, body := viewer.event(e)
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
}

// terminalViewer is who an answer is for, so a holder can be described to
// them as themselves or as somebody else.
type terminalViewer struct {
	p      terminals.Principal
	client string
}

func (v terminalViewer) event(e terminals.Event) (string, any) {
	switch e.Kind {
	case terminals.EventFrame:
		return "frame", wireFrame(e.Frame)
	case terminals.EventControl:
		return "control", wireControl(e.Control, v.p, v.client)
	case terminals.EventState:
		out := contract.TerminalState{Status: contract.TerminalStatus(e.Status)}
		if e.ClosedBy != nil {
			h := describeHolder(*e.ClosedBy, e.ClosedClient, v.p, v.client)
			out.ClosedBy = &h
		}
		return "state", out
	case terminals.EventBeat:
		out := contract.TerminalBeat{Now: unixMilli(e.Now)}
		if !e.LastCapture.IsZero() {
			out.LastCaptureAt = unixMilli(e.LastCapture)
		}
		return "beat", out
	}
	out := contract.TerminalRefusal{Error: contract.TerminalRefusalCode(terminal.CodeAccessRevoked)}
	if e.Refusal != nil {
		out.Error = contract.TerminalRefusalCode(e.Refusal.Code)
		out.Detail = e.Refusal.Detail
	}
	return "refusal", out
}

func unixMilli(t time.Time) float64 { return float64(t.UnixMilli()) / 1000 }

func describeHolder(holder terminals.Principal, holderClient string, viewer terminals.Principal, client string) contract.TerminalHolder {
	name := holder.Name
	if name == "" {
		name = "?"
	}
	same := holder.Device == viewer.Device
	return contract.TerminalHolder{Name: name, Local: holder.Local, SameDevice: same,
		SameClient: same && client != "" && holderClient == client}
}

func wireControl(c terminals.Control, viewer terminals.Principal, client string) contract.TerminalControl {
	out := contract.TerminalControl{Held: c.Held, Epoch: int64(c.Epoch)}
	if !c.Held {
		return out
	}
	h := describeHolder(c.Holder, c.Client, viewer, client)
	out.Holder = &h
	out.ExpiresAt = unixMilli(c.Expires)
	if h.SameClient {
		out.AppliedThrough = int64(c.Applied)
	}
	return out
}

func wireTerminal(row terminals.Listed, viewer terminals.Principal, client string) contract.Terminal {
	return contract.Terminal{
		ID: string(row.ID), ProjectID: row.ProjectID, Created: row.Created.Unix(),
		Status: contract.TerminalStatus(row.Status), Cols: int64(row.Cols), Rows: int64(row.Rows),
		Dir: row.Dir, Control: wireControl(row.Control, viewer, client),
	}
}

func wireFrame(f terminal.Frame) contract.TerminalFrame {
	lines := f.Lines
	if lines == nil {
		lines = []string{}
	}
	mouse := contract.TerminalMouseMode(f.Modes.Mouse)
	if mouse == "" {
		mouse = contract.TerminalMouseMode(terminal.MouseNone)
	}
	return contract.TerminalFrame{
		Rev: f.Rev, At: unixMilli(f.At), Cols: int64(f.Cols), Rows: int64(f.Rows),
		Cursor: contract.TerminalCursor{X: int64(f.Cursor.X), Y: int64(f.Cursor.Y), Visible: f.Cursor.Visible,
			Shape: contract.TerminalCursorShape(f.Cursor.Shape), Blinking: f.Cursor.Blinking},
		Modes: contract.TerminalModes{AppCursor: f.Modes.AppCursor, AppKeypad: f.Modes.AppKeypad,
			Mouse: mouse, MouseSgr: f.Modes.MouseSGR, Alt: f.Modes.Alt},
		Lines: lines, Dead: f.Dead,
	}
}

// clientPattern is a browser tab's id for itself.
var clientPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func validClient(w http.ResponseWriter, client string) bool {
	if !clientPattern.MatchString(client) {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "client is 1 to 64 of A-Z a-z 0-9 . _ -"), nil)
		return false
	}
	return true
}

func validNumber(w http.ResponseWriter, n int64) bool {
	if n < 0 {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "a number here is never negative"), nil)
		return false
	}
	return true
}

// readTerminalBody decodes one JSON object with nothing unknown in it. The
// body's size was bounded before this runs (body.go, `terminal.body_bytes`).
func readTerminalBody(w http.ResponseWriter, r *http.Request, into any) bool {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "the body could not be read"), nil)
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeInvalid, "the body is not the JSON object this route takes"), nil)
		return false
	}
	return true
}

// terminalStatus is the HTTP status each refusal is answered with.
var terminalStatus = map[terminal.RefusalCode]int{
	terminal.CodeForbidden:         http.StatusForbidden,
	terminal.CodeAccessRevoked:     http.StatusForbidden,
	terminal.CodeCloudNotSupported: http.StatusForbidden,
	terminal.CodeNotController:     http.StatusConflict,
	terminal.CodeControlled:        http.StatusConflict,
	terminal.CodeLeaseSuperseded:   http.StatusConflict,
	terminal.CodeLeaseExpired:      http.StatusConflict,
	terminal.CodeInputGap:          http.StatusConflict,
	terminal.CodeInputStateUnknown: http.StatusConflict,
	terminal.CodeFull:              http.StatusConflict,
	terminal.CodeInputTooLarge:     http.StatusRequestEntityTooLarge,
	terminal.CodeBusy:              http.StatusTooManyRequests,
	terminal.CodeViewersFull:       http.StatusTooManyRequests,
	terminal.CodeClosed:            http.StatusNotFound,
	terminal.CodeUnreachable:       http.StatusServiceUnavailable,
	terminal.CodeSocketPathTooLong: http.StatusServiceUnavailable,
	terminal.CodeUnsupported:       http.StatusNotImplemented,
	terminal.CodeInvalid:           http.StatusBadRequest,
}

// writeTerminalRefusal answers err as a TerminalRefusal. An error that is not
// a refusal is this daemon's own failure, logged and answered without its
// text.
func writeTerminalRefusal(w http.ResponseWriter, err error, viewer *terminalViewer) {
	out := contract.TerminalRefusal{}
	var rich *terminals.Refusal
	var plain *terminal.Refusal
	switch {
	case errors.As(err, &rich):
		out.Error, out.Detail = contract.TerminalRefusalCode(rich.Code), rich.Detail
		if rich.Code == terminal.CodeInputGap {
			out.AppliedThrough = int64(rich.Applied)
		}
		if rich.Holder != nil && rich.Holder.Held {
			who := terminalViewer{}
			if viewer != nil {
				who = *viewer
			}
			h := describeHolder(rich.Holder.Holder, rich.Holder.Client, who.p, who.client)
			out.Holder = &h
		}
	case errors.As(err, &plain):
		out.Error, out.Detail = contract.TerminalRefusalCode(plain.Code), plain.Detail
	default:
		log.Printf("terminals: %v", err)
		out.Error, out.Detail = contract.TerminalRefusalCode(terminal.CodeUnreachable), "the terminal server did not answer"
	}
	status, ok := terminalStatus[terminal.RefusalCode(out.Error)]
	if !ok {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

// terminalGrantRoute is POST /v1/auth/devices/{id}/terminal: this machine's
// own token gives a paired device a terminal grant, or takes it away. Never
// through Clawdline Cloud: a grant is a key to a shell.
func (g *gate) terminalGrantRoute(w http.ResponseWriter, r *http.Request, device string) {
	if cloud.ViaCloud(r.Context()) {
		writeTerminalRefusal(w, terminal.Refuse(terminal.CodeCloudNotSupported,
			"terminal grants are given on this machine, never through Clawdline Cloud"), nil)
		return
	}
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "A grant is changed with POST.")
		return
	}
	var req contract.TerminalGrantRequest
	body := readBody(r)
	grant, ok := body["grant"].(bool)
	if !ok || len(body) != 1 {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs grant: true or false, and nothing else.")
		return
	}
	req.Grant = grant
	d, held := g.auth.Holds(device)
	if !held {
		if line := cloudTerminalLine(g.dir); line != nil {
			if name, pinned := line.PinnedTerminalViewer(device); pinned {
				d.Name, d.ID, held = name, device, true
			}
		}
	}
	if !held {
		writeAuthRefusal(w, http.StatusNotFound, "not_found", "No paired device has that id.")
		return
	}
	if d.Local {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This machine's own token uses terminals without a grant.")
		return
	}
	if g.grants == nil {
		writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The terminal grants could not be opened.")
		return
	}
	row, err := g.grants.Set(device, req.Grant, time.Now())
	if err != nil {
		log.Printf("auth: a terminal grant was not changed: %v", err)
		writeAuthRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"The terminal grants could not be written; nothing was changed.")
		return
	}
	event := "device.terminal_revoke"
	if req.Grant {
		event = "device.terminal_grant"
	}
	g.files.Audit(event, map[string]string{"device": d.Name, "id": d.ID})
	out := contract.TerminalGrant{DeviceID: device, Granted: req.Grant}
	if req.Grant {
		out.GrantedAt = row.GrantedAt.Unix()
	}
	writeAuthJSON(w, out)
}

// dropTerminalGrants takes the grants of every device that is gone away with
// it: a device revoked, signed out, or all of them at once. A failure is
// logged and not the revocation's: the grant alone never lets anybody in,
// because every question asks that the device is still paired as well.
func (g *gate) dropTerminalGrants() {
	if g.grants == nil || g.auth == nil {
		return
	}
	if err := g.grants.Keep(func(device string) bool {
		_, ok := g.auth.Holds(device)
		if !ok {
			if line := cloudTerminalLine(g.dir); line != nil {
				_, ok = line.PinnedTerminalViewer(device)
			}
		}
		return ok
	}); err != nil {
		log.Printf("auth: the grants of a revoked device could not be removed: %v", err)
	}
}
