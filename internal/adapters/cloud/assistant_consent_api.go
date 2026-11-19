package cloud

import (
	"context"
	"net/http"
	"net/url"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type AssistantConsent struct {
	GrantID     string           `json:"grant_id"`
	AccountID   string           `json:"account_id"`
	PrincipalID string           `json:"principal_id"`
	Target      wire.BotEndpoint `json:"target"`
	Kinds       []string         `json:"kinds"`
	Epoch       int64            `json:"grant_epoch"`
	IssuedAt    int64            `json:"issued_at"`
	ExpiresAt   int64            `json:"expires_at"`
	RevokedAt   *int64           `json:"revoked_at"`
}

// AssistantConsent authenticates as the source machine and rejects a response
// outside its exact account, machine and grant namespace.
func (c *BotNetworkClient) AssistantConsent(ctx context.Context, id string) (AssistantConsent, error) {
	var row AssistantConsent
	if !botSegment(id) {
		return row, botFail(pa.Invalid, "assistant_consent")
	}
	err := c.request(ctx, http.MethodGet, "/v1/bot-assistant/source-consents/"+url.PathEscape(id), nil, &row)
	if err != nil {
		return AssistantConsent{}, err
	}
	if row.GrantID != id || row.AccountID != c.Identity.AccountID || row.Target.AccountID != row.AccountID ||
		row.Target.MachineID != c.Identity.MachineID || !botCapture(row.Target) || !botSegment(row.PrincipalID) ||
		row.Epoch <= 0 || row.IssuedAt <= 0 || row.ExpiresAt <= row.IssuedAt || len(row.Kinds) == 0 {
		return AssistantConsent{}, botFail(pa.ScopeDenied, "assistant_consent")
	}
	seen := make(map[string]bool, len(row.Kinds))
	for _, kind := range row.Kinds {
		if seen[kind] || kind != "directory" && kind != "message" && kind != "read" {
			return AssistantConsent{}, botFail(pa.Invalid, "assistant_consent")
		}
		seen[kind] = true
	}
	return row, nil
}
