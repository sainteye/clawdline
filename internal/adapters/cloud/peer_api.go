package cloud

// The machine-to-machine peer control plane is separate from browser pairing.
// These methods only carry machine-authenticated metadata; no plaintext Agent
// message or handoff body is sent to the API.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

type PeerPair struct {
	PairID                string  `json:"pair_id"`
	SourceMachineID       string  `json:"source_machine_id"`
	TargetMachineID       string  `json:"target_machine_id"`
	SourcePublicKey       string  `json:"source_public_key"`
	TargetPublicKey       string  `json:"target_public_key"`
	SourceKeyFingerprint  string  `json:"source_key_fingerprint"`
	TargetKeyFingerprint  string  `json:"target_key_fingerprint"`
	SourceEncryptionKey   string  `json:"source_encryption_key"`
	TargetEncryptionKey   *string `json:"target_encryption_key"`
	SourceOfferSignature  string  `json:"source_offer_signature"`
	TargetAcceptSignature *string `json:"target_accept_signature"`
	AcceptedAt            *string `json:"accepted_at"`
	RevokedAt             *string `json:"revoked_at"`
	ExpiresAt             string  `json:"expires_at"`
}

type PeerPairStart struct {
	TargetMachineID      string `json:"target_machine_id"`
	TargetKeyFingerprint string `json:"target_key_fingerprint"`
	SourceEncryptionKey  string `json:"source_encryption_key"`
	SourceOfferSignature string `json:"source_offer_signature"`
}

type PeerPairAccept struct {
	SourceKeyFingerprint  string `json:"source_key_fingerprint"`
	TargetEncryptionKey   string `json:"target_encryption_key"`
	TargetAcceptSignature string `json:"target_accept_signature"`
}

type PeerGrantCreate struct {
	PairID                    string   `json:"pair_id"`
	SourceSessionID           string   `json:"source_session_id"`
	SourceExecutionGeneration string   `json:"source_execution_generation"`
	TargetSessionID           string   `json:"target_session_id"`
	TargetExecutionGeneration string   `json:"target_execution_generation"`
	Scopes                    []string `json:"scopes"`
	ExpiresAt                 string   `json:"expires_at"`
}

type PeerGrant struct {
	GrantID              string                `json:"grant_id"`
	PairID               string                `json:"pair_id"`
	Source               agenthandoff.Endpoint `json:"source"`
	Target               agenthandoff.Endpoint `json:"target"`
	SourceKeyFingerprint string                `json:"source_key_fingerprint"`
	Scopes               []string              `json:"scopes"`
	CreatedAt            string                `json:"created_at"`
	ExpiresAt            string                `json:"expires_at"`
	RevokedAt            *string               `json:"revoked_at"`
}

type PeerAuthorization struct {
	Authorized           bool   `json:"authorized"`
	SourcePublicKey      string `json:"source_public_key"`
	SourceKeyFingerprint string `json:"source_key_fingerprint"`
	TargetMachineID      string `json:"target_machine_id"`
	GrantID              string `json:"grant_id"`
	ExpiresAt            string `json:"expires_at"`
}

func (c *AccountClient) StartPeerPair(ctx context.Context, credential string, input PeerPairStart) (PeerPair, error) {
	var out PeerPair
	err := c.post(ctx, "/v1/peer/pairs", credential, input, &out)
	return out, err
}

func (c *AccountClient) GetPeerPair(ctx context.Context, credential, id string) (PeerPair, error) {
	var out PeerPair
	err := c.getJSON(ctx, "/v1/peer/pairs/"+url.PathEscape(id), credential, &out)
	return out, err
}

func (c *AccountClient) AcceptPeerPair(ctx context.Context, credential, id string, input PeerPairAccept) (PeerPair, error) {
	var out PeerPair
	err := c.post(ctx, "/v1/peer/pairs/"+url.PathEscape(id)+"/accept", credential, input, &out)
	return out, err
}

func (c *AccountClient) RevokePeerPair(ctx context.Context, credential, id string) error {
	return c.deletePeer(ctx, "/v1/peer/pairs/"+url.PathEscape(id), credential)
}

func (c *AccountClient) CreatePeerGrant(ctx context.Context, credential string, input PeerGrantCreate) (PeerGrant, error) {
	var out PeerGrant
	err := c.post(ctx, "/v1/peer/grants", credential, input, &out)
	return out, err
}

func (c *AccountClient) GetPeerGrant(ctx context.Context, credential, id string) (PeerGrant, error) {
	var out PeerGrant
	err := c.getJSON(ctx, "/v1/peer/grants/"+url.PathEscape(id), credential, &out)
	return out, err
}

func (c *AccountClient) RevokePeerGrant(ctx context.Context, credential, id string) error {
	return c.deletePeer(ctx, "/v1/peer/grants/"+url.PathEscape(id), credential)
}

func (c *AccountClient) AuthorizePeer(ctx context.Context, credential string, request agenthandoff.Request, sourcePublicKey string) (PeerAuthorization, error) {
	var out PeerAuthorization
	err := c.post(ctx, "/v1/peer/authorize", credential, map[string]any{
		"request": request, "source_public_key": sourcePublicKey,
	}, &out)
	return out, err
}

func (c *AccountClient) deletePeer(ctx context.Context, path, credential string) error {
	ctx, cancel := context.WithTimeout(ctx, OpeningTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered %d: %s", path, resp.StatusCode, truncate(string(data), 200))
	}
	var answer struct {
		Revoked bool `json:"revoked"`
	}
	if json.Unmarshal(data, &answer) != nil || !answer.Revoked {
		return fmt.Errorf("%s answered without a revocation receipt", path)
	}
	return nil
}
