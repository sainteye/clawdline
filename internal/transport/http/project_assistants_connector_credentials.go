package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/botcapabilities"
	"github.com/sainteye/clawdline/internal/adapters/botconnectorvault"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Source composition, never a request, owns the registry identity. A fresh
// disabled source creates no schema or credential directory.
func attachConnectorCredentials(i botcapabilities.Inventory, db *sql.DB, account, machine string) botcapabilities.Inventory {
	var mu sync.Mutex
	var registry *botconnectorvault.Registry
	get := func(ctx context.Context) (*botconnectorvault.Registry, error) {
		mu.Lock()
		defer mu.Unlock()
		if registry != nil {
			return registry, nil
		}
		r, err := botconnectorvault.New(ctx, db, i.State, account, machine)
		if err == nil {
			registry = r
		}
		return r, err
	}
	i.Credential = func(ctx context.Context, b pa.Bot, ref pa.ExtensionRef) ([]byte, error) {
		r, err := get(ctx)
		if err != nil {
			return nil, err
		}
		value, _, err := r.Get(ctx, botconnectorvault.Scope{Bot: b, Ref: ref})
		return value, err
	}
	i.CaptureCredentialWithCleanup = func(b pa.Bot, ref pa.ExtensionRef) (func(context.Context) ([]byte, error), func(context.Context) error, func(context.Context) error) {
		var lock sync.Mutex
		var revision int64
		var release, quarantine func(context.Context) error
		var released bool
		credential := func(ctx context.Context) ([]byte, error) {
			lock.Lock()
			defer lock.Unlock()
			if released {
				return nil, paHTTPFailure(pa.Revoked, "connector_credential")
			}
			r, err := get(ctx)
			if err != nil {
				return nil, err
			}
			scope := botconnectorvault.Scope{Bot: b, Ref: ref}
			if revision == 0 {
				value, now, done, unknown, err := r.Acquire(ctx, scope)
				if err != nil {
					return nil, err
				}
				revision, release, quarantine = now, done, unknown
				return value, nil
			}
			if err = r.Check(ctx, scope, revision); err != nil {
				return nil, err
			}
			value, now, err := r.Get(ctx, scope)
			if err != nil {
				return nil, err
			}
			if now != revision {
				for i := range value {
					value[i] = 0
				}
				return nil, paHTTPFailure(pa.Revoked, "connector_credential")
			}
			return value, nil
		}
		done := func(ctx context.Context) error {
			lock.Lock()
			defer lock.Unlock()
			if released {
				return nil
			}
			if release != nil {
				if err := release(ctx); err != nil {
					return err
				}
			}
			released = true
			return nil
		}
		unknown := func(ctx context.Context) error {
			lock.Lock()
			defer lock.Unlock()
			if quarantine != nil {
				return quarantine(ctx)
			}
			return nil
		}
		return credential, done, unknown
	}
	i.CredentialAction = func(ctx context.Context, b pa.Bot, q pa.ConnectorCredentialAction, value []byte, secret bool, admit func(context.Context) error) (pa.ConnectorCredential, error) {
		r, err := get(ctx)
		if err != nil {
			return pa.ConnectorCredential{}, err
		}
		scope := botconnectorvault.Scope{Bot: b, Ref: q.Ref, Authority: q.Authority}
		if q.Action == "status" {
			if err = admit(ctx); err != nil {
				return pa.ConnectorCredential{}, err
			}
			return r.Status(ctx, scope)
		}
		return r.Apply(ctx, scope, q, value, secret, admit)
	}
	i.SuspendCredentials = func(ctx context.Context) error {
		mu.Lock()
		r := registry
		mu.Unlock()
		if r != nil {
			return r.Suspend(ctx)
		}
		return nil
	}
	i.CredentialTargetUsage = func() (int, bool) {
		mu.Lock()
		r := registry
		mu.Unlock()
		if r == nil {
			return 0, true
		}
		return r.SecretTargetUsage(), true
	}
	return i
}

func (h *projectAssistantHTTP) connectorCredential(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	w.Header().Set("Cache-Control", "no-store")
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	var q contract.ProjectAssistantConnectorCredentialAction
	var value []byte
	var err error
	secret := route.action == "connector-secret"
	if secret {
		var request contract.ProjectAssistantConnectorSecretSubmission
		request, err = paDecodeHTTP[contract.ProjectAssistantConnectorSecretSubmission](w, r)
		q = contract.ProjectAssistantConnectorCredentialAction{Command: request.Command, Fence: request.Fence, ExpectedVersion: request.ExpectedVersion, Ref: request.Ref, Action: request.Action, Target: request.Target}
		value = []byte(request.Value)
		defer func() {
			for i := range value {
				value[i] = 0
			}
		}()
	} else {
		q, err = paDecodeHTTP[contract.ProjectAssistantConnectorCredentialAction](w, r)
	}
	if err != nil {
		return nil, err
	}
	command, err := h.command(r, route, q.Command, pa.ConnectorCredentialOperation)
	if err != nil {
		return nil, err
	}
	out, check, err := h.options.Service.ConnectorCredential(r.Context(), pa.ConnectorCredentialAction{Command: command, Fence: paDomainFence(q.Fence), ExpectedVersion: q.ExpectedVersion, Ref: pa.ExtensionRef{ID: q.Ref.ID, Fingerprint: q.Ref.Fingerprint}, Action: q.Action, Target: q.Target}, value, secret)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Contract   string                 `json:"contract_version"`
		Credential pa.ConnectorCredential `json:"credential"`
	}{pa.ContractVersion, out})
	if err != nil {
		return nil, err
	}
	return paEncodedMemorySettings{body, check}, nil
}
