package antigravityacp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// OfferFence is a value snapshot. The caller must obtain current ownership from
// its live connection and durable pending receipt, not from a browser or history.
type OfferFence struct{ ConnectionGeneration, SessionID, Revision string }
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionOffer is immutable after decoding and safe for concurrent reads.
// The zero value cannot prepare a response. No public field can mutate an offer.
type PermissionOffer struct {
	fence      OfferFence
	id         json.RawMessage
	toolCallID string
	toolCall   json.RawMessage
	options    []PermissionOption
}

func DecodePermission(message []byte, connectionGeneration, expectedSession string) (*PermissionOffer, error) {
	if connectionGeneration == "" || expectedSession == "" {
		return nil, refusal("invalid_owner")
	}
	m, err := messageObject(message)
	if err != nil {
		return nil, err
	}
	if !stringEquals(m["jsonrpc"], "2.0") || !stringEquals(m["method"], "session/request_permission") {
		return nil, refusal("invalid_permission")
	}
	if _, ok := m["result"]; ok {
		return nil, refusal("invalid_permission")
	}
	if _, ok := m["error"]; ok {
		return nil, refusal("invalid_permission")
	}
	if _, err := requestID(m["id"]); err != nil {
		return nil, err
	}
	p, err := object(m["params"])
	if err != nil {
		return nil, refusal("invalid_permission")
	}
	var session string
	if !isString(p["sessionId"], &session) || session == "" {
		return nil, refusal("invalid_permission")
	}
	if session != expectedSession {
		return nil, refusal("wrong_session")
	}
	tool, err := object(p["toolCall"])
	if err != nil {
		return nil, refusal("invalid_permission")
	}
	var toolID string
	if !isString(tool["toolCallId"], &toolID) || toolID == "" {
		return nil, refusal("invalid_permission")
	}
	var options []PermissionOption
	var rawOptions []json.RawMessage
	if json.Unmarshal(p["options"], &rawOptions) != nil {
		return nil, refusal("invalid_permission")
	}
	for _, raw := range rawOptions {
		if !exactFields(raw, "optionId", "name", "kind") {
			return nil, refusal("invalid_permission")
		}
	}
	if json.Unmarshal(p["options"], &options) != nil || len(options) == 0 {
		return nil, refusal("invalid_permission")
	}
	seen := map[string]bool{}
	for _, option := range options {
		if option.OptionID == "" || option.Name == "" || seen[option.OptionID] {
			return nil, refusal("invalid_permission")
		}
		seen[option.OptionID] = true
		switch option.Kind {
		case "allow_once", "allow_always", "reject_once", "reject_always":
		default:
			return nil, refusal("unsupported_option_kind")
		}
	}
	// Hash the complete original offer, including opaque tool data and metadata.
	// Any byte revision gets a new digest. Ownership is checked independently.
	digest := sha256.Sum256(message)
	return &PermissionOffer{
		fence: OfferFence{connectionGeneration, expectedSession, hex.EncodeToString(digest[:])},
		id:    append(json.RawMessage(nil), m["id"]...), toolCallID: toolID,
		toolCall: append(json.RawMessage(nil), p["toolCall"]...), options: options,
	}, nil
}
func (o *PermissionOffer) Fence() OfferFence {
	if o == nil {
		return OfferFence{}
	}
	return o.fence
}
func (o *PermissionOffer) RequestID() json.RawMessage {
	if o == nil {
		return nil
	}
	return append(json.RawMessage(nil), o.id...)
}
func (o *PermissionOffer) ToolCallID() string {
	if o == nil {
		return ""
	}
	return o.toolCallID
}
func (o *PermissionOffer) ToolCall() json.RawMessage {
	if o == nil {
		return nil
	}
	return append(json.RawMessage(nil), o.toolCall...)
}
func (o *PermissionOffer) Options() []PermissionOption {
	if o == nil {
		return nil
	}
	return append([]PermissionOption(nil), o.options...)
}

// PrepareSelected returns bytes only for an exact offered ID. Neither localized
// labels nor kinds select or synthesize IDs. A repeated call prepares fresh bytes;
// it is NOT an idempotency, durable reservation, or one-shot send guarantee.
func (o *PermissionOffer) PrepareSelected(current OfferFence, optionID string) ([]byte, error) {
	if err := o.checkFence(current); err != nil {
		return nil, err
	}
	for _, option := range o.options {
		if option.OptionID == optionID {
			return o.response("selected", optionID)
		}
	}
	return nil, refusal("unknown_option")
}
func (o *PermissionOffer) PrepareCancelled(current OfferFence) ([]byte, error) {
	if err := o.checkFence(current); err != nil {
		return nil, err
	}
	return o.response("cancelled", "")
}
func (o *PermissionOffer) checkFence(current OfferFence) error {
	if o == nil || o.fence.ConnectionGeneration == "" || o.fence.SessionID == "" || o.fence.Revision == "" || current != o.fence {
		return refusal("stale_offer")
	}
	return nil
}
func (o *PermissionOffer) response(outcome, optionID string) ([]byte, error) {
	type permissionOutcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId,omitempty"`
	}
	type permissionResult struct {
		Outcome permissionOutcome `json:"outcome"`
	}
	return json.Marshal(struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      json.RawMessage  `json:"id"`
		Result  permissionResult `json:"result"`
	}{"2.0", o.id, permissionResult{permissionOutcome{outcome, optionID}}})
}
