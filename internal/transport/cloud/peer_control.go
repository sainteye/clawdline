package cloud

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// PeerControlInput names a single deliberate machine-pair or grant action.
// compared_fingerprint comes from the person's other-machine comparison,
// never from an account roster row selected by the console.
type PeerControlInput struct {
	Action                    string   `json:"action"`
	PairID                    string   `json:"pair_id,omitempty"`
	GrantID                   string   `json:"grant_id,omitempty"`
	TargetMachineID           string   `json:"target_machine_id,omitempty"`
	ComparedFingerprint       string   `json:"compared_fingerprint,omitempty"`
	SourceSessionID           string   `json:"source_session_id,omitempty"`
	SourceExecutionGeneration string   `json:"source_execution_generation,omitempty"`
	TargetSessionID           string   `json:"target_session_id,omitempty"`
	TargetExecutionGeneration string   `json:"target_execution_generation,omitempty"`
	Scopes                    []string `json:"scopes,omitempty"`
}

type PeerControlResult struct {
	Action           string `json:"action"`
	PairID           string `json:"pair_id,omitempty"`
	GrantID          string `json:"grant_id,omitempty"`
	LocalMachineID   string `json:"local_machine_id"`
	LocalFingerprint string `json:"local_fingerprint"`
	PeerMachineID    string `json:"peer_machine_id,omitempty"`
	PeerFingerprint  string `json:"peer_fingerprint,omitempty"`
	State            string `json:"state"`
	// These are present only for status. They describe locally pinned authority,
	// never message content, private keys or an inferred Cloud grant decision.
	Pairs  *[]PeerPairStatus  `json:"pairs,omitempty"`
	Grants *[]PeerGrantStatus `json:"grants,omitempty"`
}

type PeerPairStatus struct {
	PairID            string    `json:"pair_id"`
	SourceMachineID   string    `json:"source_machine_id"`
	TargetMachineID   string    `json:"target_machine_id"`
	SourceFingerprint string    `json:"source_fingerprint"`
	TargetFingerprint string    `json:"target_fingerprint"`
	State             string    `json:"state"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type PeerGrantStatus struct {
	GrantID   string                `json:"grant_id"`
	PairID    string                `json:"pair_id"`
	Source    agenthandoff.Endpoint `json:"source"`
	Target    agenthandoff.Endpoint `json:"target"`
	Scopes    []string              `json:"scopes"`
	ExpiresAt time.Time             `json:"expires_at"`
}

func (l *Link) ControlPeer(ctx context.Context, input PeerControlInput) (PeerControlResult, error) {
	if l.keys == nil || !l.identity.Valid() || l.settings.APIBase == "" {
		return PeerControlResult{}, errors.New("peer_control_unavailable")
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != l.Fingerprint() {
		return PeerControlResult{}, errors.New("peer_signer_changed")
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return PeerControlResult{}, err
	}
	defer st.Close()
	api := adaptercloud.NewAccountClient(l.settings.APIBase)
	base := PeerControlResult{Action: input.Action, LocalMachineID: l.identity.MachineID,
		LocalFingerprint: key.Fingerprint()}
	switch input.Action {
	case "status":
		return peerControlStatus(ctx, st, base, l.identity.AccountID, l.opts.Now())
	case "start":
		if input.TargetMachineID == "" || input.TargetMachineID == l.identity.MachineID || input.ComparedFingerprint == "" {
			return base, errors.New("peer_target_and_compared_fingerprint_required")
		}
		machines, err := api.Machines(ctx, l.identity.MachineCredential)
		if err != nil {
			return base, err
		}
		matched := false
		for _, machine := range machines {
			if machine.ID == input.TargetMachineID && machine.RevokedAt == nil &&
				machine.KeyFingerprint == input.ComparedFingerprint {
				matched = true
			}
		}
		if !matched {
			return base, errors.New("peer_compared_fingerprint_mismatch")
		}
		private, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return base, err
		}
		encryption := base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
		transcript := agenthandoff.PairTranscript{SourceMachineID: l.identity.MachineID,
			TargetMachineID: input.TargetMachineID, SourceFingerprint: key.Fingerprint(),
			TargetFingerprint: input.ComparedFingerprint, SourceEncryptionKey: encryption}
		signature := base64.StdEncoding.EncodeToString(key.Sign(agenthandoff.OfferBytes(transcript)))
		started, err := api.StartPeerPair(ctx, l.identity.MachineCredential, adaptercloud.PeerPairStart{
			TargetMachineID: input.TargetMachineID, TargetKeyFingerprint: input.ComparedFingerprint,
			SourceEncryptionKey: encryption, SourceOfferSignature: signature})
		if err != nil {
			return base, err
		}
		expires, err := time.Parse(time.RFC3339Nano, started.ExpiresAt)
		if err != nil || started.PairID == "" || started.SourceMachineID != l.identity.MachineID ||
			started.TargetMachineID != input.TargetMachineID || started.TargetKeyFingerprint != input.ComparedFingerprint {
			return base, errors.New("peer_api_pair_inconsistent")
		}
		if err := st.SavePending(ctx, peerstore.Pair{ID: started.PairID, AccountID: l.identity.AccountID,
			SourceMachineID: l.identity.MachineID, TargetMachineID: input.TargetMachineID,
			SourcePublicKey:   base64.StdEncoding.EncodeToString(key.PublicKey()),
			SourceFingerprint: key.Fingerprint(), TargetFingerprint: input.ComparedFingerprint,
			SourceEncryptionKey: encryption, SourceSignature: signature,
			LocalPrivateKey: private.Bytes(), ExpiresAt: expires}); err != nil {
			_ = api.RevokePeerPair(ctx, l.identity.MachineCredential, started.PairID)
			return base, err
		}
		base.PairID, base.PeerMachineID, base.PeerFingerprint, base.State =
			started.PairID, input.TargetMachineID, input.ComparedFingerprint, "waiting_for_target"
	case "accept":
		if input.PairID == "" || input.ComparedFingerprint == "" {
			return base, errors.New("peer_pair_and_compared_fingerprint_required")
		}
		pair, err := api.GetPeerPair(ctx, l.identity.MachineCredential, input.PairID)
		if err != nil {
			return base, err
		}
		if pair.TargetMachineID != l.identity.MachineID || pair.SourceKeyFingerprint != input.ComparedFingerprint ||
			pair.TargetKeyFingerprint != key.Fingerprint() || pair.AcceptedAt != nil || pair.RevokedAt != nil {
			return base, errors.New("peer_pending_pair_mismatch")
		}
		private, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return base, err
		}
		transcript := peerTranscript(pair)
		transcript.TargetEncryptionKey = base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
		transcript.TargetAcceptSignature = base64.StdEncoding.EncodeToString(key.Sign(agenthandoff.AcceptBytes(transcript)))
		if err := agenthandoff.VerifyTranscript(transcript, l.identity.MachineID, input.ComparedFingerprint); err != nil {
			return base, err
		}
		accepted, err := api.AcceptPeerPair(ctx, l.identity.MachineCredential, input.PairID,
			adaptercloud.PeerPairAccept{SourceKeyFingerprint: input.ComparedFingerprint,
				TargetEncryptionKey:   transcript.TargetEncryptionKey,
				TargetAcceptSignature: transcript.TargetAcceptSignature})
		if err != nil {
			return base, err
		}
		if err := pinPeerPair(ctx, st, accepted, private.Bytes(), l.identity, input.ComparedFingerprint); err != nil {
			_ = api.RevokePeerPair(ctx, l.identity.MachineCredential, input.PairID)
			return base, err
		}
		base.PairID, base.PeerMachineID, base.PeerFingerprint, base.State =
			input.PairID, pair.SourceMachineID, input.ComparedFingerprint, "active_target"
	case "sync":
		pending, err := st.Pair(ctx, input.PairID)
		if err != nil {
			return base, err
		}
		if pending.SourceMachineID != l.identity.MachineID || pending.TargetFingerprint != input.ComparedFingerprint ||
			!pending.RevokedAt.IsZero() {
			return base, errors.New("peer_pending_pair_mismatch")
		}
		accepted, err := api.GetPeerPair(ctx, l.identity.MachineCredential, input.PairID)
		if err != nil {
			return base, err
		}
		if err := pinPeerPair(ctx, st, accepted, pending.LocalPrivateKey, l.identity, input.ComparedFingerprint); err != nil {
			return base, err
		}
		base.PairID, base.PeerMachineID, base.PeerFingerprint, base.State =
			input.PairID, pending.TargetMachineID, input.ComparedFingerprint, "active_source"
	case "grant":
		pair, err := st.Pair(ctx, input.PairID)
		if err != nil {
			return base, err
		}
		if pair.TargetMachineID != l.identity.MachineID || pair.TargetSignature == "" ||
			!pair.RevokedAt.IsZero() || !time.Now().Before(pair.ExpiresAt) {
			return base, errors.New("peer_target_pair_inactive")
		}
		if len(input.Scopes) == 0 || len(input.Scopes) > 2 ||
			input.SourceSessionID == "" || input.TargetSessionID == "" ||
			input.SourceExecutionGeneration == "" || input.TargetExecutionGeneration == "" {
			return base, errors.New("peer_grant_scope_and_target_required")
		}
		for _, scope := range input.Scopes {
			if scope != "message" && scope != "handoff" {
				return base, errors.New("peer_grant_scope_invalid")
			}
		}
		if l.opts.PeerAdmitTarget == nil || l.opts.PeerAdmitTarget(ctx, l.identity.MachineID,
			input.TargetSessionID, input.TargetExecutionGeneration) != nil {
			return base, errors.New("peer_target_execution_unverified")
		}
		created, err := api.CreatePeerGrant(ctx, l.identity.MachineCredential, adaptercloud.PeerGrantCreate{
			PairID: pair.ID, SourceSessionID: input.SourceSessionID,
			SourceExecutionGeneration: input.SourceExecutionGeneration, TargetSessionID: input.TargetSessionID,
			TargetExecutionGeneration: input.TargetExecutionGeneration, Scopes: input.Scopes,
			ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		if err != nil {
			return base, err
		}
		if err := pinPeerGrant(ctx, st, created); err != nil {
			_ = api.RevokePeerGrant(ctx, l.identity.MachineCredential, created.GrantID)
			return base, err
		}
		base.PairID, base.GrantID, base.PeerMachineID, base.PeerFingerprint, base.State =
			pair.ID, created.GrantID, pair.SourceMachineID, pair.SourceFingerprint, "grant_active_target"
	case "grant_sync":
		grant, err := api.GetPeerGrant(ctx, l.identity.MachineCredential, input.GrantID)
		if err != nil {
			return base, err
		}
		if err := pinPeerGrant(ctx, st, grant); err != nil {
			return base, err
		}
		base.PairID, base.GrantID, base.State = grant.PairID, grant.GrantID, "grant_active_source"
	case "revoke_pair":
		if err := st.RevokePair(ctx, input.PairID, time.Now()); err != nil {
			return base, err
		}
		if err := api.RevokePeerPair(ctx, l.identity.MachineCredential, input.PairID); err != nil {
			return base, err
		}
		base.PairID, base.State = input.PairID, "revoked"
	case "revoke_grant":
		if err := st.RevokeGrant(ctx, input.GrantID, time.Now()); err != nil {
			return base, err
		}
		if err := api.RevokePeerGrant(ctx, l.identity.MachineCredential, input.GrantID); err != nil {
			return base, err
		}
		base.GrantID, base.State = input.GrantID, "revoked"
	default:
		return base, errors.New("peer_control_action_unknown")
	}
	return base, nil
}

// peerControlStatus returns a complete bounded view of this machine's local
// pins. An absent or corrupt row never becomes a claimed active grant. Cloud
// revocation is still checked separately at every send and receive admission.
func peerControlStatus(ctx context.Context, st *peerstore.Store, base PeerControlResult,
	accountID string, now time.Time) (PeerControlResult, error) {
	allPairs, err := st.ListPairs(ctx)
	if err != nil {
		return base, err
	}
	allGrants, err := st.ListGrants(ctx)
	if err != nil {
		return base, err
	}
	pairs := make([]PeerPairStatus, 0)
	grants := make([]PeerGrantStatus, 0)
	active := make(map[string]peerstore.Pair)
	for _, pair := range allPairs {
		if pair.AccountID != accountID || !pair.RevokedAt.IsZero() || !now.Before(pair.ExpiresAt) {
			continue
		}
		localIsSource := pair.SourceMachineID == base.LocalMachineID
		if !localIsSource && pair.TargetMachineID != base.LocalMachineID {
			continue
		}
		localFingerprint := pair.TargetFingerprint
		peerFingerprint := pair.SourceFingerprint
		if localIsSource {
			localFingerprint, peerFingerprint = pair.SourceFingerprint, pair.TargetFingerprint
		}
		if localFingerprint != base.LocalFingerprint || peerFingerprint == "" {
			continue
		}
		state := "active"
		if pair.TargetSignature == "" {
			if !localIsSource || !pendingPairValid(pair) {
				return base, errors.New("peer_local_pair_invalid")
			}
			state = "waiting_for_target"
		} else {
			transcript := agenthandoff.PairTranscript{PairID: pair.ID,
				SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
				SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
				SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
				SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey,
				SourceOfferSignature: pair.SourceSignature, TargetAcceptSignature: pair.TargetSignature}
			private, privateErr := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
			if privateErr != nil || agenthandoff.VerifyTranscript(transcript, base.LocalMachineID, peerFingerprint) != nil {
				return base, errors.New("peer_local_pair_invalid")
			}
			if _, _, err := agenthandoff.PairKey(pairKeys(pair), private, localIsSource); err != nil {
				return base, errors.New("peer_local_pair_invalid")
			}
			active[pair.ID] = pair
		}
		pairs = append(pairs, PeerPairStatus{PairID: pair.ID,
			SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
			SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
			State: state, ExpiresAt: pair.ExpiresAt})
	}
	for _, grant := range allGrants {
		if !grant.RevokedAt.IsZero() || !now.Before(grant.ExpiresAt) {
			continue
		}
		pair, found := active[grant.PairID]
		if !found {
			continue
		}
		if grant.Source.MachineID != pair.SourceMachineID || grant.Target.MachineID != pair.TargetMachineID ||
			grant.SourceKeyFingerprint != pair.SourceFingerprint || grant.ExpiresAt.After(pair.ExpiresAt) ||
			(!grant.AllowMessage && !grant.AllowHandoff) {
			return base, errors.New("peer_local_grant_invalid")
		}
		scopes := make([]string, 0, 2)
		if grant.AllowMessage {
			scopes = append(scopes, "message")
		}
		if grant.AllowHandoff {
			scopes = append(scopes, "handoff")
		}
		grants = append(grants, PeerGrantStatus{GrantID: grant.ID, PairID: grant.PairID,
			Source: grant.Source, Target: grant.Target, Scopes: scopes, ExpiresAt: grant.ExpiresAt})
	}
	base.State, base.Pairs, base.Grants = "identity_read", &pairs, &grants
	return base, nil
}

func pendingPairValid(pair peerstore.Pair) bool {
	if pair.SourcePublicKey == "" || pair.SourceSignature == "" || len(pair.LocalPrivateKey) != 32 ||
		pair.TargetPublicKey != "" || pair.TargetEncryptionKey != "" {
		return false
	}
	signer, err := base64.StdEncoding.Strict().DecodeString(pair.SourcePublicKey)
	if err != nil || len(signer) != ed25519.PublicKeySize ||
		domaincloud.Fingerprint(ed25519.PublicKey(signer)) != pair.SourceFingerprint {
		return false
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil || base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()) != pair.SourceEncryptionKey {
		return false
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(pair.SourceSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(signer), agenthandoff.OfferBytes(agenthandoff.PairTranscript{
		SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey,
	}), signature)
}

func peerTranscript(pair adaptercloud.PeerPair) agenthandoff.PairTranscript {
	t := agenthandoff.PairTranscript{PairID: pair.PairID,
		SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceKeyFingerprint, TargetFingerprint: pair.TargetKeyFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, SourceOfferSignature: pair.SourceOfferSignature}
	if pair.TargetEncryptionKey != nil {
		t.TargetEncryptionKey = *pair.TargetEncryptionKey
	}
	if pair.TargetAcceptSignature != nil {
		t.TargetAcceptSignature = *pair.TargetAcceptSignature
	}
	return t
}

func pinPeerPair(ctx context.Context, st *peerstore.Store, pair adaptercloud.PeerPair,
	private []byte, identity adaptercloud.Identity, compared string) error {
	if pair.PairID == "" || pair.AcceptedAt == nil || pair.RevokedAt != nil ||
		pair.TargetEncryptionKey == nil || pair.TargetAcceptSignature == nil {
		return errors.New("peer_api_pair_inactive")
	}
	expires, err := time.Parse(time.RFC3339Nano, pair.ExpiresAt)
	if err != nil {
		return err
	}
	return st.PinPair(ctx, peerstore.Pair{ID: pair.PairID, AccountID: identity.AccountID,
		SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceKeyFingerprint, TargetFingerprint: pair.TargetKeyFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: *pair.TargetEncryptionKey,
		SourceSignature: pair.SourceOfferSignature, TargetSignature: *pair.TargetAcceptSignature,
		LocalPrivateKey: private, ExpiresAt: expires}, identity.MachineID, compared)
}

func pinPeerGrant(ctx context.Context, st *peerstore.Store, grant adaptercloud.PeerGrant) error {
	if grant.RevokedAt != nil || grant.GrantID == "" {
		return errors.New("peer_api_grant_inactive")
	}
	expires, err := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
	if err != nil {
		return err
	}
	row := peerstore.Grant{ID: grant.GrantID, PairID: grant.PairID,
		Source: grant.Source, Target: grant.Target,
		SourceKeyFingerprint: grant.SourceKeyFingerprint, ExpiresAt: expires}
	for _, scope := range grant.Scopes {
		switch scope {
		case "message":
			row.AllowMessage = true
		case "handoff":
			row.AllowHandoff = true
		default:
			return errors.New("peer_api_scope_unknown")
		}
	}
	return st.PinGrant(ctx, row)
}
