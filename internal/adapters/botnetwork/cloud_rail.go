package botnetwork

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	cloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func cloudCanonical(b []byte) []byte {
	v, e := domaincloud.Parse(b)
	if e != nil {
		return nil
	}
	return domaincloud.CanonicalBytes(v)
}
func railDecode(raw []byte, out any) error {
	// The canonical parser rejects duplicate keys before struct decoding.
	if _, e := domaincloud.Parse(raw); e != nil {
		return refuse(pa.Invalid, "bot_cloud_frame")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return refuse(pa.Invalid, "bot_cloud_frame")
	}
	return nil
}

type cloudDelivery struct {
	Type         string `json:"type"`
	RequestID    string `json:"request_id"`
	HeaderDigest string `json:"header_digest"`
	FrameDigest  string `json:"frame_digest"`
	DeliveryID   string `json:"delivery_id"`
}
type railFrame struct {
	generation uint64
	raw        []byte
}
type railReply struct {
	envelope wire.BotEnvelope
	err      error
}
type railPending struct {
	header     wire.BotHeader
	result     chan railReply
	generation uint64
}
type CloudRail struct {
	Socket               *cloud.Transport
	Service              *app.Service
	Store                *Store
	Signer               ed25519.PrivateKey
	AccountID, MachineID string
	Current              func(context.Context) error
	ctx                  context.Context
	queue                chan railFrame
	mu                   sync.Mutex
	pending              map[string]railPending
	deliveries           map[string]cloudDelivery
	connection           string
	deliveryGeneration   uint64
	broken               atomic.Bool
	refused              atomic.Int64
}

func NewCloudRail(ctx context.Context, socket *cloud.Transport, service *app.Service, store *Store, signer ed25519.PrivateKey, account, machine string, current func(context.Context) error) (*CloudRail, error) {
	if socket == nil || service == nil || store == nil || len(signer) != ed25519.PrivateKeySize || current == nil {
		return nil, refuse(pa.Invalid, "bot_cloud_rail")
	}
	if e := store.InstallCloudReceipts(ctx); e != nil {
		return nil, e
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return nil, e
	}
	r := &CloudRail{Socket: socket, Service: service, Store: store, Signer: signer, AccountID: account, MachineID: machine, Current: current, ctx: ctx, queue: make(chan railFrame, pa.MaxNetworkCloudFrames), pending: map[string]railPending{}, deliveries: map[string]cloudDelivery{}, connection: hex.EncodeToString(nonce[:])}
	return r, nil
}
func (r *CloudRail) Enqueue(g uint64, raw []byte) {
	if r.broken.Load() {
		return
	}
	select {
	case r.queue <- railFrame{g, raw}:
	default:
		r.refused.Add(1)
		r.broken.Store(true)
	}
}
func (r *CloudRail) Ready() bool {
	_, ready := r.Socket.BotNetworkState()
	return ready && !r.broken.Load() && r.ctx.Err() == nil
}
func (r *CloudRail) Usage() (int, int, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queue), len(r.pending), r.refused.Load()
}
func (r *CloudRail) Run() {
	r.Socket.SetBotNetwork(r.Enqueue)
	defer r.Socket.SetBotNetwork(nil)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.ctx.Done():
			r.failPending(refuse(pa.ExecutionEffectUnknown, "bot_cloud_stopped"))
			return
		case <-tick.C:
			if r.broken.Load() {
				r.failPending(refuse(pa.ExecutionEffectUnknown, "bot_cloud_backpressure"))
				continue
			}
			if r.Current(r.ctx) == nil {
				_ = r.Socket.NegotiateBotNetwork()
			}
			g, ready := r.Socket.BotNetworkState()
			r.mu.Lock()
			for id, p := range r.pending {
				if !ready || p.generation != g {
					p.result <- railReply{err: refuse(pa.ExecutionEffectUnknown, "bot_cloud_disconnected")}
					delete(r.pending, id)
				}
			}
			r.mu.Unlock()
		case f := <-r.queue:
			g, ready := r.Socket.BotNetworkState()
			if !ready || g != f.generation || r.Current(r.ctx) != nil {
				continue
			}
			if r.deliveryGeneration != g {
				r.deliveries = map[string]cloudDelivery{}
				r.deliveryGeneration = g
			}
			if e := r.handle(f); e != nil {
				r.refused.Add(1)
			}
		}
	}
}
func (r *CloudRail) failPending(e error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, p := range r.pending {
		p.result <- railReply{err: e}
		delete(r.pending, id)
	}
}

// Exchange retains an attempt before writing and never resends it. A timeout,
// disconnect or lost result is unknown, including an earlier accepted effect.
func (r *CloudRail) Exchange(ctx context.Context, e wire.BotEnvelope) (wire.BotEnvelope, error) {
	if !r.Ready() || r.Current(ctx) != nil {
		return wire.BotEnvelope{}, refuse(pa.Unsupported, "bot_cloud_unavailable")
	}
	g, _ := r.Socket.BotNetworkState()
	hd, fd := CloudDigests(e)
	r.mu.Lock()
	if len(r.pending) >= pa.MaxNetworkCloudPending {
		r.mu.Unlock()
		return wire.BotEnvelope{}, refuse(pa.CapacityFull, "bot_cloud_pending")
	}
	if _, ok := r.pending[e.Header.RequestID]; ok {
		r.mu.Unlock()
		return wire.BotEnvelope{}, refuse(pa.IdempotencyConflict, "bot_cloud_pending")
	}
	p := railPending{header: e.Header, result: make(chan railReply, 1), generation: g}
	r.pending[e.Header.RequestID] = p
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, e.Header.RequestID); r.mu.Unlock() }()
	fresh, err := r.Store.RetainCloud(ctx, "send", CloudReceipt{Header: e.Header, HeaderDigest: hd, FrameDigest: fd, Status: "unknown"})
	if err != nil {
		return wire.BotEnvelope{}, err
	}
	if !fresh {
		return wire.BotEnvelope{}, refuse(pa.ExecutionEffectUnknown, "bot_cloud_retained")
	}
	raw, _ := json.Marshal(struct {
		Type string `json:"type"`
		wire.BotEnvelope
	}{"bot_network_publish", e})
	if err = r.Socket.SendBotNetwork(g, raw); err != nil {
		return wire.BotEnvelope{}, err
	}
	select {
	case <-ctx.Done():
		return wire.BotEnvelope{}, refuse(pa.ExecutionEffectUnknown, "bot_cloud_wait")
	case <-r.ctx.Done():
		return wire.BotEnvelope{}, refuse(pa.ExecutionEffectUnknown, "bot_cloud_stopped")
	case got := <-p.result:
		return got.envelope, got.err
	}
}
func (r *CloudRail) handle(f railFrame) error {
	var kind struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(f.raw, &kind) != nil {
		return refuse(pa.Invalid, "bot_cloud_frame")
	}
	switch kind.Type {
	case "bot_network_delivery":
		var d cloudDelivery
		if e := railDecode(f.raw, &d); e != nil {
			return e
		}
		if d.RequestID == "" || len(d.HeaderDigest) != 64 || len(d.FrameDigest) != 64 || d.DeliveryID == "" {
			return refuse(pa.Invalid, "bot_cloud_delivery")
		}
		if len(r.deliveries) >= pa.MaxNetworkCloudFrames {
			return refuse(pa.CapacityFull, "bot_cloud_deliveries")
		}
		if _, ok := r.deliveries[d.RequestID]; ok {
			return refuse(pa.IdempotencyConflict, "bot_cloud_delivery")
		}
		r.deliveries[d.RequestID] = d
	case "bot_network_envelope":
		var frame struct {
			Type string `json:"type"`
			wire.BotEnvelope
		}
		if e := railDecode(f.raw, &frame); e != nil {
			return e
		}
		e := frame.BotEnvelope
		d, ok := r.deliveries[e.Header.RequestID]
		delete(r.deliveries, e.Header.RequestID)
		hd, fd := CloudDigests(e)
		if !ok || d.HeaderDigest != hd || d.FrameDigest != fd || e.Header.Target.AccountID != r.AccountID || e.Header.Target.MachineID != r.MachineID {
			return refuse(pa.ScopeDenied, "bot_cloud_delivery")
		}
		if err := r.Service.CheckEnvelope(r.ctx, e); err != nil {
			return err
		}
		if _, err := r.Store.RetainCloud(r.ctx, "receive", CloudReceipt{Header: e.Header, HeaderDigest: hd, FrameDigest: fd, DeliveryID: d.DeliveryID, Connection: r.connection + "/" + strconv.FormatUint(f.generation, 10), Status: "target_received"}); err != nil {
			return err
		}
		ackBody := struct {
			AccountID    string `json:"account_id"`
			RequestID    string `json:"request_id"`
			HeaderDigest string `json:"header_digest"`
			FrameDigest  string `json:"frame_digest"`
			DeliveryID   string `json:"delivery_id"`
		}{r.AccountID, d.RequestID, hd, fd, d.DeliveryID}
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(r.Signer, append([]byte("clawdline-bot-network-delivery-v1\n"), canonical(ackBody)...)))
		d.Type = "bot_network_delivery_ack"
		raw, _ := json.Marshal(struct {
			cloudDelivery
			Signature string `json:"signature"`
		}{d, sig})
		if err := r.Socket.SendBotNetwork(f.generation, raw); err != nil {
			return err
		}
		if e.Header.Kind == "reply" {
			r.mu.Lock()
			p, found := r.pending[e.Header.CorrelationID]
			if found {
				expected := p.header
				expected.Source, expected.Target = expected.Target, expected.Source
				expected.Kind = "reply"
				expected.RequestID = app.ResponseID(p.header.RequestID)
				expected.CorrelationID = p.header.RequestID
				expected.BodyDigest = e.Header.BodyDigest
				if expected == e.Header {
					p.result <- railReply{envelope: e}
					delete(r.pending, e.Header.CorrelationID)
				}
			}
			r.mu.Unlock()
			return nil
		}
		reply, err := r.Service.Receive(r.ctx, e)
		if err != nil {
			return err
		}
		hd, fd = CloudDigests(reply)
		fresh, err := r.Store.RetainCloud(r.ctx, "send", CloudReceipt{Header: reply.Header, HeaderDigest: hd, FrameDigest: fd, Status: "unknown"})
		if err != nil {
			return err
		}
		if !fresh {
			return refuse(pa.ExecutionEffectUnknown, "bot_cloud_retained_reply")
		}
		raw, _ = json.Marshal(struct {
			Type string `json:"type"`
			wire.BotEnvelope
		}{"bot_network_publish", reply})
		return r.Socket.SendBotNetwork(f.generation, raw)
	case "bot_network_ack":
		var a struct {
			Type         string `json:"type"`
			RequestID    string `json:"request_id"`
			HeaderDigest string `json:"header_digest"`
			FrameDigest  string `json:"frame_digest"`
			Sender       string `json:"sender"`
			Target       string `json:"target"`
			Status       string `json:"status"`
			AcceptedAt   int64  `json:"accepted_at"`
			DeliveredAt  *int64 `json:"delivered_at"`
			Duplicate    bool   `json:"duplicate,omitempty"`
			Code         string `json:"code,omitempty"`
		}
		if e := railDecode(f.raw, &a); e != nil {
			return e
		}
		if a.Sender != r.MachineID || a.AcceptedAt <= 0 || a.Status == "relay_accepted" && a.DeliveredAt != nil || a.Status == "target_delivered" && (a.DeliveredAt == nil || *a.DeliveredAt < a.AcceptedAt) {
			return refuse(pa.ScopeDenied, "bot_cloud_ack")
		}
		return r.Store.RecordCloudStatus(r.ctx, a.RequestID, a.HeaderDigest, a.FrameDigest, a.Target, a.Status)
	case "bot_network_error":
		var e struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Status    string `json:"status"`
			Code      string `json:"code"`
		}
		if err := railDecode(f.raw, &e); err != nil {
			return err
		}
		r.mu.Lock()
		p, ok := r.pending[e.RequestID]
		if ok {
			p.result <- railReply{err: refuse(pa.ExecutionEffectUnknown, "bot_cloud_transport")}
			delete(r.pending, e.RequestID)
		}
		r.mu.Unlock()
	case "bot_network_delivery_recorded":
		// This fact acknowledges only target transport delivery. Native receipt
		// storage and Agent observation remain separate source facts.
	}
	return nil
}
