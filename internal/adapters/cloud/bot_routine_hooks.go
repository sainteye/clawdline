package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
)

// BotRoutineHookClient owns the machine credential. No handler receives it.
type BotRoutineHookClient struct {
	base       string
	credential string
	HTTP       *http.Client
}

func NewBotRoutineHookClient(base, credential string) *BotRoutineHookClient {
	return &BotRoutineHookClient{base: base, credential: credential, HTTP: &http.Client{Timeout: time.Duration(bh.MaxHTTPSeconds) * time.Second}}
}

func (c *BotRoutineHookClient) request(ctx context.Context, method, path string, in, out any) error {
	base, err := bh.TrustedBase(c.base)
	if err != nil || c.credential == "" || c.HTTP == nil {
		return bh.Fail("webhook_unavailable")
	}
	var raw []byte
	if in != nil {
		raw, err = json.Marshal(in)
		if err != nil || len(raw) > bh.MaxEnrollmentBytes {
			return bh.Fail("webhook_too_large")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String()+path, bytes.NewReader(raw))
	if err != nil {
		return bh.Fail("webhook_unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+c.credential)
	req.Header.Set("Content-Type", "application/json")
	client := *c.HTTP
	client.Timeout = time.Duration(bh.MaxHTTPSeconds) * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return bh.Fail("webhook_unavailable")
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, bh.MaxResponseBytes+1))
	if err != nil || len(raw) > bh.MaxResponseBytes {
		return bh.Fail("webhook_unavailable")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &failure)
		switch failure.Code {
		case "webhook_revision_conflict", "webhook_lease_conflict", "webhook_ack_conflict", "webhook_not_enrolled", "webhook_delivery_not_found":
			return bh.Fail(failure.Code)
		default:
			return bh.Fail("webhook_unavailable")
		}
	}
	if !botHookUniqueJSON(raw) {
		return bh.Fail("webhook_invalid_response")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF {
		return bh.Fail("webhook_invalid_response")
	}
	return nil
}

// Duplicate JSON keys could hide repeated proof headers before map decoding.
func botHookUniqueJSON(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var value func() bool
	value = func() bool {
		t, err := dec.Token()
		if err != nil {
			return false
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, e := dec.Token()
				name, ok := key.(string)
				if e != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value() {
					return false
				}
			}
		case '[':
			for dec.More() {
				if !value() {
					return false
				}
			}
		default:
			return false
		}
		end, e := dec.Token()
		return e == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
	}
	if !value() {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}

func (c *BotRoutineHookClient) Enroll(ctx context.Context, set bh.Enrollment) error {
	hooks, err := bh.Canonical(set.Hooks)
	if err != nil {
		return err
	}
	if set.Protocol != bh.Protocol || set.Revision < 1 || set.Revision > bh.MaxRevision {
		return bh.Fail("webhook_invalid")
	}
	set.Hooks = hooks
	var out struct {
		Protocol string `json:"protocol"`
		Revision int64  `json:"revision"`
	}
	if err = c.request(ctx, http.MethodPut, "/v1/bot-routine-hooks", set, &out); err != nil {
		return err
	}
	if out.Protocol != bh.Protocol || out.Revision != set.Revision {
		return bh.Fail("webhook_invalid_response")
	}
	return nil
}
func (c *BotRoutineHookClient) Claim(ctx context.Context) (*bh.Delivery, error) {
	var out struct {
		Protocol string          `json:"protocol"`
		Delivery json.RawMessage `json:"delivery"`
	}
	if err := c.request(ctx, http.MethodPost, "/v1/bot-routine-hook-deliveries/claim", map[string]string{"protocol": bh.Protocol}, &out); err != nil {
		return nil, err
	}
	if out.Protocol != bh.Protocol || len(out.Delivery) == 0 {
		return nil, bh.Fail("webhook_invalid_response")
	}
	if bytes.Equal(out.Delivery, []byte("null")) {
		return nil, nil
	}
	var delivery bh.Delivery
	decoder := json.NewDecoder(bytes.NewReader(out.Delivery))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&delivery) != nil {
		return nil, bh.Fail("webhook_invalid_response")
	}
	if _, _, err := delivery.Decode(); err != nil {
		return nil, err
	}
	return &delivery, nil
}
func (c *BotRoutineHookClient) metadata(ctx context.Context, method, id, suffix string, in any) (bh.Metadata, error) {
	var out bh.Metadata
	if !bh.ASCII(id, bh.MaxHeaderBytes) {
		return out, bh.Fail("webhook_invalid")
	}
	err := c.request(ctx, method, "/v1/bot-routine-hook-deliveries/"+url.PathEscape(id)+suffix, in, &out)
	if err != nil {
		return bh.Metadata{}, err
	}
	if out.Protocol != bh.Protocol || out.DeliveryID != id {
		return bh.Metadata{}, bh.Fail("webhook_invalid_response")
	}
	switch out.State {
	case "queued", "leased", "host_accepted", "refused", "outcome_unknown", "invalidated", "expired":
	default:
		return bh.Metadata{}, bh.Fail("webhook_invalid_response")
	}
	return out, nil
}
func (c *BotRoutineHookClient) Acknowledge(ctx context.Context, id string, ack bh.Ack) (bh.Metadata, error) {
	if ack.Protocol != bh.Protocol || ack.Status < 100 || ack.Status > 599 || !bh.ASCII(ack.LeaseToken, bh.MaxHeaderBytes) || !bh.ASCII(ack.Code, bh.MaxHeaderBytes) || ack.RunID != "" && !bh.ASCII(ack.RunID, bh.MaxHeaderBytes) {
		return bh.Metadata{}, bh.Fail("webhook_invalid")
	}
	metadata, err := c.metadata(ctx, http.MethodPost, id, "/ack", ack)
	if err != nil {
		return bh.Metadata{}, err
	}
	if metadata.State == "leased" || metadata.State == "queued" && (ack.Status != http.StatusConflict || ack.Code != "webhook_busy") {
		return bh.Metadata{}, bh.Fail("webhook_invalid_response")
	}
	return metadata, nil
}
func (c *BotRoutineHookClient) Inspect(ctx context.Context, id string) (bh.Metadata, error) {
	return c.metadata(ctx, http.MethodGet, id, "", nil)
}
func (c *BotRoutineHookClient) PublicURL(machineID, routineID string) string {
	u, err := bh.TrustedBase(c.base)
	if err != nil || u.Scheme != "https" || !bh.ASCII(machineID, bh.MaxRoutineIDBytes) || !bh.ASCII(routineID, bh.MaxRoutineIDBytes) {
		return ""
	}
	return strings.TrimSuffix(u.String(), "/") + "/v1/hooks/" + url.PathEscape(machineID) + "/" + url.PathEscape(routineID)
}
