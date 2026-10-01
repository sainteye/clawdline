package cloud

// Signed Cloud terminal requests enter here, never through the HTTP Router.
// The transport has already verified the envelope signature and replay window;
// the sender in Inbound is the only principal this file accepts.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

const (
	CloudTerminalConnectionsLimit       = 16
	CloudTerminalViewerConnectionsLimit = 2
	CloudTerminalRequestBytesLimit      = 6<<20 + 4<<10
	CloudTerminalReceiptsLimit          = 64
	CloudTerminalKeySecondsLimit        = 600
	CloudTerminalIngressLimit           = 16
	CloudTerminalRefusalsLimit          = 16
)

// TerminalCapacity reports the terminal rail without exposing its keys or
// terminal contents to diagnostics.
func (l *Link) TerminalCapacity(name string) capacity.Reading {
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	r := capacity.Reading{Known: true}
	switch name {
	case capacity.CloudTerminalConnections:
		r.Used = int64(len(l.terminalConnections))
	case capacity.CloudTerminalViewerConnections:
		counts := map[string]int64{}
		for _, c := range l.terminalConnections {
			counts[c.viewer]++
			if counts[c.viewer] > r.Used {
				r.Used = counts[c.viewer]
			}
		}
	case capacity.CloudTerminalReceipts:
		for _, c := range l.terminalConnections {
			if int64(len(c.receiptOrder)) > r.Used {
				r.Used = int64(len(c.receiptOrder))
			}
		}
	case capacity.CloudTerminalIngress:
		r.Used = int64(len(l.terminalRequests))
	case capacity.CloudTerminalRefusals:
		r.Used = int64(len(l.terminalRefusals))
	case capacity.CloudTerminalRosterRefresh, capacity.CloudTerminalRosterDeadline,
		capacity.CloudTerminalRequestBytes, capacity.CloudTerminalKeySeconds:
		r.Note = "per-operation limit; no requests retained"
	default:
		return capacity.Unmeasured("unknown terminal capacity row")
	}
	return r
}

var terminalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type terminalRequest struct {
	V          int             `json:"v"`
	Type       string          `json:"type"`
	RequestID  string          `json:"request_id"`
	Connection string          `json:"connection"`
	Operation  string          `json:"operation"`
	TerminalID string          `json:"terminal_id,omitempty"`
	ProjectID  string          `json:"project_id,omitempty"`
	Client     string          `json:"client,omitempty"`
	Epoch      uint64          `json:"epoch,omitempty"`
	Seq        uint64          `json:"seq,omitempty"`
	KeyID      string          `json:"key_id,omitempty"`
	Key        string          `json:"key,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

type terminalReceipt struct {
	V          int    `json:"v"`
	Type       string `json:"type"`
	RequestID  string `json:"request_id"`
	Connection string `json:"connection"`
	Operation  string `json:"operation"`
	TerminalID string `json:"terminal_id,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Result     any    `json:"result,omitempty"`
}

type terminalConnection struct {
	viewer, id, keyID string
	key               domaincloud.ContentKey
	expires           time.Time
	terminalID        terminal.ID
	client            string
	frameSeq          uint64
	publishedFrameSeq uint64
	watchCancel       context.CancelFunc
	watchReady        chan struct{}
	receipts          map[string][]byte
	receiptOrder      []string
	rekeyPending      bool
}

func terminalConnectionID(viewer, connection string) string { return viewer + "/" + connection }

func (l *Link) deliverTerminal(in Inbound) {
	select {
	case l.terminalRequests <- in:
	default:
		select {
		case l.terminalRefusals <- in:
		default:
			l.logf("cloud terminal: ingress and refusal lanes are full; sender=%s seq=%d was unanswered", in.Sender, in.Sequence)
		}
	}
}

func (l *Link) runTerminal(ctx context.Context) {
	refusalDone := make(chan struct{})
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		ticker := time.NewTicker(CloudTerminalRosterRefreshLimit * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.sweepTerminalConnections()
			}
		}
	}()
	go func() {
		defer close(refusalDone)
		for {
			select {
			case <-ctx.Done():
				return
			case in := <-l.terminalRefusals:
				l.refuseTerminalBusy(ctx, in)
			}
		}
	}()
	defer func() { <-refusalDone; <-sweepDone }()
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-l.terminalRequests:
			l.handleTerminal(ctx, in)
		}
	}
}

func (l *Link) sweepTerminalConnections() {
	l.terminalMu.Lock()
	connections := make([]*terminalConnection, 0, len(l.terminalConnections))
	for _, c := range l.terminalConnections {
		connections = append(connections, c)
	}
	l.terminalMu.Unlock()
	svc, err := l.terminalService()
	if err != nil {
		l.closeAllTerminalConnections()
		return
	}
	for _, c := range connections {
		if !c.expires.After(l.opts.Now()) || svc.Allow(terminals.Principal{Device: c.viewer, Cloud: true}) != nil {
			l.closeTerminalConnection(c)
		}
	}
}

func (l *Link) refuseTerminalBusy(ctx context.Context, in Inbound) {
	req, err := decodeTerminalRequest(in.Plaintext)
	if err != nil {
		return
	}
	c := l.getTerminalConnection(in.Sender, req.Connection)
	if c == nil && (req.Operation == "open_connection" || req.Operation == "rekey_connection") {
		key, err := parseConnectionKey(req)
		if err != nil {
			return
		}
		c = &terminalConnection{viewer: in.Sender, id: req.Connection, keyID: req.KeyID, key: key,
			receipts: map[string][]byte{}}
	}
	if c == nil {
		return
	}
	l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
		Status: "refused", Error: string(terminal.CodeBusy)})
}

func (l *Link) closeAllTerminalConnections() {
	l.terminalMu.Lock()
	retired := make([]*terminalConnection, 0, len(l.terminalConnections))
	for id, c := range l.terminalConnections {
		if c.watchCancel != nil {
			c.watchCancel()
		}
		delete(l.terminalConnections, id)
		retired = append(retired, c)
	}
	l.terminalMu.Unlock()
	for _, c := range retired {
		l.retireTerminalConnection(c)
	}
}

func (l *Link) revokeTerminalViewer(viewer string) error {
	l.terminalMu.Lock()
	retired := []*terminalConnection{}
	for id, c := range l.terminalConnections {
		if c.viewer == viewer {
			if c.watchCancel != nil {
				c.watchCancel()
			}
			delete(l.terminalConnections, id)
			retired = append(retired, c)
		}
	}
	l.terminalMu.Unlock()
	for _, c := range retired {
		l.retireTerminalConnection(c)
	}
	if l.opts.DropTerminalGrant != nil {
		return l.opts.DropTerminalGrant(viewer)
	}
	return nil
}

func decodeTerminalRequest(raw []byte) (terminalRequest, error) {
	var req terminalRequest
	if len(raw) == 0 || len(raw) > CloudTerminalRequestBytesLimit {
		return req, terminal.Refuse(terminal.CodeInvalid, "terminal request size is invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF ||
		req.V != 1 || req.Type != "terminal_request" || !terminalUUID.MatchString(req.RequestID) ||
		!validConnection(req.Connection) {
		return terminalRequest{}, terminal.Refuse(terminal.CodeInvalid, "terminal request shape is invalid")
	}
	return req, nil
}

func validConnection(id string) bool {
	if len(id) != 22 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 16 && base64.RawURLEncoding.EncodeToString(raw) == id
}

func parseConnectionKey(req terminalRequest) (domaincloud.ContentKey, error) {
	if len(req.KeyID) != 25 || !strings.HasPrefix(req.KeyID, "rk-") || !validConnection(req.KeyID[3:]) {
		return domaincloud.ContentKey{}, terminal.Refuse(terminal.CodeInvalid, "terminal key id is invalid")
	}
	raw, err := base64.StdEncoding.DecodeString(req.Key)
	if err != nil || base64.StdEncoding.EncodeToString(raw) != req.Key {
		return domaincloud.ContentKey{}, terminal.Refuse(terminal.CodeInvalid, "terminal key is invalid")
	}
	return domaincloud.ContentKeyFromBytes(raw)
}

func (l *Link) terminalService() (*terminals.Service, error) {
	if l.opts.TerminalService == nil {
		return nil, terminal.Refuse(terminal.CodeUnsupported, "Cloud terminals are unavailable")
	}
	return l.opts.TerminalService()
}

// handleTerminal handles only the dedicated termi rail. It is deliberately
// separate from cloudops.Bridge and Router, which cannot grant terminal access.
func (l *Link) handleTerminal(ctx context.Context, in Inbound) {
	if in.Channel != "termi/"+l.identity.MachineID+"/"+in.Sender || in.Class != string(domaincloud.ClassCtl) {
		return
	}
	req, err := decodeTerminalRequest(in.Plaintext)
	if err != nil {
		l.logf("cloud terminal: malformed signed request from %s: %v", in.Sender, err)
		return
	}
	svc, err := l.terminalService()
	if err != nil {
		l.logf("cloud terminal: service unavailable: %v", err)
		c := l.getTerminalConnection(in.Sender, req.Connection)
		if c == nil && (req.Operation == "open_connection" || req.Operation == "rekey_connection") {
			if key, keyErr := parseConnectionKey(req); keyErr == nil {
				c = &terminalConnection{viewer: in.Sender, id: req.Connection, keyID: req.KeyID,
					key: key, receipts: map[string][]byte{}}
			}
		}
		if c != nil {
			l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
				Status: "refused", Error: string(terminal.CodeUnsupported)})
		}
		return
	}
	name, _ := l.PinnedTerminalViewer(in.Sender)
	p := terminals.Principal{Device: in.Sender, Name: name, Cloud: true}
	if req.Operation == "open_connection" || req.Operation == "rekey_connection" {
		l.openTerminalConnection(ctx, svc, p, req)
		return
	}
	c := l.getTerminalConnection(in.Sender, req.Connection)
	if c == nil {
		return // No key exists to encrypt a trustworthy receipt to this tab.
	}
	if err := svc.Allow(p); err != nil {
		l.closeTerminalConnection(c)
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeForbidden)})
		return
	}
	if previous := l.terminalReceipt(c, req.RequestID); previous != nil {
		var original terminalReceipt
		if json.Unmarshal(previous, &original) != nil || original.Operation != req.Operation || original.TerminalID != req.TerminalID {
			bad, _ := json.Marshal(terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
				Status: "refused", Error: string(terminal.CodeInvalid)})
			_ = l.publishTerminal(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
				Class: string(domaincloud.ClassCtl), Payload: bad, Key: c.key, KeyID: c.keyID})
			return
		}
		_ = l.publishTerminal(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
			Class: string(domaincloud.ClassCtl), Payload: previous, Key: c.key, KeyID: c.keyID})
		return
	}
	if !l.terminalReceiptAvailable(c) {
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
			Status: "refused", Error: string(terminal.CodeBusy)})
		return
	}
	result, err := l.terminalOperation(ctx, svc, p, c, req)
	receipt := terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID,
		Status: "ok", Result: result}
	if err != nil {
		code, known := terminal.CodeOf(err)
		if !known {
			code = terminal.CodeUnreachable
		}
		receipt.Status, receipt.Error, receipt.Result = "refused", string(code), nil
		if code == terminal.CodeInputStateUnknown {
			receipt.Status = "unknown"
		}
		var refusal *terminals.Refusal
		if errors.As(err, &refusal) && refusal.Applied > 0 {
			receipt.Result = map[string]any{"applied_through": refusal.Applied}
		}
	}
	publicationErr := l.sendTerminalReceipt(ctx, c, receipt)
	if (req.Operation == "open" || req.Operation == "read" || req.Operation == "capture") && receipt.Status == "ok" {
		if publicationErr != nil {
			l.closeTerminalConnection(c)
		} else {
			l.terminalMu.Lock()
			if c.watchReady != nil {
				close(c.watchReady)
				c.watchReady = nil
			}
			l.terminalMu.Unlock()
		}
	}
	if (req.Operation == "open" || req.Operation == "read" || req.Operation == "capture") && receipt.Status != "ok" {
		l.terminalMu.Lock()
		if c.watchReady != nil && c.watchCancel != nil {
			c.watchCancel()
			c.watchReady = nil
		}
		l.terminalMu.Unlock()
	}
	if publicationErr == nil && req.Operation == "activate_connection" && receipt.Status == "ok" {
		l.terminalMu.Lock()
		c.rekeyPending = false
		l.terminalMu.Unlock()
		var body struct {
			OldConnection string `json:"old_connection"`
			FirstFrameSeq uint64 `json:"first_frame_seq"`
		}
		if strictTerminalBody(req.Body, &body) {
			if old := l.getTerminalConnection(p.Device, body.OldConnection); old != nil {
				l.closeTerminalConnection(old)
			}
		}
	}
}

func (l *Link) openTerminalConnection(ctx context.Context, svc *terminals.Service, p terminals.Principal, req terminalRequest) {
	key, err := parseConnectionKey(req)
	if err != nil {
		return
	}
	c := &terminalConnection{viewer: p.Device, id: req.Connection, keyID: req.KeyID, key: key,
		expires: l.opts.Now().Add(CloudTerminalKeySecondsLimit * time.Second), receipts: map[string][]byte{},
		rekeyPending: req.Operation == "rekey_connection"}
	if err := svc.Allow(p); err != nil {
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: string(terminal.CodeForbidden)})
		return
	}
	l.terminalMu.Lock()
	if l.terminalConnections == nil {
		l.terminalConnections = map[string]*terminalConnection{}
	}
	id := terminalConnectionID(p.Device, req.Connection)
	if old := l.terminalConnections[id]; old != nil {
		l.terminalMu.Unlock()
		if old.keyID == c.keyID && bytes.Equal(old.key.Bytes(), c.key.Bytes()) {
			l.sendTerminalReceipt(ctx, old, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
				Connection: req.Connection, Operation: req.Operation, Status: "ok", Result: l.connectionResult(old)})
		}
		return
	}
	viewerConnections := 0
	for _, existing := range l.terminalConnections {
		if existing.viewer == p.Device {
			viewerConnections++
		}
	}
	if len(l.terminalConnections) >= CloudTerminalConnectionsLimit || viewerConnections >= CloudTerminalViewerConnectionsLimit {
		l.terminalMu.Unlock()
		l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
			Connection: req.Connection, Operation: req.Operation, Status: "refused", Error: string(terminal.CodeBusy)})
		return
	}
	l.terminalConnections[id] = c
	l.terminalMu.Unlock()
	if l.transport == nil || l.transport.TerminalConnection(ctx, "register", c.viewer, c.id, c.expires) != nil {
		l.closeTerminalConnection(c)
		return
	}
	l.sendTerminalReceipt(ctx, c, terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID,
		Connection: req.Connection, Operation: req.Operation, Status: "ok", Result: l.connectionResult(c)})
}

func (l *Link) connectionResult(c *terminalConnection) any {
	return map[string]any{"connection": c.id, "key_id": c.keyID,
		"expires_at": float64(c.expires.UnixMilli()) / 1000, "machine_incarnation": l.machineIncarnation}
}

func (l *Link) getTerminalConnection(viewer, connection string) *terminalConnection {
	l.terminalMu.Lock()
	c := l.terminalConnections[terminalConnectionID(viewer, connection)]
	if c != nil && !c.expires.After(l.opts.Now()) {
		if c.watchCancel != nil {
			c.watchCancel()
		}
		delete(l.terminalConnections, terminalConnectionID(viewer, connection))
		l.terminalMu.Unlock()
		l.retireTerminalConnection(c)
		return nil
	}
	l.terminalMu.Unlock()
	return c
}

func (l *Link) closeTerminalConnection(c *terminalConnection) {
	l.terminalMu.Lock()
	removed := false
	if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c {
		delete(l.terminalConnections, terminalConnectionID(c.viewer, c.id))
		removed = true
		if c.watchCancel != nil {
			c.watchCancel()
		}
	}
	l.terminalMu.Unlock()
	if removed {
		l.retireTerminalConnection(c)
	}
}

func (l *Link) retireTerminalConnection(c *terminalConnection) {
	if l.transport == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := l.transport.TerminalConnection(ctx, "retire", c.viewer, c.id, c.expires); err != nil {
			l.logf("cloud terminal: relay retirement failed viewer=%s connection=%s: %v", c.viewer, c.id, err)
		}
	}()
}

func (l *Link) terminalReceipt(c *terminalConnection, requestID string) []byte {
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	return append([]byte(nil), c.receipts[requestID]...)
}

func (l *Link) terminalReceiptAvailable(c *terminalConnection) bool {
	l.terminalMu.Lock()
	defer l.terminalMu.Unlock()
	return len(c.receiptOrder) < CloudTerminalReceiptsLimit
}

func (l *Link) sendTerminalReceipt(ctx context.Context, c *terminalConnection, receipt terminalReceipt) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if err = l.publishTerminal(ctx, Outbound{Channel: terminalReceiptChannel(l.identity.MachineID, c),
		Class: string(domaincloud.ClassCtl), Payload: data, Key: c.key, KeyID: c.keyID}); err != nil {
		l.logf("cloud terminal: %s receipt was not delivered: %v", receipt.Operation, err)
	}
	l.terminalMu.Lock()
	if len(c.receiptOrder) < CloudTerminalReceiptsLimit {
		if _, present := c.receipts[receipt.RequestID]; !present {
			c.receiptOrder = append(c.receiptOrder, receipt.RequestID)
		}
		c.receipts[receipt.RequestID] = data
	}
	l.terminalMu.Unlock()
	return err
}

func terminalReceiptChannel(machine string, c *terminalConnection) string {
	return "termr/" + machine + "/" + c.viewer + "/" + c.id
}

func (l *Link) publishTerminal(ctx context.Context, out Outbound) error {
	if l.relay == nil {
		return ErrRelayNotReady
	}
	return l.relay.Publish(ctx, out)
}

func (l *Link) terminalOperation(ctx context.Context, svc *terminals.Service, p terminals.Principal,
	c *terminalConnection, req terminalRequest) (any, error) {
	id := terminal.ID(req.TerminalID)
	if req.Operation != "list" && req.Operation != "open" && req.Operation != "renew_connection" && req.Operation != "activate_connection" &&
		(!id.Valid() || req.TerminalID == "") {
		return nil, terminal.Refuse(terminal.CodeInvalid, "terminal_id is invalid")
	}
	switch req.Operation {
	case "activate_connection":
		var body struct {
			OldConnection string `json:"old_connection"`
			FirstFrameSeq uint64 `json:"first_frame_seq"`
		}
		if !strictTerminalBody(req.Body, &body) || !validConnection(body.OldConnection) || body.OldConnection == c.id || body.FirstFrameSeq == 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "activation body is invalid")
		}
		old := l.getTerminalConnection(p.Device, body.OldConnection)
		if old == nil {
			return nil, terminal.Refuse(terminal.CodeInvalid, "old connection is unavailable")
		}
		l.terminalMu.Lock()
		valid := c.rekeyPending && c.terminalID.Valid() && old.terminalID == c.terminalID &&
			c.client != "" && old.client == c.client && body.FirstFrameSeq <= c.publishedFrameSeq
		l.terminalMu.Unlock()
		control := svc.Control(c.terminalID)
		valid = valid && control.Held && !control.Unknown && control.Holder.Device == p.Device &&
			control.Client == c.client && control.Expires.After(l.opts.Now())
		if !valid {
			return nil, terminal.Refuse(terminal.CodeInvalid, "new frame and lease identity are unverified")
		}
		return map[string]any{"connection": c.id, "retired_connection": old.id}, nil
	case "list":
		rows, err := svc.List(ctx, p, req.ProjectID)
		if err != nil {
			return nil, err
		}
		out := make([]contract.Terminal, 0, len(rows))
		for _, row := range rows {
			out = append(out, cloudWireTerminal(row, p, req.Client))
		}
		return map[string]any{"terminals": out}, nil
	case "open":
		var body struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if !strictTerminalBody(req.Body, &body) || body.Cols <= 0 || body.Rows <= 0 || l.opts.TerminalProject == nil {
			return nil, terminal.Refuse(terminal.CodeInvalid, "open needs project_id, cols and rows")
		}
		dir, ok := l.opts.TerminalProject(ctx, req.ProjectID)
		if !ok {
			return nil, terminal.Refuse(terminal.CodeInvalid, "project_id is unknown")
		}
		t, err := svc.Open(ctx, p, ports.OpenTerminal{ProjectPath: dir, ProjectID: req.ProjectID, Cols: body.Cols, Rows: body.Rows})
		if err != nil {
			return nil, err
		}
		row, err := svc.Find(ctx, p, t.ID)
		if err != nil {
			return nil, err
		}
		if err := l.watchTerminal(ctx, svc, p, c, t.ID, req.Client); err != nil {
			return nil, err
		}
		return cloudWireTerminal(row, p, req.Client), nil
	case "read", "capture":
		row, err := svc.Find(ctx, p, id)
		if err != nil {
			return nil, err
		}
		if err := l.watchTerminal(ctx, svc, p, c, id, req.Client); err != nil {
			return nil, err
		}
		if req.Operation == "capture" {
			return map[string]any{"terminal_id": req.TerminalID}, nil
		}
		return cloudWireTerminal(row, p, req.Client), nil
	case "history":
		var body struct {
			Lines int `json:"lines"`
		}
		if len(req.Body) > 0 && !strictTerminalBody(req.Body, &body) {
			return nil, terminal.Refuse(terminal.CodeInvalid, "history body is invalid")
		}
		lines, err := svc.History(ctx, p, id, body.Lines)
		if err != nil {
			return nil, err
		}
		if lines == nil {
			lines = []string{}
		}
		return map[string]any{"lines": lines}, nil
	case "control":
		if req.Client == "" {
			return nil, terminal.Refuse(terminal.CodeInvalid, "control needs client")
		}
		var body struct {
			Action terminals.Action `json:"action"`
		}
		if len(req.Body) > 0 && !strictTerminalBody(req.Body, &body) {
			return nil, terminal.Refuse(terminal.CodeInvalid, "control body is invalid")
		}
		var control terminals.Control
		if body.Action == "" {
			if _, err := svc.Find(ctx, p, id); err != nil {
				return nil, err
			}
			control = svc.Control(id)
		} else {
			var err error
			control, err = svc.SetControl(ctx, p, id, req.Client, body.Action)
			if err != nil {
				return nil, err
			}
		}
		unknown := control.Held && control.Holder.Device == p.Device && control.Client == req.Client && control.Unknown
		return map[string]any{"machine_incarnation": l.machineIncarnation,
			"control": cloudWireControl(control, p, req.Client), "input_state_unknown": unknown}, nil
	case "input", "paste":
		if req.Client == "" || req.Epoch == 0 || req.Seq == 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "input needs client, epoch and seq")
		}
		var body struct {
			Data string `json:"data"`
			Text string `json:"text"`
		}
		if !strictTerminalBody(req.Body, &body) {
			return nil, terminal.Refuse(terminal.CodeInvalid, "input body is invalid")
		}
		var applied uint64
		var dup bool
		var err error
		if req.Operation == "input" {
			data, decodeErr := base64.StdEncoding.DecodeString(body.Data)
			if decodeErr != nil || len(data) == 0 {
				return nil, terminal.Refuse(terminal.CodeInvalid, "input data is invalid")
			}
			applied, dup, err = svc.Input(ctx, p, id, req.Client, req.Epoch, req.Seq, data)
		} else {
			if body.Text == "" {
				return nil, terminal.Refuse(terminal.CodeInvalid, "paste text is empty")
			}
			applied, dup, err = svc.Paste(ctx, p, id, req.Client, req.Epoch, req.Seq, body.Text)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"applied_through": applied, "duplicate": dup}, nil
	case "resize":
		var body struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if !strictTerminalBody(req.Body, &body) || req.Client == "" || req.Epoch == 0 || body.Cols <= 0 || body.Rows <= 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "resize body is invalid")
		}
		if err := svc.Resize(ctx, p, id, req.Client, req.Epoch, body.Cols, body.Rows); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	case "close":
		if req.Client == "" || req.Epoch == 0 {
			return nil, terminal.Refuse(terminal.CodeInvalid, "close needs client and epoch")
		}
		if err := svc.Close(ctx, p, id, req.Client, req.Epoch); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	case "renew_connection":
		// The connection key has an absolute ten-minute lifetime. Renewal is
		// an authenticated liveness query, not a way to extend that lifetime.
		return l.connectionResult(c), nil
	default:
		return nil, terminal.Refuse(terminal.CodeInvalid, "terminal operation is unknown")
	}
}

func strictTerminalBody(raw json.RawMessage, out any) bool {
	if len(raw) == 0 {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return false
	}
	return dec.Decode(new(any)) == io.EOF
}

func cloudWireControl(c terminals.Control, viewer terminals.Principal, client string) contract.TerminalControl {
	out := contract.TerminalControl{Held: c.Held, Epoch: int64(c.Epoch)}
	if !c.Held {
		return out
	}
	same := c.Holder.Device == viewer.Device
	name := c.Holder.Name
	if name == "" {
		name = "?"
	}
	out.Holder = &contract.TerminalHolder{Name: name, Local: c.Holder.Local,
		SameDevice: same, SameClient: same && c.Client == client}
	out.ExpiresAt = float64(c.Expires.UnixMilli()) / 1000
	if out.Holder.SameClient {
		out.AppliedThrough = int64(c.Applied)
	}
	return out
}

func cloudWireTerminal(row terminals.Listed, viewer terminals.Principal, client string) contract.Terminal {
	return contract.Terminal{ID: string(row.ID), ProjectID: row.ProjectID, Created: row.Created.Unix(),
		Status: contract.TerminalStatus(row.Status), Cols: int64(row.Cols), Rows: int64(row.Rows),
		Dir: row.Dir, Control: cloudWireControl(row.Control, viewer, client)}
}

func cloudWireFrame(frame terminal.Frame) contract.TerminalFrame {
	lines := frame.Lines
	if lines == nil {
		lines = []string{}
	}
	mouse := contract.TerminalMouseMode(frame.Modes.Mouse)
	if mouse == "" {
		mouse = contract.TerminalMouseMode(terminal.MouseNone)
	}
	return contract.TerminalFrame{Rev: frame.Rev, At: float64(frame.At.UnixMilli()) / 1000,
		Cols: int64(frame.Cols), Rows: int64(frame.Rows), Lines: lines, Dead: frame.Dead,
		Cursor: contract.TerminalCursor{X: int64(frame.Cursor.X), Y: int64(frame.Cursor.Y),
			Visible: frame.Cursor.Visible, Shape: contract.TerminalCursorShape(frame.Cursor.Shape), Blinking: frame.Cursor.Blinking},
		Modes: contract.TerminalModes{AppCursor: frame.Modes.AppCursor, AppKeypad: frame.Modes.AppKeypad,
			Mouse: mouse, MouseSgr: frame.Modes.MouseSGR, Alt: frame.Modes.Alt}}
}

func (l *Link) watchTerminal(ctx context.Context, svc *terminals.Service, p terminals.Principal,
	c *terminalConnection, id terminal.ID, client string) error {
	watch, err := svc.Watch(ctx, p, id, client)
	if err != nil {
		return err
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	l.terminalMu.Lock()
	if c.watchCancel != nil {
		c.watchCancel()
	}
	ready := make(chan struct{})
	c.watchCancel, c.watchReady, c.terminalID, c.client, c.frameSeq, c.publishedFrameSeq = cancel, ready, id, client, 0, 0
	l.terminalMu.Unlock()
	go func() {
		defer watch.Stop()
		select {
		case <-ready:
		case <-watchCtx.Done():
			return
		}
		_ = watch.Run(watchCtx, func(e terminals.Event) error {
			if e.Kind != terminals.EventFrame {
				return nil
			}
			if err := svc.Allow(p); err != nil {
				l.closeTerminalConnection(c)
				return err
			}
			l.terminalMu.Lock()
			if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] != c || c.terminalID != id ||
				!c.expires.After(l.opts.Now()) {
				l.terminalMu.Unlock()
				return context.Canceled
			}
			c.frameSeq++
			seq := c.frameSeq
			l.terminalMu.Unlock()
			payload, err := json.Marshal(map[string]any{"v": 1, "type": "terminal_frame",
				"terminal_id": string(id), "connection": c.id, "frame_seq": seq,
				"captured_at": float64(e.Frame.At.UnixMilli()) / 1000, "frame": cloudWireFrame(e.Frame)})
			if err != nil {
				return err
			}
			if err := svc.Allow(p); err != nil {
				l.closeTerminalConnection(c)
				return err
			}
			if err := l.publishTerminal(watchCtx, Outbound{Channel: "term/" + l.identity.MachineID + "/" + c.viewer + "/" + c.id,
				Class: string(domaincloud.ClassStream), Payload: payload, Key: c.key, KeyID: c.keyID}); err != nil {
				return err
			}
			l.terminalMu.Lock()
			if l.terminalConnections[terminalConnectionID(c.viewer, c.id)] == c && c.frameSeq >= seq {
				c.publishedFrameSeq = seq
			}
			l.terminalMu.Unlock()
			return nil
		})
	}()
	return nil
}
