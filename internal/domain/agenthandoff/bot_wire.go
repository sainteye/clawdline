package agenthandoff

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"
)

// BotWireVersion is independent of the Session peer protocol. A paired machine
// is a cryptographic peer, not authorization to invoke any of its Bots.
const BotWireVersion = 1

// BotEndpoint names a stored identity and a captured conversation generation.
// Display names, Session IDs and filesystem paths have no routing role.
type BotEndpoint struct {
	AccountID      string `json:"account_id"`
	MachineID      string `json:"machine_id"`
	ProjectID      string `json:"project_id"`
	BotID          string `json:"bot_id"`
	ConversationID string `json:"conversation_id"`
	Generation     int64  `json:"generation"`
}

// BotHeader binds authority and correlation to encrypted content. AuthorityID
// names a dedicated Bot consent, never an existing Session grant. AuthorityEpoch
// is checked against current durable consent by admission at every boundary.
type BotHeader struct {
	Version        int         `json:"version"`
	RequestID      string      `json:"request_id"`
	CorrelationID  string      `json:"correlation_id"`
	Kind           string      `json:"kind"`
	Source         BotEndpoint `json:"source"`
	Target         BotEndpoint `json:"target"`
	PairID         string      `json:"pair_id"`
	AuthorityID    string      `json:"authority_id"`
	AuthorityEpoch int64       `json:"authority_epoch"`
	BodyDigest     string      `json:"body_digest"`
}

type BotEnvelope struct {
	Header     BotHeader `json:"header"`
	Ciphertext string    `json:"ciphertext"`
	Nonce      string    `json:"nonce"`
	KeyID      string    `json:"key_id"`
	Signature  string    `json:"signature"`
}

const (
	BotInvalidRequest Refusal = "bot_network_invalid_request"
	BotInvalidWire    Refusal = "bot_network_invalid_wire"
)

var botFrame = frameDomain{signatureLabel: "clawdline-bot-network-v1",
	aadLabel: "clawdline-bot-network-aad-v1", maxCiphertext: MaxBodyBytes + 16}

func botEndpointValid(e BotEndpoint, directory bool) bool {
	if !validSegment(e.AccountID) || !validSegment(e.MachineID) || !validSegment(e.ProjectID) {
		return false
	}
	if directory && e.BotID == "" && e.ConversationID == "" && e.Generation == 0 {
		return true
	}
	return validSegment(e.BotID) && validSegment(e.ConversationID) && e.Generation >= 0
}

// ValidateBot checks structure and content binding, not authorization. Only a
// directory target, or the source of its correlated reply, may omit Bot capture.
func ValidateBot(h BotHeader, body []byte) error {
	sum := sha256.Sum256(body)
	if h.Version != BotWireVersion || !validSegment(h.RequestID) ||
		!validSegment(h.PairID) || !validSegment(h.AuthorityID) || h.AuthorityEpoch <= 0 ||
		h.Source.AccountID != h.Target.AccountID || h.Source.MachineID == h.Target.MachineID ||
		len(body) == 0 || len(body) > MaxBodyBytes || !utf8.Valid(body) ||
		h.BodyDigest != hex.EncodeToString(sum[:]) {
		return BotInvalidRequest
	}
	if h.CorrelationID != "" && !validSegment(h.CorrelationID) {
		return BotInvalidRequest
	}
	switch h.Kind {
	case "directory", "message":
		if h.CorrelationID != "" {
			return BotInvalidRequest
		}
	case "reply", "read":
		if h.CorrelationID == "" || h.CorrelationID == h.RequestID {
			return BotInvalidRequest
		}
	default:
		return BotInvalidRequest
	}
	if !botEndpointValid(h.Source, h.Kind == "reply") || !botEndpointValid(h.Target, h.Kind == "directory") {
		return BotInvalidRequest
	}
	return nil
}

func botPairSide(h BotHeader, pair PairKeys) (bool, bool) {
	if h.PairID != pair.PairID {
		return false, false
	}
	if h.Source.MachineID == pair.SourceMachineID && h.Target.MachineID == pair.TargetMachineID {
		return true, true
	}
	return false, h.Source.MachineID == pair.TargetMachineID && h.Target.MachineID == pair.SourceMachineID
}

func (e BotEnvelope) frame() sealedFrame {
	return sealedFrame{Ciphertext: e.Ciphertext, Nonce: e.Nonce, KeyID: e.KeyID, Signature: e.Signature}
}

func BotSignatureInput(e BotEnvelope) ([]byte, error) {
	return botFrame.signatureInput(e.Header, e.frame())
}

// SealBot uses the existing X25519/HKDF pair primitive, with a Bot-only signature
// and AAD domain. A caller must independently check current Bot consent.
func SealBot(h BotHeader, body []byte, pair PairKeys, private *ecdh.PrivateKey, signer ed25519.PrivateKey) (BotEnvelope, error) {
	side, ok := botPairSide(h, pair)
	if !ok || ValidateBot(h, body) != nil || len(signer) != ed25519.PrivateKeySize {
		return BotEnvelope{}, BotInvalidWire
	}
	f, err := botFrame.seal(h, body, pair, private, side, signer)
	if err != nil {
		return BotEnvelope{}, BotInvalidWire
	}
	return BotEnvelope{Header: h, Ciphertext: f.Ciphertext, Nonce: f.Nonce, KeyID: f.KeyID, Signature: f.Signature}, nil
}

// OpenBot verifies only the pinned machine signature and encrypted wire. It does
// not turn a valid Session pair into a Bot execution capability.
func OpenBot(e BotEnvelope, pair PairKeys, private *ecdh.PrivateKey, pinnedSigner ed25519.PublicKey) ([]byte, error) {
	side, ok := botPairSide(e.Header, pair)
	if !ok || len(pinnedSigner) != ed25519.PublicKeySize ||
		len(e.Ciphertext) > ((MaxBodyBytes+16+2)/3)*4 {
		return nil, BotInvalidWire
	}
	body, err := botFrame.open(e.Header, e.frame(), pair, private, !side, pinnedSigner)
	if err != nil || ValidateBot(e.Header, body) != nil {
		return nil, BotInvalidWire
	}
	return body, nil
}

// BotReceiptIdentity keeps the same lane across changed captures and consent
// epochs, so changed-content retries conflict instead of executing again.
func BotReceiptIdentity(h BotHeader) (actor, key, requestDigest string) {
	lane, _ := json.Marshal([]string{h.Source.AccountID, h.Source.MachineID,
		h.Source.ProjectID, h.Source.BotID, h.Target.MachineID, h.Target.ProjectID, h.Target.BotID})
	encoded, _ := canonicalHeader(h)
	sum := sha256.Sum256(encoded)
	return string(lane), h.RequestID, hex.EncodeToString(sum[:])
}
