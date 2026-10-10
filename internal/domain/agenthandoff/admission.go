// Package agenthandoff defines the receiver's fail-closed admission contract
// for cross-machine Agent messages and Session handoffs. It has no transport:
// a same-account Cloud roster entry is not a machine-to-machine grant.
package agenthandoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

type Kind string

const (
	Message Kind = "message"
	Handoff Kind = "handoff"
)

// Endpoint names one execution, not a row selected in a console.
type Endpoint struct {
	MachineID           string `json:"machine_id"`
	SessionID           string `json:"session_id"`
	ExecutionGeneration string `json:"execution_generation"`
}

// Request is the signed and encrypted payload's immutable routing header.
// RequestID is also the durable receipt's idempotency key. The body digest
// binds a resend to the same content without placing content in a receipt.
type Request struct {
	RequestID  string   `json:"request_id"`
	Kind       Kind     `json:"kind"`
	Source     Endpoint `json:"source"`
	Target     Endpoint `json:"target"`
	GrantID    string   `json:"grant_id"`
	BodyDigest string   `json:"body_digest"`
}

// Principal is supplied by a transport that verified a pinned peer machine
// key before decrypting the payload. A claimed machine ID inside the payload
// cannot create this value.
type Principal struct {
	MachineID      string
	KeyFingerprint string
	PairID         string
	MachinePeer    bool
}

// Grant is the receiver's current, locally stored authorization. It is
// specific to the source and target Sessions and one verified peer key.
// Revocation beats an older signed request and any account roster state.
type Grant struct {
	ID                 string
	PairID             string
	SourceMachineID    string
	SourceSessionID    string
	SourceGeneration   string
	TargetMachineID    string
	TargetSessionID    string
	TargetGeneration   string
	PeerKeyFingerprint string
	AllowMessage       bool
	AllowHandoff       bool
	Revoked            bool
	ExpiresAt          time.Time
}

// Facts must be freshly established at the receiver's point of effect.
// GrantReadable means the local grant store was read successfully; a zero
// Grant with that bit false is never interpreted as an empty grant list.
type Facts struct {
	LocalMachineID string
	Principal      Principal
	PairActive     bool
	// MachineAccess is a signed, locally pinned machine pair whose ID is the
	// request authority. It covers either kind for current Sessions on the pair.
	MachineAccess        bool
	GrantReadable        bool
	Grant                Grant
	PeerCapability       bool
	LocalCapability      bool
	MachineWritesAllowed bool
	TargetCurrent        bool
	Now                  time.Time
}

// ReceiptScope is separate from viewer-driven Cloud Session commands. The
// caller stores the returned actor, request ID and digest in the daemon's
// durable request_receipts table before any effect.
const ReceiptScope = "cloud.agent_handoff"

// MaxBodyBytes is the plaintext limit for one peer Agent message or handoff.
const MaxBodyBytes = 512 << 10

// ReceiptIdentity keeps a request ID in the same source/target Session lane
// across execution generations. A resend with a changed generation, grant,
// operation or content therefore conflicts with the original receipt rather
// than starting a second execution.
func ReceiptIdentity(request Request) (actor, key, requestDigest string) {
	actorBytes, _ := json.Marshal([]string{
		request.Source.MachineID, request.Source.SessionID,
		request.Target.MachineID, request.Target.SessionID,
	})
	requestBytes, _ := json.Marshal(request)
	sum := sha256.Sum256(requestBytes)
	return string(actorBytes), request.RequestID, hex.EncodeToString(sum[:])
}

// Refusal is stable protocol data. No text from a request becomes a code.
type Refusal string

func (r Refusal) Error() string { return string(r) }

const (
	InvalidRequest        Refusal = "handoff_invalid_request"
	SourceUnverified      Refusal = "handoff_source_unverified"
	WrongMachine          Refusal = "handoff_wrong_machine"
	GrantUnavailable      Refusal = "handoff_grant_unavailable"
	PairInactive          Refusal = "handoff_pair_inactive"
	GrantDenied           Refusal = "handoff_grant_denied"
	GrantRevoked          Refusal = "handoff_grant_revoked"
	GrantExpired          Refusal = "handoff_grant_expired"
	CapabilityUnavailable Refusal = "handoff_capability_unavailable"
	MachineWritesDisabled Refusal = "handoff_machine_writes_disabled"
	ExecutionUnverified   Refusal = "handoff_execution_unverified"
)

var generation = regexp.MustCompile(`^[0-9a-f]{32}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Admit checks all evidence the receiver must have before claiming the
// request receipt and applying an effect. A caller must also bind the receipt
// to the full request digest, re-read these facts at the final effect boundary,
// and refuse an orphaned receipt instead of executing it again.
func Admit(request Request, body []byte, facts Facts) error {
	if err := Validate(request, body); err != nil {
		return err
	}
	if !facts.Principal.MachinePeer || facts.Principal.MachineID == "" ||
		facts.Principal.KeyFingerprint == "" ||
		facts.Principal.MachineID != request.Source.MachineID {
		return SourceUnverified
	}
	if facts.LocalMachineID == "" || facts.LocalMachineID != request.Target.MachineID {
		return WrongMachine
	}
	if !facts.PairActive || facts.Principal.PairID == "" {
		return PairInactive
	}
	if facts.MachineAccess && request.GrantID != facts.Principal.PairID {
		return GrantDenied
	}
	if !facts.MachineAccess && !facts.GrantReadable {
		return GrantUnavailable
	}
	grant := facts.Grant
	if !facts.MachineAccess {
		if grant.Revoked {
			return GrantRevoked
		}
		if facts.Now.IsZero() || grant.ExpiresAt.IsZero() ||
			!facts.Now.Before(grant.ExpiresAt) {
			return GrantExpired
		}
		if grant.ID == "" || grant.ID != request.GrantID ||
			grant.PairID != facts.Principal.PairID ||
			grant.SourceMachineID != request.Source.MachineID ||
			grant.SourceSessionID != request.Source.SessionID ||
			grant.SourceGeneration != request.Source.ExecutionGeneration ||
			grant.TargetMachineID != request.Target.MachineID ||
			grant.TargetSessionID != request.Target.SessionID ||
			grant.TargetGeneration != request.Target.ExecutionGeneration ||
			grant.PeerKeyFingerprint != facts.Principal.KeyFingerprint ||
			(request.Kind == Message && !grant.AllowMessage) ||
			(request.Kind == Handoff && !grant.AllowHandoff) {
			return GrantDenied
		}
	}
	if !facts.PeerCapability || !facts.LocalCapability {
		return CapabilityUnavailable
	}
	if !facts.MachineWritesAllowed {
		return MachineWritesDisabled
	}
	if !facts.TargetCurrent {
		return ExecutionUnverified
	}
	return nil
}

// Validate checks the complete immutable header and plaintext binding
// before either source or target asks a revocable authority.
func Validate(request Request, body []byte) error {
	bodySum := sha256.Sum256(body)
	if !validSegment(request.RequestID) || !validSegment(request.GrantID) ||
		len(body) == 0 || len(body) > MaxBodyBytes || !utf8.Valid(body) || !digest.MatchString(request.BodyDigest) ||
		request.BodyDigest != hex.EncodeToString(bodySum[:]) ||
		!validEndpoint(request.Source) || !validEndpoint(request.Target) ||
		(request.Kind != Message && request.Kind != Handoff) {
		return InvalidRequest
	}
	return nil
}

func validEndpoint(endpoint Endpoint) bool {
	return validSegment(endpoint.MachineID) && validSegment(endpoint.SessionID) &&
		generation.MatchString(endpoint.ExecutionGeneration)
}

func validSegment(value string) bool {
	if len(value) == 0 || len(value) > cloud.MaxSegmentBytes {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '!' || value[i] > '~' || value[i] == '/' || value[i] == '|' {
			return false
		}
	}
	return true
}
