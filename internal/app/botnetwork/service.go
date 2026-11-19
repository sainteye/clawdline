// Package botnetwork owns the Bot-only network rail. Machine pairing proves a
// cryptographic peer; a separate durable Bot grant authorizes its work.
package botnetwork

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Grant struct {
	ID                  string              `json:"id"`
	AccountID           string              `json:"account_id"`
	Epoch               int64               `json:"epoch"`
	Source              wire.BotEndpoint    `json:"source"`
	Target              wire.BotEndpoint    `json:"target"`
	Pair                wire.PairTranscript `json:"pair"`
	ComparedFingerprint string              `json:"compared_fingerprint"`
	ExpiresAt           time.Time           `json:"expires_at"`
	Revoked             bool                `json:"revoked"`
}

type Receipt struct {
	Header  wire.BotHeader `json:"header"`
	Digest  string         `json:"digest"`
	State   string         `json:"state"`
	TurnID  string         `json:"turn_id,omitempty"`
	Reply   string         `json:"reply,omitempty"`
	Failure *pa.Failure    `json:"failure,omitempty"`
	// Read is a transport publication fact, never an Agent-observed fact.
	Read bool `json:"read"`
}

type Repository interface {
	Outgoing(context.Context, wire.BotEndpoint) ([]Grant, error)
	Grant(context.Context, string) (Grant, error)
	Claim(context.Context, wire.BotHeader) (Receipt, bool, error)
	Receipt(context.Context, wire.BotHeader, string) (Receipt, error)
	Save(context.Context, Receipt) error
}

type Host interface {
	Check(context.Context, wire.BotEndpoint, bool) error
	Directory(context.Context, wire.BotEndpoint) ([]Entry, error)
	Submit(context.Context, wire.BotHeader, string) (string, error)
	Result(context.Context, Receipt) (Receipt, error)
}
type Entry struct {
	Endpoint wire.BotEndpoint `json:"endpoint"`
	Name     string           `json:"name"`
}
type Payload struct {
	Text string `json:"text,omitempty"`
}
type Response struct {
	Routes  []Route     `json:"routes,omitempty"`
	Receipt *Receipt    `json:"receipt,omitempty"`
	Bots    []Entry     `json:"bots,omitempty"`
	Failure *pa.Failure `json:"failure,omitempty"`
}
type Options struct {
	// Authority performs the independently configured account/pair decision for
	// this exact header. Production supplies the live private service adapter;
	// isolated source fixtures use explicitly provisioned durable local consent.
	Authority            BotAuthority
	AccountID, MachineID string
	Private              *ecdh.PrivateKey
	// PrivateFor resolves each original pair's existing local key custody.
	PrivateFor func(context.Context, string) (*ecdh.PrivateKey, error)
	Signer     ed25519.PrivateKey
	Repository Repository
	Host       Host
	Clock      func() time.Time
}
type Service struct{ o Options }
type BotAuthority interface {
	CheckBotAuthority(context.Context, wire.BotHeader) error
}

func (s *Service) AccountID() string { return s.o.AccountID }
func (s *Service) CheckRequest(ctx context.Context, h wire.BotHeader) error {
	if _, e := s.authorize(ctx, h); e != nil {
		return e
	}
	return s.o.Host.Check(ctx, h.Target, true)
}

// CheckEnvelope authenticates transport delivery without claiming admission or
// performing native work. Replies keep their original reversed captures.
func (s *Service) CheckEnvelope(ctx context.Context, e wire.BotEnvelope) error {
	g, err := s.o.Repository.Grant(ctx, e.Header.AuthorityID)
	if err != nil {
		return err
	}
	h := e.Header
	if h.Target.MachineID != s.o.MachineID || g.Revoked || g.Epoch != h.AuthorityEpoch || !s.o.Clock().Before(g.ExpiresAt) {
		return failure(pa.Revoked, "bot_network_delivery")
	}
	a, b := h.Source, h.Target
	if h.Kind == "reply" {
		a, b = b, a
	}
	if !sameIdentity(a, g.Source) || !sameIdentity(b, g.Target) || h.PairID != g.Pair.PairID || g.AccountID != s.o.AccountID || wire.VerifyTranscript(g.Pair, s.o.MachineID, g.ComparedFingerprint) != nil {
		return failure(pa.ScopeDenied, "bot_network_delivery")
	}
	signer, err := pairSigner(g, h.Source.MachineID)
	if err != nil {
		return err
	}
	private, err := s.private(ctx, h.PairID)
	if err != nil {
		return err
	}
	if _, err = wire.OpenBot(e, pairKeys(g), private, signer); err != nil {
		return failure(pa.ScopeDenied, "bot_network_delivery_wire")
	}
	return s.o.Authority.CheckBotAuthority(ctx, h)
}

func failure(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }
func New(o Options) (*Service, error) {
	if o.AccountID == "" || o.MachineID == "" || o.Private == nil && o.PrivateFor == nil || len(o.Signer) != ed25519.PrivateKeySize || o.Repository == nil || o.Host == nil || o.Clock == nil || o.Authority == nil {
		return nil, failure(pa.Invalid, "bot_network_options")
	}
	return &Service{o: o}, nil
}
func (s *Service) private(ctx context.Context, pair string) (*ecdh.PrivateKey, error) {
	if s.o.PrivateFor != nil {
		return s.o.PrivateFor(ctx, pair)
	}
	return s.o.Private, nil
}
func Digest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func pairKeys(g Grant) wire.PairKeys {
	return wire.PairKeys{PairID: g.Pair.PairID, SourceMachineID: g.Pair.SourceMachineID, TargetMachineID: g.Pair.TargetMachineID, SourceEncryptionKey: g.Pair.SourceEncryptionKey, TargetEncryptionKey: g.Pair.TargetEncryptionKey}
}
func sameIdentity(a, b wire.BotEndpoint) bool {
	return a.AccountID == b.AccountID && a.MachineID == b.MachineID && a.ProjectID == b.ProjectID && a.BotID == b.BotID && a.ConversationID == b.ConversationID
}

// authorize is repeated before admission, effects and publication. A grant is a
// locally human-provisioned record; no network method creates or updates it.
func (s *Service) authorize(ctx context.Context, h wire.BotHeader) (Grant, error) {
	g, e := s.o.Repository.Grant(ctx, h.AuthorityID)
	if e != nil {
		return g, e
	}
	if g.Revoked || g.Epoch != h.AuthorityEpoch {
		return g, failure(pa.Revoked, "bot_network_grant")
	}
	if !s.o.Clock().Before(g.ExpiresAt) {
		return g, failure(pa.Expired, "bot_network_grant")
	}
	if h.Source.AccountID != s.o.AccountID || g.AccountID != s.o.AccountID || h.Target.AccountID != s.o.AccountID || h.PairID != g.Pair.PairID || !sameIdentity(h.Source, g.Source) || !sameIdentity(h.Target, g.Target) {
		return g, failure(pa.ScopeDenied, "bot_network_grant")
	}
	if e = wire.VerifyTranscript(g.Pair, s.o.MachineID, g.ComparedFingerprint); e != nil {
		return g, failure(pa.Unpaired, "bot_network_pair")
	}
	if e = s.o.Authority.CheckBotAuthority(ctx, h); e != nil {
		return g, e
	}
	return g, nil
}

func (s *Service) Receive(ctx context.Context, envelope wire.BotEnvelope) (wire.BotEnvelope, error) {
	h := envelope.Header
	if h.Target.MachineID != s.o.MachineID {
		return wire.BotEnvelope{}, failure(pa.ScopeDenied, "bot_network_machine")
	}
	// Authenticate the pinned encrypted source before a production authority
	// adapter can spend an external authorization request on this header.
	g, e := s.o.Repository.Grant(ctx, h.AuthorityID)
	if e != nil {
		return wire.BotEnvelope{}, e
	}
	signer, e := pairSigner(g, h.Source.MachineID)
	if e != nil {
		return wire.BotEnvelope{}, e
	}
	private, e := s.private(ctx, h.PairID)
	if e != nil {
		return wire.BotEnvelope{}, e
	}
	body, e := wire.OpenBot(envelope, pairKeys(g), private, signer)
	if e != nil {
		return wire.BotEnvelope{}, failure(pa.ScopeDenied, "bot_network_wire")
	}
	if _, e = s.authorize(ctx, h); e != nil {
		return wire.BotEnvelope{}, e
	}
	var response Response
	response, e = s.dispatch(ctx, h, body)
	if e != nil {
		response = Response{Failure: asFailure(e)}
	}
	if _, e = s.authorize(ctx, h); e != nil {
		return wire.BotEnvelope{}, e
	}
	if e = s.o.Host.Check(ctx, h.Target, false); e != nil {
		return wire.BotEnvelope{}, e
	}
	reply, _ := json.Marshal(response)
	out := h
	out.Kind = "reply"
	out.RequestID = ResponseID(h.RequestID)
	out.CorrelationID = h.RequestID
	out.Source, out.Target = h.Target, h.Source
	out.BodyDigest = Digest(reply)
	return wire.SealBot(out, reply, pairKeys(g), private, s.o.Signer)
}

func (s *Service) dispatch(ctx context.Context, h wire.BotHeader, body []byte) (Response, error) {
	if e := s.o.Host.Check(ctx, h.Target, true); e != nil {
		return Response{}, e
	}
	if h.Kind == "directory" {
		bots, e := s.o.Host.Directory(ctx, h.Target)
		return Response{Bots: bots}, e
	}
	if h.Kind == "read" {
		r, e := s.o.Repository.Receipt(ctx, h, h.CorrelationID)
		if e != nil {
			return Response{}, e
		}
		r, e = s.o.Host.Result(ctx, r)
		if e != nil {
			return Response{}, e
		}
		if _, e = s.authorize(ctx, h); e != nil {
			return Response{}, e
		}
		r.Read = true
		if e = s.o.Repository.Save(ctx, r); e != nil {
			return Response{}, e
		}
		return Response{Receipt: &r}, nil
	}
	if h.Kind != "message" {
		return Response{}, failure(pa.Invalid, "bot_network_kind")
	}
	var p Payload
	if e := json.Unmarshal(body, &p); e != nil || p.Text == "" || len(p.Text) > pa.MaxCollaborationRequestBytes {
		return Response{}, failure(pa.Invalid, "bot_network_body")
	}
	r, fresh, e := s.o.Repository.Claim(ctx, h)
	if e != nil {
		return Response{}, e
	}
	if !fresh {
		return Response{Receipt: &r}, nil
	}
	// A claim precedes native admission. A crash after this point is unknown;
	// exact replay reads it and never starts another effect.
	if _, e = s.authorize(ctx, h); e == nil {
		e = s.o.Host.Check(ctx, h.Target, true)
	}
	if e != nil {
		r.State = "refused"
		r.Failure = asFailure(e)
	} else {
		r.TurnID, e = s.o.Host.Submit(ctx, h, p.Text)
		if e != nil {
			r.State = "unknown"
			r.Failure = asFailure(e)
			if r.Failure.Code == pa.StaleGeneration {
				r.State = "refused"
			}
		} else {
			r.State = "accepted"
		}
	}
	if e = s.o.Repository.Save(ctx, r); e != nil {
		return Response{}, e
	}
	return Response{Receipt: &r}, nil
}

func asFailure(e error) *pa.Failure {
	if f, ok := e.(*pa.Failure); ok {
		return f
	}
	return &pa.Failure{Code: pa.OutcomeUnknown, Field: "bot_network_source"}
}
