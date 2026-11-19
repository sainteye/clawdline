package http

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	adapter "github.com/sainteye/clawdline/internal/adapters/botnetwork"
	cloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/adapters/projectassistanthost"
	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	botwire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The source object exists before Host.Enable, but opens neither a feature
// table nor a pair database until real feature/write policy allows it.
type projectAssistantNetwork struct {
	server      *Server
	identity    cloud.Identity
	client      *cloud.BotNetworkClient
	signer      ed25519.PrivateKey
	host        *projectassistanthost.NetworkHost
	network     *app.Service
	mu          sync.Mutex
	store       *adapter.Store
	pairs       *peerstore.Store
	rail        *adapter.CloudRail
	cancel      context.CancelFunc
	lease       cloud.BotCapabilities
	nextRefresh time.Time
	assistant   *localAssistantSource
}

func (s *Server) prepareBotNetwork(i cloud.Identity) *projectAssistantNetwork {
	if !i.Valid() {
		return nil
	}
	keys, e := cloudkeys.Open(s.cfg.Dir)
	if e != nil {
		return nil
	}
	key, found, e := keys.DeviceKey()
	if e != nil || !found || !key.Valid() {
		return nil
	}
	n := &projectAssistantNetwork{server: s, identity: i, client: cloud.NewBotNetworkClient(i), signer: ed25519.NewKeyFromSeed(key.Seed()), host: &projectassistanthost.NetworkHost{AccountID: i.AccountID}}
	n.network, e = app.New(app.Options{AccountID: i.AccountID, MachineID: i.MachineID, PrivateFor: n.private, Signer: n.signer, Repository: n, Host: n.host, Clock: time.Now, Authority: n})
	if e != nil {
		return nil
	}
	return n
}
func (n *projectAssistantNetwork) service() *app.Service {
	if n == nil {
		return nil
	}
	return n.network
}
func (n *projectAssistantNetwork) current(ctx context.Context) error {
	i, e := n.server.projectAssistantIdentity()
	if e != nil || i.AccountID != n.identity.AccountID || i.MachineID != n.identity.MachineID || i.MachineCredential != n.identity.MachineCredential || i.APIBase != n.identity.APIBase {
		return paHTTPFailure(pa.Unpaired, "bot_cloud_identity_changed")
	}
	settings, e := cloud.ReadSettings(n.server.settingsFile())
	if e != nil || !settings.Enabled || cloud.CheckEnvironment(i, settings) != nil {
		return paHTTPFailure(pa.Unsupported, "bot_cloud_disabled")
	}
	keys, e := cloudkeys.Open(n.server.cfg.Dir)
	if e != nil {
		return paHTTPFailure(pa.Unpaired, "bot_cloud_key")
	}
	key, found, e := keys.DeviceKey()
	if e != nil || !found || string(key.PublicKey()) != string(n.signer.Public().(ed25519.PublicKey)) {
		return paHTTPFailure(pa.Unpaired, "bot_cloud_key_changed")
	}
	_, e = n.server.projectAssistantSource(ctx, pa.BotKey{MachineID: i.MachineID}, true)
	return e
}
func (n *projectAssistantNetwork) active(ctx context.Context) (*adapter.Store, *adapter.CloudAuthority, error) {
	if e := n.current(ctx); e != nil {
		return nil, nil, e
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.store == nil {
		st, e := adapter.NewStore(ctx, n.server.store.ProjectAssistantDatabase(), n.identity.MachineID)
		if e != nil {
			return nil, nil, e
		}
		pairs, e := peerstore.Open(filepath.Join(n.server.cfg.Dir, cloudkeys.DirName))
		if e != nil {
			return nil, nil, e
		}
		n.store, n.pairs = st, pairs
	}
	return n.store, &adapter.CloudAuthority{Client: n.client, Store: n.store, Pairs: n.pairs, Signer: n.signer, Current: n.current}, nil
}
func (n *projectAssistantNetwork) private(ctx context.Context, id string) (*ecdh.PrivateKey, error) {
	_, a, e := n.active(ctx)
	if e != nil {
		return nil, e
	}
	return a.Private(ctx, id)
}
func (n *projectAssistantNetwork) CheckBotAuthority(ctx context.Context, h botwire.BotHeader) error {
	_, a, e := n.active(ctx)
	if e != nil {
		return e
	}
	return a.CheckBotAuthority(ctx, h)
}
func (n *projectAssistantNetwork) Outgoing(ctx context.Context, ep botwire.BotEndpoint) ([]app.Grant, error) {
	n.mu.Lock()
	ready := n.rail != nil && n.rail.Ready() && n.lease.ExpiresAt > time.Now().Unix()
	n.mu.Unlock()
	if !ready {
		return nil, paHTTPFailure(pa.Unsupported, "bot_cloud_unavailable")
	}
	st, _, e := n.active(ctx)
	if e != nil {
		return nil, e
	}
	grants, e := st.Outgoing(ctx, ep)
	if e != nil {
		return nil, e
	}
	out := []app.Grant{}
	for _, g := range grants {
		cap, e := n.client.Capabilities(ctx, g.Target.MachineID)
		if e != nil {
			return nil, e
		}
		if cap.State != "ready" {
			return nil, paHTTPFailure(pa.Unsupported, "bot_cloud_target_unavailable")
		}
		for _, target := range cap.Endpoints {
			old := g.Target
			old.Generation = target.Generation
			if old == target {
				g.Target = target
				out = append(out, g)
				break
			}
		}
	}
	return out, nil
}
func (n *projectAssistantNetwork) Grant(ctx context.Context, id string) (app.Grant, error) {
	st, _, e := n.active(ctx)
	if e != nil {
		return app.Grant{}, e
	}
	return st.Grant(ctx, id)
}
func (n *projectAssistantNetwork) Claim(ctx context.Context, h botwire.BotHeader) (app.Receipt, bool, error) {
	st, _, e := n.active(ctx)
	if e != nil {
		return app.Receipt{}, false, e
	}
	return st.Claim(ctx, h)
}
func (n *projectAssistantNetwork) Receipt(ctx context.Context, h botwire.BotHeader, id string) (app.Receipt, error) {
	st, _, e := n.active(ctx)
	if e != nil {
		return app.Receipt{}, e
	}
	return st.Receipt(ctx, h, id)
}
func (n *projectAssistantNetwork) Save(ctx context.Context, r app.Receipt) error {
	st, _, e := n.active(ctx)
	if e != nil {
		return e
	}
	return st.Save(ctx, r)
}
func (n *projectAssistantNetwork) stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cancel != nil {
		n.cancel()
	}
	n.lease = cloud.BotCapabilities{}
}
func (s *Server) botNetworkAccount(context.Context) (string, error) {
	s.projectAssistants.mu.RLock()
	n := s.projectAssistants.network
	machine := s.projectAssistants.machine
	s.projectAssistants.mu.RUnlock()
	if n == nil {
		return "unconfigured:" + machine, nil
	}
	return n.identity.AccountID, nil
}
func (s *Server) botNetworkCapabilities(ctx context.Context, account, machine string) pa.NetworkCapabilities {
	result := pa.NetworkCapabilities{Peer: pa.NetworkCapability{State: pa.NetworkUnavailable, Reason: "bot_cloud_unavailable"}, Assistants: pa.NetworkCapability{State: pa.NetworkUnavailable, Reason: "assistant_unconfigured"}}
	s.projectAssistants.mu.RLock()
	n := s.projectAssistants.network
	s.projectAssistants.mu.RUnlock()
	if n == nil || account != n.identity.AccountID || machine != n.identity.MachineID || n.current(ctx) != nil {
		return result
	}
	n.mu.Lock()
	ready := n.rail != nil && n.rail.Ready() && n.lease.ExpiresAt > time.Now().Unix()
	n.mu.Unlock()
	if ready {
		result.Peer = pa.NetworkCapability{State: pa.NetworkReady}
	}
	if n.assistant != nil {
		result.Assistants = n.assistant.capability(ctx)
	}
	return result
}
func (s *Server) botNetworkTransport(ctx context.Context, machine string) (app.Transport, error) {
	s.projectAssistants.mu.RLock()
	n := s.projectAssistants.network
	s.projectAssistants.mu.RUnlock()
	if n == nil || n.current(ctx) != nil {
		return nil, paHTTPFailure(pa.Unsupported, "bot_cloud_unavailable")
	}
	n.mu.Lock()
	rail := n.rail
	n.mu.Unlock()
	if rail == nil || !rail.Ready() {
		return nil, paHTTPFailure(pa.Unsupported, "bot_cloud_unavailable")
	}
	return rail, nil
}

// StartBotNetwork binds the normal authenticated Cloud socket. It does no
// network IO or store creation while Bots are off. The Cloud lifecycle owner
// must await done before attaching a replacement socket.
func (s *Server) StartBotNetwork(ctx context.Context, socket *cloud.Transport) <-chan struct{} {
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(done)
		defer cancel()
		assistantDone := make(chan struct{})
		go func() {
			defer close(assistantDone)
			s.runAssistantSources(ctx, socket)
		}()
		defer func() {
			cancel()
			<-assistantDone
		}()
		var owner *projectAssistantNetwork
		defer func() {
			if owner != nil {
				owner.stop()
				owner.mu.Lock()
				owner.rail = nil
				owner.cancel = nil
				owner.lease = cloud.BotCapabilities{}
				owner.mu.Unlock()
			}
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var railDone chan struct{}
		defer func() {
			cancel()
			if railDone != nil {
				<-railDone
			}
		}()
		for ctx.Err() == nil {
			s.projectAssistants.mu.RLock()
			n := s.projectAssistants.network
			s.projectAssistants.mu.RUnlock()
			if n != nil && n.current(ctx) == nil {
				owner = n
				n.mu.Lock()
				rail := n.rail
				n.mu.Unlock()
				if rail == nil {
					st, _, e := n.active(ctx)
					if e == nil {
						railCtx, stopRail := context.WithCancel(ctx)
						rail, e = adapter.NewCloudRail(railCtx, socket, n.network, st, n.signer, n.identity.AccountID, n.identity.MachineID, n.current)
						if e == nil {
							n.mu.Lock()
							n.rail, n.cancel = rail, stopRail
							n.mu.Unlock()
							railDone = make(chan struct{})
							go func() { defer close(railDone); rail.Run() }()
						} else {
							stopRail()
						}
					}
				}
				if rail != nil && rail.Ready() {
					_ = n.refresh(ctx)
				}
			} else if owner != nil {
				owner.stop()
				if railDone != nil {
					<-railDone
					railDone = nil
				}
				owner.mu.Lock()
				owner.lease = cloud.BotCapabilities{}
				owner.rail = nil
				owner.cancel = nil
				owner.mu.Unlock()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}
func (n *projectAssistantNetwork) refresh(ctx context.Context) error {
	n.mu.Lock()
	if time.Now().Before(n.nextRefresh) {
		n.mu.Unlock()
		return nil
	}
	n.mu.Unlock()
	endpoints, e := n.host.Host.NetworkCaptures(ctx, n.identity.AccountID)
	if e != nil {
		return e
	}
	n.mu.Lock()
	lease := n.lease
	n.mu.Unlock()
	a, _ := json.Marshal(lease.Endpoints)
	b, _ := json.Marshal(endpoints)
	if lease.ExpiresAt > time.Now().Unix()+pa.MaxNetworkCloudRefreshSeconds && string(a) == string(b) {
		return nil
	}
	st, _, e := n.active(ctx)
	if e != nil {
		return e
	}
	revision, pending, e := st.LeaseRevision(ctx)
	if e != nil {
		return e
	}
	if current, readErr := n.client.Capabilities(ctx, n.identity.MachineID); readErr == nil {
		revision = current.Revision
		pending = false
		if e = st.SaveLeaseRevision(ctx, revision, false); e != nil {
			return e
		}
	} else if pending {
		n.mu.Lock()
		n.lease = cloud.BotCapabilities{}
		n.nextRefresh = time.Now().Add(pa.MaxNetworkCloudRefreshSeconds * time.Second)
		n.mu.Unlock()
		return paHTTPFailure(pa.OutcomeUnknown, "bot_cloud_lease_unknown")
	}
	if e = st.SaveLeaseRevision(ctx, revision, true); e != nil {
		return e
	}
	updated, e := n.client.PutCapabilities(ctx, revision, endpoints)
	if e != nil {
		n.mu.Lock()
		n.lease = cloud.BotCapabilities{Revision: revision}
		n.nextRefresh = time.Now().Add(pa.MaxNetworkCloudRefreshSeconds * time.Second)
		n.mu.Unlock()
		return e
	}
	if e = st.SaveLeaseRevision(ctx, updated.Revision, false); e != nil {
		return e
	}
	if e = n.current(ctx); e != nil {
		return e
	}
	n.mu.Lock()
	n.lease = updated
	n.mu.Unlock()
	return nil
}
