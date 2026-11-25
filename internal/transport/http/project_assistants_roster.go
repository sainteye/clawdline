package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// This route is deliberately local. The ordinary Cloud recorder cannot flush
// and is not a recipient-isolated subscription carrier.
func (h *projectAssistantHTTP) rosterEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paWriteHTTPFailure(w, paHTTPFailure(pa.Invalid, "roster_method"))
		return
	}
	if cloudtransport.ViaCloud(r.Context()) {
		paWriteHTTPFailure(w, paHTTPFailure(pa.Unsupported, "roster_cloud_pending"))
		return
	}
	if _, err := h.status(r.Context(), true); err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if cursor != "" && cursor != last {
			paWriteHTTPFailure(w, paHTTPFailure(pa.CursorExpired, "roster_cursor_conflict"))
			return
		}
		cursor = last
	}
	machine := r.URL.Query().Get("machine_id")
	if machine != h.options.MachineID {
		paWriteHTTPFailure(w, paHTTPFailure(pa.ScopeDenied, "roster_machine"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(pa.MaxRosterLifetimeSeconds)*time.Second)
	defer cancel()
	sub, err := h.options.Service.SubscribeRoster(ctx, pa.RosterRequest{MachineID: machine, ProjectID: r.URL.Query().Get("project_id"), Cursor: cursor})
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	defer sub.Close()
	rc := http.NewResponseController(w)
	deadline := func() error {
		return rc.SetWriteDeadline(time.Now().Add(time.Duration(capacity.MaximumBotWriteMilliseconds) * time.Millisecond))
	}
	if err = deadline(); err != nil {
		paWriteHTTPFailure(w, paHTTPFailure(pa.Unsupported, "roster_write_deadline"))
		return
	}
	// Read the initial bounded page before committing the response headers, so
	// an expired/tampered/restarted cursor receives the ordinary typed refusal.
	if h.options.BeforeRosterSnapshot != nil {
		h.options.BeforeRosterSnapshot()
	}
	initial, err := sub.Read(ctx, cursor)
	if err != nil {
		paWriteHTTPFailure(w, err)
		return
	}
	var check atomic.Value
	check.Store(initial.RevalidateAuthority)
	stopped := make(chan struct{})
	done := make(chan struct{})
	defer func() { close(stopped); <-done }()
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Duration(pa.MaxRosterRecheckMilliseconds) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				_ = rc.SetWriteDeadline(time.Now())
				return
			case <-ticker.C:
				if check.Load().(func(context.Context) error)(ctx) != nil {
					cancel()
					_ = rc.SetWriteDeadline(time.Now())
					return
				}
			}
		}
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Add("Trailer", "X-Clawdline-Roster-Error")
	// When a blocked peer cannot receive a final SSE frame, a completed HTTP
	// response still carries a typed closure trailer. A broken connection may
	// lose even that trailer; EOF never means the last cursor was applied.
	defer func() { w.Header().Set("X-Clawdline-Roster-Error", string(pa.CursorExpired)) }()
	write := func(frame pa.RosterFrame, fence, authority func(context.Context) error) error {
		wire, err := paWireRosterFrame(frame)
		if err != nil {
			return err
		}
		body, err := json.Marshal(wire)
		if err != nil {
			return err
		}
		if len(body) > cloudops.ReadPlaintextBytesLimit {
			return paHTTPFailure(pa.CapacityFull, "roster_frame_bytes")
		}
		if h.options.BeforeViewPublish != nil {
			h.options.BeforeViewPublish()
		}
		if _, err = h.status(ctx, true); err != nil {
			return err
		}
		if err = fence(ctx); err != nil {
			return err
		}
		check.Store(fence)
		defer check.Store(authority)
		if err = deadline(); err != nil {
			return err
		}
		// SSE IDs belong only to applied data frames, never hello/control.
		if frame.Cursor != "" {
			if _, err = fmt.Fprintf(w, "id: %s\n", frame.Cursor); err != nil {
				return err
			}
		}
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame.Type, body); err != nil {
			return err
		}
		return rc.Flush()
	}
	control := func(err error) {
		// Content-free closure carries no cursor, source identity or rich rows.
		// A blocked/disconnected peer may receive EOF instead; it must reconnect
		// with its last APPLIED cursor, never infer success from hello or EOF.
		if ctx.Err() != nil {
			return
		}
		kind := pa.RosterClosure
		if pa.Code(err) == pa.CursorExpired || pa.Code(err) == pa.CapacityFull {
			kind = pa.RosterResync
		}
		_ = write(pa.RosterFrame{Type: kind, Failure: &pa.Failure{Code: pa.Code(err), Field: "roster_stream"}}, sub.Revalidate, sub.Revalidate)
	}
	if err = write(pa.RosterFrame{Type: "hello", SourceEpoch: initial.Page.Frame.SourceEpoch}, sub.Revalidate, initial.RevalidateAuthority); err != nil {
		return
	}
	page := initial
	for {
		if err = write(page.Page.Frame, page.Revalidate, page.RevalidateAuthority); err != nil {
			control(err)
			return
		}
		cursor = page.Page.Frame.Cursor
		if !page.Page.More {
			// No timer/list refresh. Only a successful post-commit invalidation
			// schedules a new durable read; the ticker checks authority only.
			select {
			case <-ctx.Done():
				return
			case <-sub.Wakeups:
			}
		}
		page, err = sub.Read(ctx, cursor)
		if err != nil {
			control(err)
			return
		}
	}
}

func paWireRosterFrame(frame pa.RosterFrame) (contract.ProjectAssistantRosterFrame, error) {
	out := contract.ProjectAssistantRosterFrame{Type: contract.ProjectAssistantRosterFrameType(frame.Type), Cursor: frame.Cursor, SourceEpoch: frame.SourceEpoch}
	valid := false
	for _, kind := range contract.ProjectAssistantRosterFrameTypeValues {
		if out.Type == kind {
			valid = true
			break
		}
	}
	if !valid {
		return out, paHTTPFailure(pa.Invalid, "roster_frame_type")
	}
	if frame.Failure != nil {
		failure := paWireFailure(*frame.Failure)
		out.Failure = &failure
	}
	for _, change := range frame.Changes {
		row := contract.ProjectAssistantRosterChange{Key: paWireBotKey(change.Key), Revision: change.Revision, Removed: change.Removed}
		if change.Summary != nil {
			summary, err := paWireBotSummary(*change.Summary)
			if err != nil {
				return out, err
			}
			row.Summary = &summary
		}
		out.Changes = append(out.Changes, row)
	}
	return out, nil
}
