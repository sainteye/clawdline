package cloud

// Peer frames have a separate trust root from viewer envelopes. Only a
// locally pinned, doubly signed machine pair may establish the principal.

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	peerapp "github.com/sainteye/clawdline/internal/app/agenthandoff"
	peercontract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

const (
	PeerIngressLimit    = 16
	PeerAckIngressLimit = 16
)

// PeerIngressUsage measures the fuller of the work and relay-ack queues.
// Each has the same registered capacity and both refuse new frames when full.
func (l *Link) PeerIngressUsage() (int, bool) {
	l.mu.Lock()
	requests, acks := l.peerRequests, l.peerAcks
	l.mu.Unlock()
	if requests == nil || acks == nil {
		return 0, false
	}
	return max(len(requests), len(acks)), true
}

type peerFrame struct {
	Type            string `json:"type"`
	SourcePublicKey string `json:"source_public_key,omitempty"`
	peercontract.Envelope
}

type peerAck struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

func (l *Link) enqueuePeer(frame []byte) {
	if len(frame) > adaptercloud.PeerFrameBytesLimit || l.peerRequests == nil {
		return
	}
	copyFrame := append([]byte(nil), frame...)
	select {
	case l.peerRequests <- copyFrame:
	default:
		l.logf("cloud peer: inbound queue full; frame outcome unknown")
	}
}

func (l *Link) enqueuePeerAck(frame []byte) {
	if len(frame) > adaptercloud.PeerFrameBytesLimit || l.peerAcks == nil {
		return
	}
	select {
	case l.peerAcks <- append([]byte(nil), frame...):
	default:
		l.logf("cloud peer: ack queue full; relay delivery outcome unknown")
	}
}

func (l *Link) runPeer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-l.peerRequests:
			requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			l.receivePeer(requestCtx, frame)
			cancel()
		case frame := <-l.peerAcks:
			l.recordPeerAck(ctx, frame)
		}
	}
}

func (l *Link) recordPeerAck(ctx context.Context, raw []byte) {
	var ack peerAck
	if json.Unmarshal(raw, &ack) != nil || ack.Type != "peer_ack" || ack.RequestID == "" ||
		(ack.Status != "delivered" && ack.Status != "machine_offline" && ack.Status != "unknown") {
		return
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	if err := st.MarkOutbox(ctx, ack.RequestID, "accepted", ack.Status); err != nil {
		l.logf("cloud peer: could not record ack for %s: %v", ack.RequestID, err)
	}
}

func (l *Link) receivePeer(ctx context.Context, raw []byte) {
	if l.opts.PeerReceipts == nil || l.opts.PeerAdmitTarget == nil || l.opts.PeerExecute == nil {
		return
	}
	var frame peerFrame
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "peer_envelope" ||
		frame.Request.Target.MachineID != l.identity.MachineID {
		return
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	pair, _, err := peerAuthorityRows(ctx, st, frame.Request.GrantID)
	if err != nil || pair.TargetMachineID != l.identity.MachineID ||
		pair.SourceMachineID != frame.Request.Source.MachineID ||
		pair.TargetSignature == "" || !pair.RevokedAt.IsZero() ||
		!l.opts.Now().Before(pair.ExpiresAt) {
		return
	}
	// The relay hint is deliberately ignored; only the local signed pin can
	// supply a signing key or principal.
	signer, err := base64.StdEncoding.Strict().DecodeString(pair.SourcePublicKey)
	if err != nil || len(signer) != ed25519.PublicKeySize {
		return
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return
	}
	keys := pairKeys(pair)
	body, err := peercontract.Open(frame.Envelope, keys, private, signer)
	if err != nil {
		return
	}
	principal := peercontract.Principal{MachineID: pair.SourceMachineID,
		KeyFingerprint: pair.SourceFingerprint, PairID: pair.ID, MachinePeer: true}
	receiver := peerapp.Receiver{Receipts: l.opts.PeerReceipts,
		Facts: func(ctx context.Context, request peercontract.Request, principal peercontract.Principal) (peercontract.Facts, error) {
			return l.peerFacts(ctx, request, principal)
		},
		Execute: l.opts.PeerExecute, Now: l.opts.Now}
	result := receiver.Receive(ctx, frame.Request, body, principal)
	l.logf("cloud peer: request %s machine execution %s code %s", result.RequestID, result.MachineExecution, result.Code)
}

func pairKeys(pair peerstore.Pair) peercontract.PairKeys {
	return peercontract.PairKeys{PairID: pair.ID, SourceMachineID: pair.SourceMachineID,
		TargetMachineID: pair.TargetMachineID, SourceEncryptionKey: pair.SourceEncryptionKey,
		TargetEncryptionKey: pair.TargetEncryptionKey}
}

// A machine pair is the authority for every current Session on its directed
// edge. Older, Session-specific grants remain readable for in-flight work.
func peerAuthorityRows(ctx context.Context, st *peerstore.Store, id string) (peerstore.Pair, *peerstore.Grant, error) {
	pair, err := st.Pair(ctx, id)
	if err == nil {
		return pair, nil, nil
	}
	if !errors.Is(err, peerstore.ErrNotFound) {
		return peerstore.Pair{}, nil, err
	}
	grant, err := st.Grant(ctx, id)
	if err != nil || !grant.RevokedAt.IsZero() {
		return peerstore.Pair{}, nil, errors.New("peer_grant_denied")
	}
	pair, err = st.Pair(ctx, grant.PairID)
	return pair, &grant, err
}

func (l *Link) peerFacts(ctx context.Context, request peercontract.Request,
	principal peercontract.Principal) (peercontract.Facts, error) {
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return peercontract.Facts{}, err
	}
	defer st.Close()
	pair, grant, err := peerAuthorityRows(ctx, st, request.GrantID)
	if err != nil {
		return peercontract.Facts{}, err
	}
	if pair.ID != principal.PairID || pair.SourceFingerprint != principal.KeyFingerprint ||
		pair.SourceMachineID != principal.MachineID || pair.TargetMachineID != l.identity.MachineID {
		return peercontract.Facts{}, errors.New("peer_pin_mismatch")
	}
	// Cloud is a second revocation authority. Its answer cannot supply the
	// principal, and an unreadable or negative answer closes admission.
	authority, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeer(ctx,
		l.identity.MachineCredential, request, pair.SourcePublicKey)
	if err != nil || !authority.Authorized || authority.SourcePublicKey != pair.SourcePublicKey ||
		authority.SourceKeyFingerprint != pair.SourceFingerprint ||
		authority.TargetMachineID != l.identity.MachineID || authority.GrantID != request.GrantID {
		return peercontract.Facts{}, errors.New("peer_authority_unavailable")
	}
	current := l.opts.PeerAdmitTarget(ctx, request.Target.MachineID,
		request.Target.SessionID, request.Target.ExecutionGeneration) == nil
	return peercontract.Facts{LocalMachineID: l.identity.MachineID,
		Principal: principal, PairActive: pair.TargetSignature != "" && pair.RevokedAt.IsZero() && l.opts.Now().Before(pair.ExpiresAt),
		MachineAccess: grant == nil, GrantReadable: grant != nil,
		Grant:          peerGrantFacts(grant),
		PeerCapability: true, LocalCapability: true,
		MachineWritesAllowed: l.allowCommands(), TargetCurrent: current, Now: l.opts.Now()}, nil
}

func peerGrantFacts(grant *peerstore.Grant) peercontract.Grant {
	if grant == nil {
		return peercontract.Grant{}
	}
	return peercontract.Grant{ID: grant.ID, PairID: grant.PairID,
		SourceMachineID: grant.Source.MachineID, SourceSessionID: grant.Source.SessionID,
		SourceGeneration: grant.Source.ExecutionGeneration, TargetMachineID: grant.Target.MachineID,
		TargetSessionID: grant.Target.SessionID, TargetGeneration: grant.Target.ExecutionGeneration,
		PeerKeyFingerprint: grant.SourceKeyFingerprint, AllowMessage: grant.AllowMessage,
		AllowHandoff: grant.AllowHandoff, Revoked: !grant.RevokedAt.IsZero(), ExpiresAt: grant.ExpiresAt}
}

// PublishPeer signs and encrypts a fixed source and target. The result reports
// only the local write to the relay socket; it makes no execution claim.
func (l *Link) PublishPeer(ctx context.Context, request peercontract.Request, body []byte) (peerapp.Result, error) {
	answer := peerapp.Result{RequestID: request.RequestID, MachineExecution: "unknown",
		RelayAccepted: "unknown", RelayDelivered: "unknown", SessionDelivered: "unknown",
		AgentObserved: "unknown", AgentAcknowledged: "unknown"}
	if l.transport == nil || l.keys == nil || request.Source.MachineID != l.identity.MachineID ||
		l.opts.PeerAdmitTarget == nil || !l.allowCommands() || len(body) == 0 || len(body) > peercontract.MaxBodyBytes {
		return answer, errors.New("peer_send_unavailable")
	}
	if err := peercontract.Validate(request, body); err != nil {
		return answer, err
	}
	if err := l.opts.PeerAdmitTarget(ctx, request.Source.MachineID,
		request.Source.SessionID, request.Source.ExecutionGeneration); err != nil {
		return answer, err
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return answer, err
	}
	defer st.Close()
	pair, grant, err := peerAuthorityRows(ctx, st, request.GrantID)
	if grant != nil && (!l.opts.Now().Before(grant.ExpiresAt) ||
		grant.Source != request.Source || grant.Target != request.Target ||
		(request.Kind == peercontract.Message && !grant.AllowMessage) ||
		(request.Kind == peercontract.Handoff && !grant.AllowHandoff)) {
		return answer, errors.New("peer_grant_denied")
	}
	if err != nil || pair.SourceMachineID != l.identity.MachineID ||
		pair.TargetMachineID != request.Target.MachineID || pair.TargetSignature == "" ||
		(grant != nil && (grant.SourceKeyFingerprint != pair.SourceFingerprint || grant.ExpiresAt.After(pair.ExpiresAt))) ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return answer, errors.New("peer_pair_inactive")
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != pair.SourceFingerprint {
		return answer, errors.New("peer_signer_changed")
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeer(ctx,
		l.identity.MachineCredential, request, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.SourcePublicKey != pair.SourcePublicKey ||
		auth.SourceKeyFingerprint != pair.SourceFingerprint ||
		auth.TargetMachineID != request.Target.MachineID || auth.GrantID != request.GrantID {
		return answer, errors.New("peer_authority_unavailable")
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return answer, err
	}
	envelope, err := peercontract.Seal(request, body, pairKeys(pair), private,
		ed25519.NewKeyFromSeed(key.Seed()))
	if err != nil {
		return answer, err
	}
	frame, err := json.Marshal(struct {
		Type string `json:"type"`
		peercontract.Envelope
	}{Type: "peer_publish", Envelope: envelope})
	if err != nil {
		return answer, err
	}
	if err := st.SaveOutbox(ctx, peerstore.OutboxItem{Request: request, RelayAccepted: "unknown",
		RelayDelivered: "unknown", At: l.opts.Now()}); err != nil {
		return answer, err
	}
	if err := l.transport.PublishPeer(frame); err != nil {
		return answer, err
	}
	return answer, nil
}
