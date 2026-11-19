package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type hostDevServers struct{ h *Host }

func (d hostDevServers) rows(ctx context.Context, key pa.BotKey, project pa.ProjectScope) ([]servers.SavedServer, error) {
	d.h.mu.Lock()
	r := d.h.serverRegistry
	d.h.mu.Unlock()
	if r != nil {
		return r.Snapshot(key, project), nil
	}
	c, err := serverstore.NewCatalog(ctx, d.h.o.Database)
	if err != nil {
		return nil, err
	}
	all, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil {
		return nil, err
	}
	rows := []servers.SavedServer{}
	for _, saved := range all {
		if saved.Spec.Key == key && saved.Spec.Project == project {
			switch saved.Attempt.State {
			case servers.Accepted, servers.Running, servers.Stopping:
				saved.Attempt.State = servers.Unknown
				saved.Attempt.Failure = "owner_lost"
			}
			rows = append(rows, saved)
		}
	}
	return rows, nil
}

func (d hostDevServers) Read(ctx context.Context, key pa.BotKey, project pa.ProjectScope, id string) (app.DevServerResult, error) {
	rows, err := d.rows(ctx, key, project)
	if err != nil {
		return app.DevServerResult{}, err
	}
	d.h.mu.Lock()
	r := d.h.serverRegistry
	d.h.mu.Unlock()
	out := app.DevServerResult{Servers: []app.DevServerInfo{}}
	for _, saved := range rows {
		if id == "" {
			out.Servers = append(out.Servers, d.h.observedServerInfo(ctx, r, saved))
			continue
		}
		if saved.ID != id {
			continue
		}
		info := d.h.observedServerInfo(ctx, r, saved)
		out.Server = &info
		if r == nil {
			out.Log = &app.DevServerLog{Truncated: true}
			return boundedServerResult(out)
		}
		if saved.Attempt.Identity.AttemptID == "" {
			out.Log = &app.DevServerLog{}
			return boundedServerResult(out)
		}
		log, err := r.ReadHumanLog(ctx, key, project, id, saved.Attempt.Identity.AttemptID)
		if err != nil {
			return app.DevServerResult{}, err
		}
		out.Log = &app.DevServerLog{Text: log.Text, Truncated: log.Truncated}
		return boundedServerResult(out)
	}
	if id != "" {
		return app.DevServerResult{}, failure(pa.NotFound, "server")
	}
	return boundedServerResult(out)
}

func (d hostDevServers) Control(ctx context.Context, key pa.BotKey, project pa.ProjectScope, authority app.HumanServerAuthority, q app.DevServerControl) (result app.DevServerResult, err error) {
	stoppedForRestart := false
	defer func() {
		if stoppedForRestart && err != nil {
			// The observed process was already stopped. A later refusal must not
			// suggest that the entire restart had no effect or can be replayed.
			err = failure(pa.OutcomeUnknown, "server_restart")
		}
	}()
	if q.Key != key || authority.Principal == "" || authority.Check == nil {
		return app.DevServerResult{}, failure(pa.ScopeDenied, "server_control")
	}
	d.h.mu.Lock()
	r, enabled := d.h.serverRegistry, d.h.enabled && !d.h.closed
	d.h.mu.Unlock()
	if r == nil || !enabled {
		return app.DevServerResult{}, failure(pa.Disabled, "server_runtime")
	}
	var expected servers.SavedServer
	for _, saved := range r.Snapshot(key, project) {
		if saved.ID == q.ID {
			expected = saved
			break
		}
	}
	if expected.ID == "" {
		return app.DevServerResult{}, failure(pa.NotFound, "server")
	}
	fingerprint, _ := expected.Spec.Fingerprint()
	// A command key is scoped to this principal, known server and action. Start
	// uses a stable attempt identity persisted by the existing attempt ledger.
	payload, _ := json.Marshal([]string{authority.Principal, key.MachineID, key.BotID, q.ID, q.Action, q.IdempotencyKey})
	hash := sha256.Sum256(payload)
	attempt := "human-" + hex.EncodeToString(hash[:])
	replay := (q.Action == "start" || q.Action == "restart") && expected.Attempt.Identity.AttemptID == attempt
	if fingerprint != q.Fingerprint || (!replay && (expected.Attempt.Identity.AttemptID != q.Attempt || expected.Attempt.Version != q.Revision)) {
		return app.DevServerResult{}, failure(pa.StaleVersion, "server_definition")
	}
	if err := authority.Check(ctx); err != nil {
		return app.DevServerResult{}, err
	}
	if err := d.h.serverControl(ctx, expected.Spec); err != nil {
		return app.DevServerResult{}, err
	}
	ctx = context.WithValue(ctx, serverHumanAuthorityKey{}, authority.Check)
	switch q.Action {
	case "start", "restart":
		if err := d.h.serverCheck(ctx, expected.Spec); err != nil {
			return app.DevServerResult{}, err
		}
		if q.Action == "restart" && !replay && expected.Attempt.Version > 0 {
			if err := r.StopExpected(ctx, expected); err != nil {
				return app.DevServerResult{}, err
			}
			stoppedForRestart = true
			beforeStop := expected
			for _, saved := range r.Snapshot(key, project) {
				if saved.ID == q.ID {
					if saved.Spec != beforeStop.Spec || saved.Attempt.Identity != beforeStop.Attempt.Identity {
						return app.DevServerResult{}, failure(pa.StaleVersion, "server_restart")
					}
					expected = saved
					break
				}
			}
		}
		// The authenticated person explicitly starts the exact displayed resource
		// once. This callback cannot be constructed through any native tool.
		review := serverReview(func(_ context.Context, _ pa.PermissionAction, options []pa.PermissionOption) (pa.PermissionOption, error) {
			if err := d.h.serverCheck(ctx, expected.Spec); err != nil {
				return pa.PermissionOption{}, err
			}
			for _, o := range options {
				if o.Kind == pa.AllowOnce {
					return o, nil
				}
			}
			return pa.PermissionOption{}, failure(pa.Invalid, "server_control")
		})
		saved, err := r.StartExpected(context.WithValue(ctx, serverReviewKey{}, review), expected, attempt)
		if err != nil {
			return app.DevServerResult{}, err
		}
		info := d.h.observedServerInfo(ctx, r, saved)
		return boundedServerResult(app.DevServerResult{Servers: []app.DevServerInfo{}, Server: &info})
	case "stop":
		if expected.Attempt.Version > 0 {
			if err := r.StopExpected(ctx, expected); err != nil {
				return app.DevServerResult{}, err
			}
		}
	case "remove":
		if err := r.RemoveExpected(ctx, expected); err != nil {
			return app.DevServerResult{}, err
		}
	default:
		return app.DevServerResult{}, failure(pa.Invalid, "server_control")
	}
	return d.Read(ctx, key, project, "")
}
