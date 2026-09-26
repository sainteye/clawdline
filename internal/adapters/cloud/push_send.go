package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PushSendPath is the machine-credential route that forwards one sealed Web
// Push message to a subscription the account's browser registered with Cloud.
const PushSendPath = "/v1/push/send"

// PushSendRequest is one message, already sealed by this machine to the
// browser's own keys (RFC 8291). Cloud holds the subscription's endpoint and
// the account's VAPID key and never the browser's p256dh or auth, so what it
// forwards is ciphertext it cannot open.
type PushSendRequest struct {
	SubscriptionID string
	Ciphertext     []byte
	ContentType    string
	TTL            int
	Urgency        string
	Topic          string
}

// PushSendReply is what Cloud answered, before anybody decides what it means:
// that decision is internal/adapters/push's, beside the direct path's.
type PushSendReply struct {
	// Status is Cloud's own HTTP status.
	Status int
	// Code is the refusal code (`subscription_gone`, `push_failed`,
	// `unknown_subscription`, …), or "".
	Code string
	// PushStatus is the push service's status as Cloud reported it; 0 when
	// Cloud did not reach one.
	PushStatus int
	// RetryAfter is what a 429 asked for.
	RetryAfter time.Duration
}

// PushClient is the machine side of `POST /v1/push/send`. Like
// ScheduleWebhookClient it holds the credential and never hands it out.
type PushClient struct {
	Client     *AccountClient
	Credential string
}

// NewPushClient is a client whose request timeout is the direct path's: a
// forward that has not been answered in fifteen seconds will not make its
// notification timely either.
func NewPushClient(baseURL, credential string) PushClient {
	client := NewAccountClient(baseURL)
	client.HTTP = &http.Client{Timeout: 15 * time.Second}
	return PushClient{Client: client, Credential: credential}
}

// Send forwards one message. A transport failure is the error; every HTTP
// answer, refusals included, is a reply.
func (c PushClient) Send(ctx context.Context, request PushSendRequest) (PushSendReply, error) {
	payload := map[string]any{
		"subscription_id": request.SubscriptionID,
		"ciphertext":      base64.RawURLEncoding.EncodeToString(request.Ciphertext),
		"content_type":    request.ContentType,
		"ttl":             request.TTL,
		"urgency":         request.Urgency,
	}
	if request.Topic != "" {
		payload["topic"] = request.Topic
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return PushSendReply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Client.BaseURL+PushSendPath, bytes.NewReader(encoded))
	if err != nil {
		return PushSendReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Credential)
	resp, err := c.Client.HTTP.Do(req)
	if err != nil {
		return PushSendReply{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	reply := PushSendReply{Status: resp.StatusCode}
	var body struct {
		PushStatus int             `json:"push_status"`
		RetryAfter json.Number     `json:"retry_after"`
		Error      json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil {
		reply.PushStatus = body.PushStatus
		retryAfter := body.RetryAfter
		// Cloud's API puts a refusal's facts inside `error.details`; the flat
		// spelling above is what a success carries.
		var nested struct {
			Details struct {
				PushStatus int         `json:"push_status"`
				RetryAfter json.Number `json:"retry_after"`
			} `json:"details"`
		}
		if len(body.Error) > 0 && json.Unmarshal(body.Error, &nested) == nil {
			if reply.PushStatus == 0 {
				reply.PushStatus = nested.Details.PushStatus
			}
			if retryAfter == "" {
				retryAfter = nested.Details.RetryAfter
			}
		}
		if seconds, err := retryAfter.Int64(); err == nil && seconds > 0 {
			reply.RetryAfter = time.Duration(seconds) * time.Second
		}
	}
	if resp.StatusCode >= 400 {
		reply.Code = decodeAPIError(PushSendPath, resp.StatusCode, data).Code
	}
	if header := strings.TrimSpace(resp.Header.Get("Retry-After")); header != "" && reply.RetryAfter == 0 {
		if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
			reply.RetryAfter = time.Duration(seconds) * time.Second
		}
	}
	return reply, nil
}
