package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	adapter "github.com/sainteye/clawdline/internal/adapters/botnetwork"
	"github.com/sainteye/clawdline/internal/adapters/cloud"
	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	botwire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const assistantSourcePolicyFile = "assistant-source.json"

type assistantSourcePolicy struct {
	AccountID string            `json:"account_id"`
	MachineID string            `json:"machine_id"`
	KeyID     string            `json:"gateway_key_id"`
	Gateway   ed25519.PublicKey `json:"gateway_public_key"`
	Link      []byte            `json:"link_key"`
}

type localAssistantSource struct {
	*adapter.AssistantStore
	owner   *projectAssistantNetwork
	policy  app.AssistantPolicy
	pin     [sha256.Size]byte
	service *app.AssistantService
	sealed  *adapter.AssistantSealedSource
	binding atomic.Pointer[assistantSourceSocket]
	refused atomic.Int64
}

type assistantDelivery struct {
	gen uint64
	raw []byte
}

type assistantSourceSocket struct {
	socket  *cloud.Transport
	pending chan assistantDelivery
}

func readAssistantSourcePolicy(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, botwire.AssistantUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, botwire.AssistantUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm() != 0600 {
		return nil, botwire.AssistantUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, botwire.MaxBodyBytes+1))
	if err != nil || len(raw) > botwire.MaxBodyBytes {
		return nil, botwire.AssistantUnavailable
	}
	return raw, nil
}

func (n *projectAssistantNetwork) prepareAssistant(ctx context.Context) {
	if n == nil {
		return
	}
	raw, err := readAssistantSourcePolicy(filepath.Join(n.server.cfg.Dir, assistantSourcePolicyFile))
	if err != nil {
		return
	}
	var p assistantSourcePolicy
	if !decodeAssistantFrame(raw, &p) || p.AccountID != n.identity.AccountID || p.MachineID != n.identity.MachineID ||
		len(p.Gateway) != ed25519.PublicKeySize || bytes.Equal(p.Gateway, n.signer.Public().(ed25519.PublicKey)) ||
		len(p.Link) != sha256.Size || p.KeyID == "" {
		return
	}
	receipts, err := adapter.NewAssistantStore(ctx, n.server.store.ProjectAssistantDatabase(), p.AccountID, p.MachineID)
	if err != nil {
		return
	}
	a := &localAssistantSource{AssistantStore: receipts, owner: n, pin: sha256.Sum256(raw),
		policy: app.AssistantPolicy{AccountID: p.AccountID, MachineID: p.MachineID, Version: 1,
			GlobalOptIn: true, GatewayKeyID: p.KeyID, GatewayPublicKey: p.Gateway}}
	a.service, err = app.NewAssistant(app.AssistantOptions{AccountID: p.AccountID, MachineID: p.MachineID,
		Repository: a, Host: n.host, Clock: time.Now})
	if err != nil {
		return
	}
	a.sealed, err = adapter.NewAssistantSealedSource(p.AccountID, p.MachineID, p.Link, a.service)
	if err == nil {
		n.assistant = a
	}
}

func (n *projectAssistantNetwork) assistantService() *app.AssistantService {
	if n == nil || n.assistant == nil {
		return nil
	}
	return n.assistant.service
}

func (a *localAssistantSource) AssistantPolicy(ctx context.Context) (app.AssistantPolicy, error) {
	if err := ctx.Err(); err != nil {
		return app.AssistantPolicy{}, err
	}
	if err := a.owner.current(ctx); err != nil {
		return app.AssistantPolicy{}, err
	}
	raw, err := readAssistantSourcePolicy(filepath.Join(a.owner.server.cfg.Dir, assistantSourcePolicyFile))
	if err != nil || sha256.Sum256(raw) != a.pin {
		return app.AssistantPolicy{}, botwire.AssistantUnavailable
	}
	return a.policy, nil
}

func (a *localAssistantSource) AssistantGrant(ctx context.Context, id string) (botwire.AssistantGrant, error) {
	row, err := a.owner.client.AssistantConsent(ctx, id)
	if err != nil {
		return botwire.AssistantGrant{}, err
	}
	g := botwire.AssistantGrant{ID: id, Epoch: row.Epoch, KeyID: a.policy.GatewayKeyID, PublicKey: a.policy.GatewayPublicKey,
		PrincipalID: row.PrincipalID, AccountID: row.AccountID, MachineID: row.Target.MachineID, ProjectID: row.Target.ProjectID,
		BotIDs: []string{row.Target.BotID}, Capture: &row.Target, Revoked: row.RevokedAt != nil,
		NotBefore: time.Unix(row.IssuedAt, 0), ExpiresAt: time.Unix(row.ExpiresAt, 0)}
	for _, kind := range row.Kinds {
		switch kind {
		case "directory":
			g.AllowList = true
		case "message":
			g.AllowMessage = true
		case "read":
			g.AllowRead = true
		}
	}
	return g, nil
}

func (a *localAssistantSource) capability(ctx context.Context) pa.NetworkCapability {
	if binding := a.binding.Load(); binding != nil {
		if _, ready := binding.socket.AssistantRailState(); ready {
			if _, err := a.AssistantPolicy(ctx); err == nil {
				return pa.NetworkCapability{State: pa.NetworkReady}
			}
		}
	}
	return pa.NetworkCapability{State: pa.NetworkUnavailable, Reason: "assistant_unavailable"}
}

type assistantFrame struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	DeliveryID string `json:"delivery_id"`
	Sealed     string `json:"sealed"`
}

func decodeAssistantFrame(raw []byte, out any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(out) == nil && dec.Decode(new(any)) == io.EOF
}

func (s *Server) runAssistantSources(ctx context.Context, socket *cloud.Transport) {
	var owner *localAssistantSource
	var stop func()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		s.projectAssistants.mu.RLock()
		n := s.projectAssistants.network
		s.projectAssistants.mu.RUnlock()
		var next *localAssistantSource
		if n != nil && n.assistant != nil {
			if _, err := n.assistant.AssistantPolicy(ctx); err == nil {
				next = n.assistant
			}
		}
		if next != owner {
			if stop != nil {
				stop()
				stop = nil
			}
			owner = next
			if owner != nil {
				stop = owner.attach(ctx, socket)
			}
		}
		if owner != nil {
			_ = socket.NegotiateAssistantRail()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *localAssistantSource) attach(parent context.Context, socket *cloud.Transport) func() {
	ctx, cancel := context.WithCancel(parent)
	binding := &assistantSourceSocket{socket: socket, pending: make(chan assistantDelivery, pa.MaxNetworkCloudFrames)}
	done := make(chan struct{})
	socket.SetAssistantRail(func(gen uint64, raw []byte) {
		select {
		case binding.pending <- assistantDelivery{gen, raw}:
		case <-ctx.Done():
		default:
			a.refused.Add(1)
		}
	})
	a.binding.Store(binding)
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case d := <-binding.pending:
				a.receive(ctx, socket, d.gen, d.raw)
			}
		}
	}()
	return func() {
		cancel()
		a.binding.Store(nil)
		socket.SetAssistantRail(nil)
		<-done
	}
}

func (a *localAssistantSource) receive(parent context.Context, socket *cloud.Transport, gen uint64, raw []byte) {
	if current, ready := socket.AssistantRailState(); !ready || current != gen {
		return
	}
	var frame assistantFrame
	if !decodeAssistantFrame(raw, &frame) || frame.Type != "bot_assistant_call" || frame.Version != 1 || frame.DeliveryID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, cloud.OpeningTimeout)
	defer cancel()
	if _, err := a.AssistantPolicy(ctx); err != nil {
		return
	}
	sealed, err := a.sealed.Handle(ctx, frame.Sealed)
	if err != nil {
		return
	}
	if _, err = a.AssistantPolicy(ctx); err == nil {
		_ = socket.ReplyAssistant(gen, frame.DeliveryID, sealed)
	}
}
