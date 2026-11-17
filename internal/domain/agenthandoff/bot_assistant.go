package agenthandoff

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"
	"unicode/utf8"
)

// AssistantCall is sent only on the dedicated assistant rail. Its claimed
// account and grant fields are signed inputs, not an authenticated principal.
type AssistantCall struct {
	Version       int         `json:"version"`
	RequestID     string      `json:"request_id"`
	CorrelationID string      `json:"correlation_id"`
	Kind          string      `json:"kind"`
	Target        BotEndpoint `json:"target"`
	GrantID       string      `json:"grant_id"`
	GrantEpoch    int64       `json:"grant_epoch"`
	KeyID         string      `json:"key_id"`
	BodyDigest    string      `json:"body_digest"`
	IssuedAt      int64       `json:"issued_at"`
	ExpiresAt     int64       `json:"expires_at"`
	Signature     string      `json:"signature"`
}

// AssistantGrant is read from the source's durable human-managed trust store.
// Neither an incoming JSON object, a machine roster nor a viewer bearer may
// create it. PublicKey is a dedicated gateway key, never a human login key.
type AssistantGrant struct {
	ID           string
	Epoch        int64
	KeyID        string
	PublicKey    ed25519.PublicKey
	PrincipalID  string
	AccountID    string
	MachineID    string
	ProjectID    string
	BotIDs       []string
	AllowList    bool
	AllowMessage bool
	AllowRead    bool
	Revoked      bool
	NotBefore    time.Time
	ExpiresAt    time.Time
}

// AssistantFacts must come from independent source state immediately before
// admission, effect and publication. GlobalOptIn is authenticated human consent.
// GatewayConfigured is false until an actual trusted gateway key is installed.
type AssistantFacts struct {
	GatewayConfigured bool
	GlobalOptIn       bool
	FeatureEnabled    bool
	SourceWritable    bool
	TargetCurrent     bool
	TargetReachable   bool
	GrantReadable     bool
	Now               time.Time
	Grant             AssistantGrant
}

const (
	AssistantUnavailable Refusal = "bot_assistant_unavailable"
	AssistantInvalid     Refusal = "bot_assistant_invalid_request"
	AssistantUnverified  Refusal = "bot_assistant_unverified"
	AssistantDenied      Refusal = "bot_assistant_denied"
	AssistantRevoked     Refusal = "bot_assistant_revoked"
	AssistantExpired     Refusal = "bot_assistant_expired"
	AssistantStale       Refusal = "bot_assistant_stale"
)

// VerifiedAssistant cannot be fabricated from transport JSON. The dispatcher
// must retain and reverify its original call against current durable facts.
type VerifiedAssistant struct {
	principalID string
	accountID   string
	grantID     string
	epoch       int64
}

func (p VerifiedAssistant) PrincipalID() string { return p.principalID }
func (p VerifiedAssistant) AccountID() string   { return p.accountID }
func (p VerifiedAssistant) GrantID() string     { return p.grantID }
func (p VerifiedAssistant) Epoch() int64        { return p.epoch }

// AssistantSigningInput excludes only the signature. The gateway uses the same
// canonical-JSON implementation as peer envelopes, under a separate domain.
func AssistantSigningInput(call AssistantCall) ([]byte, error) {
	call.Signature = ""
	encoded, err := canonicalHeader(call)
	if err != nil {
		return nil, AssistantInvalid
	}
	return append([]byte("clawdline-bot-assistant-call-v1\n"), encoded...), nil
}

// SignAssistantCall is used by a dedicated gateway; it grants no local authority.
func SignAssistantCall(call AssistantCall, signer ed25519.PrivateKey) (AssistantCall, error) {
	if len(signer) != ed25519.PrivateKeySize {
		return AssistantCall{}, AssistantInvalid
	}
	input, err := AssistantSigningInput(call)
	if err != nil {
		return AssistantCall{}, err
	}
	call.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, input))
	return call, nil
}

func validAssistantCall(call AssistantCall, body []byte) bool {
	sum := sha256.Sum256(body)
	if call.Version != BotWireVersion || !validSegment(call.RequestID) || !validSegment(call.GrantID) ||
		!validSegment(call.KeyID) || call.GrantEpoch <= 0 || !botEndpointValid(call.Target, false) ||
		len(body) == 0 || len(body) > MaxBodyBytes || !utf8.Valid(body) ||
		call.BodyDigest != hex.EncodeToString(sum[:]) || call.IssuedAt <= 0 || call.ExpiresAt <= call.IssuedAt {
		return false
	}
	switch call.Kind {
	case "directory", "message":
		return call.CorrelationID == ""
	case "read":
		return validSegment(call.CorrelationID) && call.CorrelationID != call.RequestID
	default:
		return false
	}
}

// VerifyAssistantCall derives principal identity only after verifying a
// dedicated pinned gateway key and fresh source-owned consent. It never accepts
// a caller-supplied principal object, browser bearer or unconditional callback.
func VerifyAssistantCall(call AssistantCall, body []byte, facts AssistantFacts) (VerifiedAssistant, error) {
	deny := func(err error) (VerifiedAssistant, error) { return VerifiedAssistant{}, err }
	if !facts.GatewayConfigured {
		return deny(AssistantUnavailable)
	}
	if !validAssistantCall(call, body) {
		return deny(AssistantInvalid)
	}
	if !facts.GrantReadable {
		return deny(AssistantUnavailable)
	}
	g := facts.Grant
	if g.ID != call.GrantID || g.KeyID != call.KeyID || !validSegment(g.PrincipalID) ||
		len(g.PublicKey) != ed25519.PublicKeySize {
		return deny(AssistantUnverified)
	}
	input, err := AssistantSigningInput(call)
	sig, decodeErr := decodeCanonical(call.Signature, ed25519.SignatureSize)
	if err != nil || decodeErr != nil || !ed25519.Verify(g.PublicKey, input, sig) {
		return deny(AssistantUnverified)
	}
	if g.Revoked {
		return deny(AssistantRevoked)
	}
	if facts.Now.IsZero() || g.NotBefore.IsZero() || g.ExpiresAt.IsZero() ||
		facts.Now.Before(g.NotBefore) || !facts.Now.Before(g.ExpiresAt) ||
		facts.Now.Unix() < call.IssuedAt || facts.Now.Unix() >= call.ExpiresAt ||
		time.Unix(call.IssuedAt, 0).Before(g.NotBefore) || time.Unix(call.ExpiresAt, 0).After(g.ExpiresAt) {
		return deny(AssistantExpired)
	}
	if g.Epoch != call.GrantEpoch || !facts.TargetCurrent {
		return deny(AssistantStale)
	}
	if !facts.GlobalOptIn || !facts.FeatureEnabled || !facts.SourceWritable || !facts.TargetReachable ||
		g.AccountID != call.Target.AccountID || g.MachineID != call.Target.MachineID ||
		g.ProjectID != call.Target.ProjectID ||
		(call.Kind == "directory" && !g.AllowList) || (call.Kind == "message" && !g.AllowMessage) ||
		(call.Kind == "read" && !g.AllowRead) {
		return deny(AssistantDenied)
	}
	allowed := false
	for _, id := range g.BotIDs {
		if id == call.Target.BotID {
			allowed = true
			break
		}
	}
	if !allowed {
		return deny(AssistantDenied)
	}
	return VerifiedAssistant{principalID: g.PrincipalID, accountID: g.AccountID, grantID: g.ID, epoch: g.Epoch}, nil
}
