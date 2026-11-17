package cloud

// This client speaks only the dedicated machine Bot control plane. Its bearer
// comes from the installed machine identity, never a viewer or model request.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type BotNetworkClient struct {
	Identity Identity
	HTTP     *http.Client
	Clock    func() time.Time
}
type BotCapabilities struct {
	MachineID   string             `json:"machine_id"`
	Revision    int64              `json:"revision"`
	WireVersion int                `json:"wire_version"`
	State       string             `json:"state"`
	Endpoints   []wire.BotEndpoint `json:"endpoints"`
	ExpiresAt   int64              `json:"expires_at"`
}
type BotConsent struct {
	ID        string           `json:"authority_id"`
	AccountID string           `json:"account_id"`
	PairID    string           `json:"pair_id"`
	Epoch     int64            `json:"authority_epoch"`
	Source    wire.BotEndpoint `json:"source"`
	Target    wire.BotEndpoint `json:"target"`
	Kinds     []string         `json:"kinds"`
	CreatedAt int64            `json:"created_at"`
	ExpiresAt int64            `json:"expires_at"`
	RevokedAt *int64           `json:"revoked_at"`
}

func NewBotNetworkClient(i Identity) *BotNetworkClient {
	return &BotNetworkClient{Identity: i, HTTP: &http.Client{Timeout: OpeningTimeout}, Clock: time.Now}
}
func botFail(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }
func botSegment(s string) bool {
	if len(s) == 0 || len(s) > domaincloud.MaxSegmentBytes {
		return false
	}
	for _, c := range []byte(s) {
		if c < 33 || c > 126 || c == '/' || c == '|' {
			return false
		}
	}
	return true
}
func botCapture(e wire.BotEndpoint) bool {
	return botSegment(e.AccountID) && botSegment(e.MachineID) && botSegment(e.ProjectID) && botSegment(e.BotID) && botSegment(e.ConversationID) && e.Generation >= 0 && e.Generation <= 9007199254740991
}
func botDecode(raw []byte, out any) error {
	if !botHookUniqueJSON(raw) || !botRequired(raw, reflect.TypeOf(out).Elem()) {
		return botFail(pa.Unsupported, "bot_cloud_response")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return botFail(pa.Unsupported, "bot_cloud_response")
	}
	return nil
}

func botRequired(raw []byte, t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return true
		}
		return botRequired(raw, t.Elem())
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	switch t.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			v, ok := fields[tag]
			if !ok || !botRequired(v, f.Type) {
				return false
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return false
		}
		for _, v := range items {
			if !botRequired(v, t.Elem()) {
				return false
			}
		}
	}
	return true
}
func (c *BotNetworkClient) request(ctx context.Context, method, path string, in, out any) error {
	if c == nil || !c.Identity.Valid() || c.HTTP == nil || c.Clock == nil {
		return botFail(pa.Unsupported, "bot_cloud_identity")
	}
	u, e := url.Parse(c.Identity.APIBase)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return botFail(pa.Unsupported, "bot_cloud_origin")
	}
	var raw []byte
	if in != nil {
		raw, e = json.Marshal(in)
		if e != nil {
			return botFail(pa.Invalid, "bot_cloud_request")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, OpeningTimeout)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Identity.APIBase, "/")+path, bytes.NewReader(raw))
	if e != nil {
		return botFail(pa.Unsupported, "bot_cloud_request")
	}
	r.Header.Set("Authorization", "Bearer "+c.Identity.MachineCredential)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(r)
	if e != nil {
		return botFail(pa.Offline, "bot_cloud_unavailable")
	}
	defer resp.Body.Close()
	raw, e = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if e != nil || len(raw) > maxResponseBytes {
		return botFail(pa.Unsupported, "bot_cloud_response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var denial struct {
			Authorized *bool  `json:"authorized"`
			Reason     string `json:"reason"`
		}
		if botDecode(raw, &denial) == nil && denial.Authorized != nil && !*denial.Authorized {
			return botDenial(denial.Reason)
		}
		if resp.StatusCode == 401 {
			return botFail(pa.Unpaired, "bot_cloud_authentication")
		}
		return botFail(pa.Unsupported, "bot_cloud_unavailable")
	}
	return botDecode(raw, out)
}
func botDenial(reason string) error {
	code := pa.Unsupported
	switch reason {
	case "invalid_shape":
		code = pa.Invalid
	case "wrong_account", "not_participant", "capture_mismatch":
		code = pa.ScopeDenied
	case "pair_inactive", "consent_missing":
		code = pa.Unpaired
	case "pair_revoked", "consent_revoked":
		code = pa.Revoked
	case "pair_expired", "consent_expired":
		code = pa.Expired
	case "consent_stale":
		code = pa.StaleVersion
	case "source_offline":
		code = pa.Offline
	case "request_conflict":
		code = pa.IdempotencyConflict
	case "capacity_exhausted":
		code = pa.CapacityFull
	case "correlation_missing":
		code = pa.NotFound
	case "transient_unknown":
		code = pa.OutcomeUnknown
	case "capability_unavailable", "source_unavailable":
		code = pa.Unsupported
	default:
		return botFail(pa.Unsupported, "bot_cloud_unavailable")
	}
	return botFail(code, "bot_cloud_"+reason)
}
func (c *BotNetworkClient) CheckBotAuthority(ctx context.Context, h wire.BotHeader) error {
	i := c.Identity
	if h.Source.AccountID != i.AccountID || h.Target.AccountID != i.AccountID || h.Source.MachineID != i.MachineID && h.Target.MachineID != i.MachineID {
		return botFail(pa.ScopeDenied, "bot_cloud_identity")
	}
	var out struct {
		Authorized *bool  `json:"authorized"`
		AccountID  string `json:"account_id"`
		PairID     string `json:"pair_id"`
		ID         string `json:"authority_id"`
		Epoch      int64  `json:"authority_epoch"`
		ExpiresAt  int64  `json:"expires_at"`
	}
	if e := c.request(ctx, http.MethodPost, "/v1/bot-network/authorize", struct {
		Header wire.BotHeader `json:"header"`
	}{h}, &out); e != nil {
		return e
	}
	if out.Authorized == nil || !*out.Authorized || out.AccountID != i.AccountID || out.PairID != h.PairID || out.ID != h.AuthorityID || out.Epoch != h.AuthorityEpoch || out.ExpiresAt <= c.Clock().Unix() || out.ExpiresAt > c.Clock().Unix()+pa.MaxNetworkCloudLeaseSeconds {
		return botFail(pa.Unsupported, "bot_cloud_authorization")
	}
	return nil
}
func (c *BotNetworkClient) Capabilities(ctx context.Context, machine string) (BotCapabilities, error) {
	var out BotCapabilities
	if !botSegment(machine) {
		return out, botFail(pa.Invalid, "bot_cloud_machine")
	}
	e := c.request(ctx, http.MethodGet, "/v1/bot-network/capabilities/"+url.PathEscape(machine), nil, &out)
	if e != nil {
		return out, e
	}
	if out.MachineID != machine || out.Revision < 1 || out.WireVersion != 1 || out.ExpiresAt <= c.Clock().Unix() || out.ExpiresAt > c.Clock().Unix()+pa.MaxNetworkCloudLeaseSeconds || out.State != "ready" && out.State != "unavailable" || len(out.Endpoints) > pa.MaxNetworkCloudEndpoints || out.State == "ready" && len(out.Endpoints) == 0 || out.State == "unavailable" && len(out.Endpoints) != 0 {
		return out, botFail(pa.Unsupported, "bot_cloud_capabilities")
	}
	seen := map[string]bool{}
	for _, ep := range out.Endpoints {
		key := ep.ProjectID + "/" + ep.BotID
		if !botCapture(ep) || ep.AccountID != c.Identity.AccountID || ep.MachineID != machine || seen[key] {
			return out, botFail(pa.Unsupported, "bot_cloud_capture")
		}
		seen[key] = true
	}
	return out, nil
}
func (c *BotNetworkClient) PutCapabilities(ctx context.Context, revision int64, endpoints []wire.BotEndpoint) (BotCapabilities, error) {
	state := "ready"
	if len(endpoints) == 0 {
		state = "unavailable"
		endpoints = []wire.BotEndpoint{}
	}
	for _, ep := range endpoints {
		if !botCapture(ep) || ep.AccountID != c.Identity.AccountID || ep.MachineID != c.Identity.MachineID {
			return BotCapabilities{}, botFail(pa.Invalid, "bot_cloud_capture")
		}
	}
	if len(endpoints) > pa.MaxNetworkCloudEndpoints {
		return BotCapabilities{}, botFail(pa.CapacityFull, "bot_cloud_endpoints")
	}
	expires := c.Clock().Unix() + pa.MaxNetworkCloudLeaseSeconds
	var out struct {
		Revision  int64 `json:"revision"`
		ExpiresAt int64 `json:"expires_at"`
	}
	e := c.request(ctx, http.MethodPut, "/v1/bot-network/capabilities", struct {
		ExpectedRevision int64              `json:"expected_revision"`
		Version          int                `json:"wire_version"`
		State            string             `json:"state"`
		Endpoints        []wire.BotEndpoint `json:"endpoints"`
		ExpiresAt        int64              `json:"expires_at"`
	}{revision, 1, state, endpoints, expires}, &out)
	if e != nil {
		return BotCapabilities{}, e
	}
	if out.Revision != revision+1 || out.ExpiresAt != expires {
		return BotCapabilities{}, botFail(pa.Unsupported, "bot_cloud_lease")
	}
	return BotCapabilities{MachineID: c.Identity.MachineID, Revision: out.Revision, WireVersion: 1, State: state, Endpoints: endpoints, ExpiresAt: expires}, nil
}
func (c *BotNetworkClient) Consent(ctx context.Context, id string) (BotConsent, error) {
	var out BotConsent
	if !botSegment(id) {
		return out, botFail(pa.Invalid, "bot_cloud_consent")
	}
	e := c.request(ctx, http.MethodGet, "/v1/bot-network/source-consents/"+url.PathEscape(id), nil, &out)
	if e != nil {
		return out, e
	}
	if out.ID != id || out.AccountID != c.Identity.AccountID || !botSegment(out.PairID) || out.Epoch < 1 || !botCapture(out.Source) || !botCapture(out.Target) || out.Source.AccountID != out.AccountID || out.Target.AccountID != out.AccountID || out.Source.MachineID == out.Target.MachineID || out.Source.MachineID != c.Identity.MachineID && out.Target.MachineID != c.Identity.MachineID || out.CreatedAt <= 0 || out.ExpiresAt <= out.CreatedAt || out.RevokedAt != nil && *out.RevokedAt <= 0 || len(out.Kinds) == 0 {
		return out, botFail(pa.Unsupported, "bot_cloud_consent")
	}
	seen := map[string]bool{}
	for _, kind := range out.Kinds {
		if seen[kind] || kind != "directory" && kind != "message" && kind != "read" {
			return out, botFail(pa.Unsupported, "bot_cloud_consent")
		}
		seen[kind] = true
	}
	return out, nil
}
func (c *BotNetworkClient) Pair(ctx context.Context, id string) (PeerPair, error) {
	var out PeerPair
	if !botSegment(id) {
		return out, botFail(pa.Invalid, "bot_cloud_pair")
	}
	e := c.request(ctx, http.MethodGet, "/v1/peer/pairs/"+url.PathEscape(id), nil, &out)
	if e == nil && out.PairID != id {
		e = botFail(pa.Unpaired, "bot_cloud_pair")
	}
	return out, e
}
