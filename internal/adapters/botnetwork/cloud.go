package botnetwork

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"time"

	cloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// CloudAuthority joins current machine identity/key custody, the pinned pair,
// and live dedicated API authority. No Session grant is consulted.
type CloudAuthority struct {
	Client  *cloud.BotNetworkClient
	Store   *Store
	Pairs   *peerstore.Store
	Signer  ed25519.PrivateKey
	Current func(context.Context) error
}

func pairTranscript(p peerstore.Pair) wire.PairTranscript {
	return wire.PairTranscript{PairID: p.ID, SourceMachineID: p.SourceMachineID, TargetMachineID: p.TargetMachineID, SourcePublicKey: p.SourcePublicKey, TargetPublicKey: p.TargetPublicKey, SourceFingerprint: p.SourceFingerprint, TargetFingerprint: p.TargetFingerprint, SourceEncryptionKey: p.SourceEncryptionKey, TargetEncryptionKey: p.TargetEncryptionKey, SourceOfferSignature: p.SourceSignature, TargetAcceptSignature: p.TargetSignature}
}
func (a *CloudAuthority) Pair(ctx context.Context, id string) (peerstore.Pair, error) {
	if a.Current == nil || a.Current(ctx) != nil || a.Client == nil || a.Pairs == nil {
		return peerstore.Pair{}, refuse(pa.Unsupported, "bot_cloud_current")
	}
	p, e := a.Pairs.Pair(ctx, id)
	if e != nil || p.AccountID != a.Client.Identity.AccountID || !p.RevokedAt.IsZero() || !a.Client.Clock().Before(p.ExpiresAt) {
		return p, refuse(pa.Unpaired, "bot_cloud_pair")
	}
	fp := p.TargetFingerprint
	pub := p.SourcePublicKey
	if a.Client.Identity.MachineID == p.TargetMachineID {
		fp = p.SourceFingerprint
		pub = p.TargetPublicKey
	}
	if len(a.Signer) != ed25519.PrivateKeySize || base64.StdEncoding.EncodeToString(a.Signer.Public().(ed25519.PublicKey)) != pub || wire.VerifyTranscript(pairTranscript(p), a.Client.Identity.MachineID, fp) != nil {
		return p, refuse(pa.Unpaired, "bot_cloud_pair_key")
	}
	return p, nil
}
func (a *CloudAuthority) Private(ctx context.Context, id string) (*ecdh.PrivateKey, error) {
	p, e := a.Pair(ctx, id)
	if e != nil {
		return nil, e
	}
	key, e := ecdh.X25519().NewPrivateKey(p.LocalPrivateKey)
	if e != nil {
		return nil, refuse(pa.Unpaired, "bot_cloud_pair_key")
	}
	keys := wire.PairKeys{PairID: p.ID, SourceMachineID: p.SourceMachineID, TargetMachineID: p.TargetMachineID, SourceEncryptionKey: p.SourceEncryptionKey, TargetEncryptionKey: p.TargetEncryptionKey}
	if _, _, e = wire.PairKey(keys, key, a.Client.Identity.MachineID == p.SourceMachineID); e != nil {
		return nil, refuse(pa.Unpaired, "bot_cloud_pair_key")
	}
	return key, nil
}
func (a *CloudAuthority) CheckBotAuthority(ctx context.Context, h wire.BotHeader) error {
	p, e := a.Pair(ctx, h.PairID)
	if e != nil {
		return e
	}
	g, e := a.Store.Grant(ctx, h.AuthorityID)
	if e != nil {
		return e
	}
	if g.Pair != pairTranscript(p) {
		return refuse(pa.Unpaired, "bot_cloud_pair_changed")
	}
	if e = a.Client.CheckBotAuthority(ctx, h); e != nil {
		return e
	}
	return a.Current(ctx)
}

// SyncConsent can only copy an already human-approved API record, matched to
// an independently pinned local pair. The authenticated source manager owns
// calling this port. Source-consent metadata never substitutes for authorize.
func (a *CloudAuthority) SyncConsent(ctx context.Context, id string) error {
	c, e := a.Client.Consent(ctx, id)
	if e != nil {
		return e
	}
	p, e := a.Pair(ctx, c.PairID)
	if e != nil {
		return e
	}
	if !(c.Source.MachineID == p.SourceMachineID && c.Target.MachineID == p.TargetMachineID || c.Source.MachineID == p.TargetMachineID && c.Target.MachineID == p.SourceMachineID) {
		return refuse(pa.Unpaired, "bot_cloud_consent_pair")
	}
	remote, e := a.Client.Pair(ctx, c.PairID)
	if e != nil {
		return e
	}
	t := pairTranscript(p)
	if remote.TargetEncryptionKey == nil || remote.TargetAcceptSignature == nil || remote.AcceptedAt == nil || remote.RevokedAt != nil || remote.SourceMachineID != p.SourceMachineID || remote.TargetMachineID != p.TargetMachineID || remote.SourcePublicKey != p.SourcePublicKey || remote.TargetPublicKey != p.TargetPublicKey || remote.SourceKeyFingerprint != p.SourceFingerprint || remote.TargetKeyFingerprint != p.TargetFingerprint || remote.SourceEncryptionKey != p.SourceEncryptionKey || *remote.TargetEncryptionKey != p.TargetEncryptionKey || remote.SourceOfferSignature != p.SourceSignature || *remote.TargetAcceptSignature != p.TargetSignature {
		return refuse(pa.Unpaired, "bot_cloud_pair_metadata")
	}
	exp, e := time.Parse(time.RFC3339, remote.ExpiresAt)
	if e != nil || !exp.Equal(p.ExpiresAt) || time.Unix(c.ExpiresAt, 0).After(exp) {
		return refuse(pa.Expired, "bot_cloud_pair_lifetime")
	}
	fp := p.TargetFingerprint
	if a.Client.Identity.MachineID == p.TargetMachineID {
		fp = p.SourceFingerprint
	}
	g := app.Grant{ID: c.ID, AccountID: c.AccountID, Epoch: c.Epoch, Source: c.Source, Target: c.Target, Pair: t, ComparedFingerprint: fp, ExpiresAt: time.Unix(c.ExpiresAt, 0).UTC(), Revoked: c.RevokedAt != nil}
	if old, e := a.Store.Grant(ctx, id); e == nil {
		// JSON's Z timestamp decodes as UTC, while time.Unix carries Local.
		// A process timezone must not turn the same expiry and epoch into a
		// different grant. Every other authority field remains exact.
		old.ExpiresAt = old.ExpiresAt.UTC()
		if old == g {
			return nil
		}
	}
	if e = a.Current(ctx); e != nil {
		return e
	}
	return a.Store.SyncGrant(ctx, g)
}
