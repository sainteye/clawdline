package projectassistantacp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// providerFailure keeps ACP's code and allowlisted category without retaining
// the native message. Like Codync's rpc_error/codex_kind, the first string field
// wins before validation; an invalid errorKind cannot fall through to category.
func providerFailure(r *rpcError) error {
	var data struct {
		ErrorKind      *string         `json:"errorKind"`
		Category       *string         `json:"category"`
		CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
		Details        string          `json:"details"`
	}
	// A malformed details field must not discard valid classification metadata.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(r.Data, &fields)
	if json.Unmarshal(fields["errorKind"], &data.ErrorKind) != nil {
		data.ErrorKind = nil
	}
	if json.Unmarshal(fields["category"], &data.Category) != nil {
		data.Category = nil
	}
	_ = json.Unmarshal(fields["details"], &data.Details)
	data.CodexErrorInfo = fields["codexErrorInfo"]
	kind := ""
	switch {
	case data.ErrorKind != nil:
		kind = *data.ErrorKind
	case data.Category != nil:
		kind = *data.Category
	default:
		kind = codexErrorKind(data.CodexErrorInfo)
	}
	if !validErrorKind(kind) {
		kind = ""
	}
	// Upstream checks the message and details, including string-valued data.
	// This text is inspected only inside a genuine RPC error, never chat output.
	if data.Details == "" {
		_ = json.Unmarshal(r.Data, &data.Details)
	}
	return &app.ProviderFailure{Reason: providerReason(r.Code, kind, r.Message+" "+data.Details), RPCCode: r.Code,
		Cause: refusal(pa.Unsupported, "rpc_"+strconv.Itoa(r.Code))}
}

// claudePromptFailure reads the pinned adapter's negotiated AIR sessionFailure
// response. Its quota policy uses limit/error with an empty action array;
// context/budget limits advertise new_session and rate limits advertise retry.
// An ordinary success or a chat message has no authority to report quota.
func claudePromptFailure(raw json.RawMessage, stopReason, output string) error {
	var response struct {
		Usage struct {
			OutputTokens *int64 `json:"outputTokens"`
		} `json:"usage"`
		Meta struct {
			JetBrains struct {
				AIR struct {
					Version int             `json:"version"`
					Failure json.RawMessage `json:"sessionFailure"`
				} `json:"air"`
			} `json:"jetbrains"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return refusal(pa.ExecutionEffectUnknown, "prompt_result")
	}
	air := response.Meta.JetBrains.AIR
	if len(air.Failure) == 0 {
		return nil
	}
	var failure struct {
		ID       string    `json:"id"`
		Revision int64     `json:"revision"`
		Category string    `json:"category"`
		Severity string    `json:"severity"`
		Actions  *[]string `json:"actions"`
	}
	if err := json.Unmarshal(air.Failure, &failure); err != nil || air.Version != 1 ||
		failure.ID == "" || failure.Revision <= 0 || failure.Severity != "error" || failure.Actions == nil {
		return refusal(pa.ExecutionEffectUnknown, "prompt_result")
	}
	if failure.Category != "limit" || len(*failure.Actions) != 0 {
		return refusal(pa.ExecutionEffectUnknown, "prompt_result")
	}
	quota := &app.ProviderFailure{Reason: app.ProviderUsageExhausted, Cause: refusal(pa.Unsupported, "prompt_quota")}
	noOutput := stopReason == "end_turn" && output == "" && response.Usage.OutputTokens != nil && *response.Usage.OutputTokens == 0
	return &app.TerminalQuotaFailure{Failure: quota, NoOutput: noOutput}
}

func validErrorKind(kind string) bool {
	if kind == "" || len(kind) > capacity.MaximumBotErrorKindBytes {
		return false
	}
	for _, b := range []byte(kind) {
		if b != '_' && (b < 'a' || b > 'z') {
			return false
		}
	}
	return true
}

func codexErrorKind(info json.RawMessage) string {
	var name string
	if json.Unmarshal(info, &name) != nil {
		var object map[string]json.RawMessage
		if json.Unmarshal(info, &object) != nil || len(object) == 0 {
			return ""
		}
		// serde_json's default map is ordered by key.
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		name = keys[0]
	}
	if len(name) > capacity.MaximumBotErrorKindBytes {
		return ""
	}
	var kind strings.Builder
	for _, b := range []byte(name) {
		if b >= 'A' && b <= 'Z' {
			kind.WriteByte('_')
			b += 'a' - 'A'
		}
		kind.WriteByte(b)
	}
	return kind.String()
}

func providerReason(code int, kind, message string) app.ProviderFailureReason {
	if code == -32000 { // ACP AUTH_REQUIRED.
		return app.ProviderSignedOut
	}
	switch kind {
	case "usage_limit_exceeded":
		return app.ProviderUsageExhausted
	case "rate_limit", "billing_error", "account_on_hold":
		// Claude's SDK reports plan limits under billing/rate categories.
		for _, text := range []string{"You've hit your", "You've reached your", "You're out of usage credits", "Your org is out of usage", "Your seat type doesn't include usage credits", "Your seat type doesn't include usage", "Your usage allocation has been disabled by your admin", "Your group's usage limit is set to $0", "requires usage credits", "You're out of extra usage", "Your seat type doesn't include extra usage", "usage limit"} {
			if strings.Contains(message, text) {
				return app.ProviderUsageExhausted
			}
		}
		if kind == "rate_limit" {
			return app.ProviderRateLimited
		}
		return app.ProviderOutOfCredits
	case "rate_limit_exceeded":
		return app.ProviderRateLimited
	case "overloaded", "server_error", "server_overloaded", "internal_server_error", "response_too_many_failed_attempts":
		return app.ProviderOverloaded
	case "authentication_failed", "oauth_org_not_allowed", "unauthorized", "auth":
		return app.ProviderSignedOut
	case "model_not_found":
		return app.ProviderModelMissing
	}
	// Deliberately stricter than upstream's uncategorized message fallback:
	// arbitrary/invalid categories cannot become quota errors by quoting text.
	return ""
}
