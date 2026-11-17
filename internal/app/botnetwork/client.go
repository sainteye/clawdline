package botnetwork

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Transport interface {
	Exchange(context.Context, wire.BotEnvelope) (wire.BotEnvelope, error)
}
type Call struct {
	RequestID, CorrelationID, Kind, AuthorityID string
	Source, Target                              wire.BotEndpoint
	Text                                        string
}

func ResponseID(request string) string { return Digest([]byte("bot-network-response-v1\n" + request)) }

func decodeSigner(raw string) (ed25519.PublicKey, error) {
	b, e := base64.StdEncoding.Strict().DecodeString(raw)
	if e != nil || len(b) != ed25519.PublicKeySize {
		return nil, failure(pa.Unpaired, "bot_network_signer")
	}
	return ed25519.PublicKey(b), nil
}

// pairSigner follows the actual sender, independently of which machine
// originally offered the pair. Bot grants may run in either direction.
func pairSigner(g Grant, machine string) (ed25519.PublicKey, error) {
	switch machine {
	case g.Pair.SourceMachineID:
		return decodeSigner(g.Pair.SourcePublicKey)
	case g.Pair.TargetMachineID:
		return decodeSigner(g.Pair.TargetPublicKey)
	default:
		return nil, failure(pa.Unpaired, "bot_network_signer_machine")
	}
}

// Call sends once. A transport failure is unknown and is never retried here.
// The caller must retain the original ID before explicit recovery.
func (s *Service) Call(ctx context.Context, q Call, t Transport) (Response, error) {
	if t == nil {
		return Response{}, failure(pa.Unsupported, "bot_network_transport")
	}
	g, e := s.o.Repository.Grant(ctx, q.AuthorityID)
	if e != nil {
		return Response{}, e
	}
	body, _ := json.Marshal(Payload{Text: q.Text})
	h := wire.BotHeader{Version: wire.BotWireVersion, RequestID: q.RequestID, CorrelationID: q.CorrelationID, Kind: q.Kind, Source: q.Source, Target: q.Target, PairID: g.Pair.PairID, AuthorityID: g.ID, AuthorityEpoch: g.Epoch, BodyDigest: Digest(body)}
	if _, e = s.authorize(ctx, h); e != nil {
		return Response{}, e
	}
	if h.Source.MachineID != s.o.MachineID {
		return Response{}, failure(pa.ScopeDenied, "bot_network_source")
	}
	if e = s.o.Host.Check(ctx, q.Source, true); e != nil {
		return Response{}, e
	}
	envelope, e := wire.SealBot(h, body, pairKeys(g), s.o.Private, s.o.Signer)
	if e != nil {
		return Response{}, e
	}
	reply, e := t.Exchange(ctx, envelope)
	if e != nil {
		if f, ok := e.(*pa.Failure); ok {
			return Response{}, f
		}
		return Response{}, failure(pa.ExecutionEffectUnknown, "bot_network_transport")
	}
	expected := h
	expected.Source, expected.Target = h.Target, h.Source
	expected.Kind = "reply"
	expected.RequestID = ResponseID(h.RequestID)
	expected.CorrelationID = h.RequestID
	expected.BodyDigest = reply.Header.BodyDigest
	if reply.Header != expected {
		return Response{}, failure(pa.ScopeDenied, "bot_network_reply_correlation")
	}
	signer, e := pairSigner(g, reply.Header.Source.MachineID)
	if e != nil {
		return Response{}, e
	}
	plain, e := wire.OpenBot(reply, pairKeys(g), s.o.Private, signer)
	if e != nil {
		return Response{}, failure(pa.ScopeDenied, "bot_network_reply_wire")
	}
	if _, e = s.authorize(ctx, h); e != nil {
		return Response{}, e
	}
	if e = s.o.Host.Check(ctx, q.Source, false); e != nil {
		return Response{}, e
	}
	var result Response
	if e = json.Unmarshal(plain, &result); e != nil {
		return Response{}, failure(pa.Invalid, "bot_network_reply")
	}
	if result.Failure != nil {
		return Response{}, result.Failure
	}
	return result, nil
}
