package botnetwork

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"time"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// AssistantPolicy is source-owned human management state. Saving a Bot's
// switches cannot enable this policy or install gateway trust.
type AssistantPolicy struct {
	AccountID        string            `json:"account_id"`
	MachineID        string            `json:"machine_id"`
	Version          int64             `json:"version"`
	GlobalOptIn      bool              `json:"global_opt_in"`
	GatewayKeyID     string            `json:"gateway_key_id"`
	GatewayPublicKey ed25519.PublicKey `json:"gateway_public_key"`
}

type AssistantReceipt struct {
	Call        wire.AssistantCall `json:"call"`
	PrincipalID string             `json:"principal_id"`
	Digest      string             `json:"digest"`
	State       string             `json:"state"`
	TurnID      string             `json:"turn_id,omitempty"`
	Reply       string             `json:"reply,omitempty"`
	Failure     *pa.Failure        `json:"failure,omitempty"`
	// Read records source publication, not assistant observation or acknowledgement.
	Read bool `json:"read"`
}

type AssistantResponse struct {
	Receipt *AssistantReceipt `json:"receipt,omitempty"`
	Bots    []Entry           `json:"bots,omitempty"`
}

type AssistantRepository interface {
	AssistantPolicy(context.Context) (AssistantPolicy, error)
	AssistantGrant(context.Context, string) (wire.AssistantGrant, error)
	ClaimAssistant(context.Context, wire.AssistantCall, wire.VerifiedAssistant) (AssistantReceipt, bool, error)
	AssistantReceipt(context.Context, wire.AssistantCall, wire.VerifiedAssistant, string) (AssistantReceipt, error)
	SaveAssistant(context.Context, AssistantReceipt) error
}

// AssistantHost is distinct from the Bot peer Host: an assistant has its own
// verified principal, never a fabricated Bot ID, Session or execution fence.
// Each implementation must recheck current target state at native effect and
// result boundaries and preserve the untrusted peer-work origin.
type AssistantHost interface {
	CheckAssistant(context.Context, wire.BotEndpoint, bool) (AssistantTargetState, error)
	DirectoryAssistant(context.Context, wire.BotEndpoint, []string) ([]Entry, error)
	SubmitAssistant(context.Context, wire.AssistantCall, wire.VerifiedAssistant, string, []byte) (string, error)
	ResultAssistant(context.Context, AssistantReceipt) (AssistantReceipt, error)
}

type AssistantTargetState struct {
	FeatureEnabled  bool
	SourceWritable  bool
	TargetCurrent   bool
	TargetReachable bool
}

type AssistantOptions struct {
	AccountID  string
	MachineID  string
	Repository AssistantRepository
	Host       AssistantHost
	Clock      func() time.Time
}

type AssistantService struct{ o AssistantOptions }

func NewAssistant(o AssistantOptions) (*AssistantService, error) {
	if o.AccountID == "" || o.MachineID == "" || o.Repository == nil || o.Host == nil || o.Clock == nil {
		return nil, failure(pa.Invalid, "assistant_network_options")
	}
	return &AssistantService{o: o}, nil
}

// Verify reads policy and the dedicated key/consent afresh. A human browser
// credential is never examined or accepted by this rail.
func (s *AssistantService) Verify(ctx context.Context, call wire.AssistantCall, body []byte) (wire.VerifiedAssistant, wire.AssistantGrant, error) {
	var zero wire.VerifiedAssistant
	p, err := s.o.Repository.AssistantPolicy(ctx)
	if err != nil {
		return zero, wire.AssistantGrant{}, wire.AssistantUnavailable
	}
	state, err := s.o.Host.CheckAssistant(ctx, call.Target, true)
	if err != nil {
		return zero, wire.AssistantGrant{}, err
	}
	// Re-read policy after target IO as well as at each effect/publication
	// boundary. Turning global consent off while that IO waits must win.
	p, err = s.o.Repository.AssistantPolicy(ctx)
	if err != nil || p.AccountID != s.o.AccountID || p.MachineID != s.o.MachineID ||
		p.GatewayKeyID == "" || len(p.GatewayPublicKey) != ed25519.PublicKeySize {
		return zero, wire.AssistantGrant{}, wire.AssistantUnavailable
	}
	if p.AccountID != s.o.AccountID || p.MachineID != s.o.MachineID ||
		p.GatewayKeyID == "" || len(p.GatewayPublicKey) != ed25519.PublicKeySize {
		return zero, wire.AssistantGrant{}, wire.AssistantUnavailable
	}
	g, err := s.o.Repository.AssistantGrant(ctx, call.GrantID)
	if err != nil {
		return zero, g, wire.AssistantUnavailable
	}
	if g.KeyID != p.GatewayKeyID || !bytes.Equal(g.PublicKey, p.GatewayPublicKey) {
		return zero, g, wire.AssistantUnverified
	}
	if call.Target.AccountID != s.o.AccountID || call.Target.MachineID != s.o.MachineID {
		return zero, g, wire.AssistantDenied
	}
	facts := wire.AssistantFacts{GatewayConfigured: true, GlobalOptIn: p.GlobalOptIn,
		GrantReadable: true, Grant: g, Now: s.o.Clock(), FeatureEnabled: state.FeatureEnabled,
		SourceWritable: state.SourceWritable, TargetCurrent: state.TargetCurrent, TargetReachable: state.TargetReachable}
	principal, err := wire.VerifyAssistantCall(call, body, facts)
	if err != nil {
		return zero, g, err
	}
	return principal, g, nil
}

// Receive is synchronous only through durable acceptance. Native execution is
// asynchronous; callers explicitly read a correlated receipt. No unknown effect
// is retried, and no source response claims the assistant observed its result.
func (s *AssistantService) Receive(ctx context.Context, call wire.AssistantCall, body []byte) (AssistantResponse, error) {
	p, g, err := s.Verify(ctx, call, body)
	if err != nil {
		return AssistantResponse{}, err
	}
	var out AssistantResponse
	switch call.Kind {
	case "directory":
		out.Bots, err = s.o.Host.DirectoryAssistant(ctx, call.Target, append([]string(nil), g.BotIDs...))
		if err == nil {
			filtered := out.Bots[:0]
			for _, entry := range out.Bots {
				if entry.Endpoint.AccountID != g.AccountID || entry.Endpoint.MachineID != g.MachineID || entry.Endpoint.ProjectID != g.ProjectID {
					continue
				}
				for _, id := range g.BotIDs {
					if entry.Endpoint.BotID == id {
						filtered = append(filtered, entry)
						break
					}
				}
			}
			out.Bots = filtered
		}
	case "message":
		var payload Payload
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF || payload.Text == "" || len(payload.Text) > pa.MaxCollaborationRequestBytes {
			return out, wire.AssistantInvalid
		}
		var receipt AssistantReceipt
		var fresh bool
		receipt, fresh, err = s.o.Repository.ClaimAssistant(ctx, call, p)
		if err != nil {
			return out, err
		}
		if fresh {
			if _, _, err = s.Verify(ctx, call, body); err == nil {
				receipt.TurnID, err = s.o.Host.SubmitAssistant(ctx, call, p, payload.Text, append([]byte(nil), body...))
				if err == nil {
					receipt.State = "accepted"
				} else {
					receipt.State, receipt.Failure = "unknown", assistantFailure(err)
				}
			} else {
				receipt.State, receipt.Failure = "refused", assistantFailure(err)
			}
			if saveErr := s.o.Repository.SaveAssistant(ctx, receipt); saveErr != nil {
				return out, saveErr
			}
		}
		out.Receipt = &receipt
	case "read":
		var receipt AssistantReceipt
		receipt, err = s.o.Repository.AssistantReceipt(ctx, call, p, call.CorrelationID)
		if err == nil {
			receipt, err = s.o.Host.ResultAssistant(ctx, receipt)
		}
		if err == nil {
			if _, _, err = s.Verify(ctx, call, body); err != nil {
				return out, err
			}
			receipt.Read = true
			err = s.o.Repository.SaveAssistant(ctx, receipt)
		}
		out.Receipt = &receipt
	default:
		return out, wire.AssistantInvalid
	}
	if err != nil {
		return AssistantResponse{}, err
	}
	if _, _, err = s.Verify(ctx, call, body); err != nil {
		return AssistantResponse{}, err
	}
	return out, nil
}

// AssistantReceiptIdentity isolates callers by the authenticated principal and
// grant. Generations/epoch remain in the digest so ID reuse conflicts.
func AssistantReceiptIdentity(call wire.AssistantCall, principalID string) (actor, id, requestDigest string) {
	lane, _ := json.Marshal([]string{call.Target.AccountID, call.Target.MachineID, call.Target.ProjectID,
		call.Target.BotID, principalID, call.GrantID})
	input, _ := wire.AssistantSigningInput(call)
	return string(lane), call.RequestID, Digest(input)
}

func assistantFailure(err error) *pa.Failure {
	if refusal, ok := err.(wire.Refusal); ok {
		code := pa.ScopeDenied
		switch refusal {
		case wire.AssistantUnavailable:
			code = pa.Unsupported
		case wire.AssistantInvalid:
			code = pa.Invalid
		case wire.AssistantUnverified:
			code = pa.AuthRejected
		case wire.AssistantRevoked:
			code = pa.Revoked
		case wire.AssistantExpired:
			code = pa.Expired
		case wire.AssistantStale:
			code = pa.StaleGeneration
		}
		return &pa.Failure{Code: code, Field: string(refusal)}
	}
	return asFailure(err)
}
