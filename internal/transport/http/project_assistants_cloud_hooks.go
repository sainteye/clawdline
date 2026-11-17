package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// botRoutineHookServer keeps only an acknowledged digest in memory. Cancellation,
// any failed Cloud IO and authority changes remove public readiness immediately.
type botRoutineHookServer struct {
	mu     sync.RWMutex
	worker *botRoutineHookWorker
}
type botRoutineHookWorker struct {
	server      *Server
	identity    bh.Identity
	cloud       bh.Cloud
	store       *projectassistantstore.Store
	ctx         context.Context
	mu          sync.RWMutex
	fingerprint string
	revision    int64
}

// StartBotRoutineHooks has a separate sequential lane and no provider calls.
// Its owner must await done before starting a replacement Cloud generation.
func (s *Server) StartBotRoutineHooks(ctx context.Context, id bh.Identity, cloud bh.Cloud) <-chan struct{} {
	done := make(chan struct{})
	worker := &botRoutineHookWorker{server: s, identity: id, cloud: cloud, ctx: ctx}
	if s.store != nil {
		worker.store = projectassistantstore.New(s.store.ProjectAssistantDatabase())
	}
	s.projectAssistants.cloudHooks.mu.Lock()
	s.projectAssistants.cloudHooks.worker = worker
	s.projectAssistants.cloudHooks.mu.Unlock()
	go func() {
		defer close(done)
		defer worker.clear()
		backoff := time.Duration(bh.MaxPollSeconds) * time.Second
		for ctx.Err() == nil {
			cycleCtx, cancel := context.WithTimeout(ctx, time.Duration(bh.MaxHTTPSeconds)*time.Second)
			err := worker.cycle(cycleCtx)
			cancel()
			if err != nil {
				worker.clear()
				backoff = min(backoff*2, time.Duration(bh.MaxBackoffSeconds)*time.Second)
			} else {
				backoff = time.Duration(bh.MaxPollSeconds) * time.Second
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return done
}
func (w *botRoutineHookWorker) clear() {
	w.mu.Lock()
	w.fingerprint = ""
	w.revision = 0
	w.mu.Unlock()
}
func (w *botRoutineHookWorker) current() error {
	if w.ctx.Err() != nil {
		return bh.Fail("webhook_unavailable")
	}
	i, err := w.server.projectAssistantIdentity()
	if err != nil || w.identity.AccountID == "" || i.AccountID != w.identity.AccountID || i.MachineID != w.identity.MachineID {
		return bh.Fail("webhook_authority_changed")
	}
	w.server.projectAssistants.mu.RLock()
	stopped := w.server.projectAssistants.stopped
	w.server.projectAssistants.mu.RUnlock()
	if stopped {
		return bh.Fail("webhook_unavailable")
	}
	return nil
}

// snapshot rechecks source/Project on every publication and receipt. Opt-out
// produces an empty set so previously granted Cloud authority can be withdrawn.
func (w *botRoutineHookWorker) snapshot(ctx context.Context) (bh.Enrollment, string, error) {
	if err := w.current(); err != nil {
		return bh.Enrollment{}, "", err
	}
	if w.store == nil {
		return bh.Enrollment{}, "", nil
	}
	installed, err := w.store.CloudHooksInstalled(ctx)
	if err != nil || !installed {
		return bh.Enrollment{}, "", err
	}
	hooks := []bh.Hook{}
	source, err := w.server.projectAssistantSource(ctx, pa.BotKey{MachineID: w.identity.MachineID}, false)
	if err != nil {
		return bh.Enrollment{}, "", err
	}
	if pa.CheckSource(source, true) == nil {
		candidates, e := w.store.CloudHookCandidates(ctx, w.identity.MachineID)
		if e != nil {
			return bh.Enrollment{}, "", e
		}
		for _, c := range candidates {
			project, e := (projectAssistantProjects{w.server}).Resolve(ctx, pa.ProjectSelection{MachineID: c.Routine.Key.MachineID, ProjectID: c.Routine.Project.ProjectID})
			if e != nil || !project.Current || !project.Available || project.Scope != c.Routine.Project {
				continue
			}
			hooks = append(hooks, c.Hook)
		}
	}
	if err = w.current(); err != nil {
		return bh.Enrollment{}, "", err
	}
	return w.store.PrepareCloudHooks(ctx, w.identity, hooks)
}
func (w *botRoutineHookWorker) cycle(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			w.clear()
		}
	}()
	set, fingerprint, err := w.snapshot(ctx)
	if err != nil {
		return err
	}
	if set.Revision == 0 {
		return nil
	} // stable default-off sources never enroll emptiness
	w.mu.RLock()
	ready := w.fingerprint == fingerprint && w.revision == set.Revision
	w.mu.RUnlock()
	if !ready {
		w.clear()
		if err = w.cloud.Enroll(ctx, set); err != nil {
			return err
		}
		live, digest, e := w.snapshot(ctx)
		if e != nil {
			return e
		}
		if digest != fingerprint || live.Revision != set.Revision {
			return bh.Fail("webhook_authority_changed")
		}
		w.mu.Lock()
		w.fingerprint = fingerprint
		w.revision = set.Revision
		w.mu.Unlock()
	}
	if pending, e := w.store.CloudHookPending(ctx, w.identity); e != nil {
		return e
	} else if pending != nil {
		return w.resolve(ctx, *pending)
	}
	if len(set.Hooks) == 0 {
		return nil
	}
	delivery, err := w.cloud.Claim(ctx)
	if err != nil || delivery == nil {
		return err
	}
	// Networking may have raced a key, Project, feature or account change.
	live, digest, err := w.snapshot(ctx)
	if err != nil {
		return err
	}
	if digest != fingerprint || live.Revision != set.Revision {
		return bh.Fail("webhook_authority_changed")
	}
	known := false
	for _, h := range live.Hooks {
		if h.ID == delivery.HookID && h.Enabled {
			known = true
			break
		}
	}
	if !known || delivery.Revision > live.Revision {
		return bh.Fail("webhook_authority_changed")
	}
	body, headers, err := delivery.Decode()
	if err != nil {
		return err
	}
	if err = w.store.BeginCloudHook(ctx, w.identity, delivery.ID); err != nil {
		return err
	}
	if err = w.current(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://source.invalid/hooks/routines/"+delivery.HookID, bytes.NewReader(body))
	if err != nil {
		return bh.Fail("webhook_invalid")
	}
	request.Header = headers
	response := &botHookResponse{header: http.Header{}}
	// Invoke the production receiver directly, without a local/device bearer.
	// It rechecks the current key, source, Project, memory and durable receipt.
	w.server.routineHooksRoute(response, request)
	ack := bh.Ack{Protocol: bh.Protocol, LeaseToken: delivery.LeaseToken, Status: response.status, Code: "webhook_outcome_unknown"}
	if ack.Status == 0 {
		ack.Status = http.StatusOK
	}
	if !response.overflow {
		var result struct {
			Code  string `json:"code"`
			RunID string `json:"run_id"`
		}
		if json.Unmarshal(response.body.Bytes(), &result) == nil {
			if ack.Status >= 200 && ack.Status < 300 {
				ack.Code = "accepted"
				ack.RunID = result.RunID
			} else if result.Code != "" {
				ack.Code = result.Code
			}
		}
	}
	if err = w.store.FinishCloudHook(ctx, w.identity, delivery.ID, ack); err != nil {
		return err
	}
	return w.resolve(ctx, projectassistantstore.CloudHookPending{ID: delivery.ID, Ack: &ack})
}
func (w *botRoutineHookWorker) resolve(ctx context.Context, p projectassistantstore.CloudHookPending) error {
	if err := w.current(); err != nil {
		return err
	}
	// No actual local response survived: inspect only, never invent an ack.
	if p.Ack == nil {
		metadata, err := w.cloud.Inspect(ctx, p.ID)
		if err != nil {
			return err
		}
		if !botHookTerminal(metadata.State) {
			return bh.Fail("webhook_outcome_unknown")
		}
		return w.store.ClearCloudHook(ctx, w.identity, p.ID)
	}
	_, err := w.cloud.Acknowledge(ctx, p.ID, *p.Ack)
	if err == nil {
		return w.store.ClearCloudHook(ctx, w.identity, p.ID)
	}
	// Preserve the exact ack across network ambiguity. A metadata read can prove
	// completion; a queued/leased response never proves this lease's ack landed.
	metadata, inspectErr := w.cloud.Inspect(ctx, p.ID)
	if inspectErr == nil && botHookTerminal(metadata.State) {
		return w.store.ClearCloudHook(ctx, w.identity, p.ID)
	}
	return err
}
func botHookTerminal(state string) bool {
	switch state {
	case "host_accepted", "refused", "outcome_unknown", "invalidated", "expired":
		return true
	}
	return false
}

// botHookResponse bounds the source response without using a network socket.
type botHookResponse struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func (r *botHookResponse) Header() http.Header { return r.header }
func (r *botHookResponse) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *botHookResponse) Write(raw []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.body.Len()+len(raw) > bh.MaxLocalResponseBytes {
		r.overflow = true
		return len(raw), nil
	}
	return r.body.Write(raw)
}

var _ io.Writer = (*botHookResponse)(nil)

func (s *Server) botRoutineHookPublicURL(ctx context.Context, routineID, key string) string {
	s.projectAssistants.cloudHooks.mu.RLock()
	w := s.projectAssistants.cloudHooks.worker
	s.projectAssistants.cloudHooks.mu.RUnlock()
	if w == nil || w.ctx.Err() != nil {
		return ""
	}
	set, digest, err := w.snapshot(ctx)
	if err != nil {
		return ""
	}
	w.mu.RLock()
	ready := digest != "" && w.fingerprint == digest && w.revision == set.Revision
	w.mu.RUnlock()
	if !ready {
		return ""
	}
	for _, h := range set.Hooks {
		if h.ID == routineID && h.Key == key && h.Enabled {
			return w.cloud.PublicURL(w.identity.MachineID, routineID)
		}
	}
	return ""
}
