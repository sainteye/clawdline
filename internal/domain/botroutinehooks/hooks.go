// Package botroutinehooks defines the separate machine-authenticated webhook bridge.
// Event bytes and proof remain untrusted; only the source Host admits a run.
package botroutinehooks

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const Protocol = "clawdline.bot_routine_hooks.v1"

const (
	MaxHooks                    = 1000
	MaxPending                  = 1
	MaxEnrollmentBytes          = 512 << 10
	MaxEventBytes               = 64 << 10
	MaxHeaderBytes              = 200
	MaxRoutineIDBytes           = 100
	MaxResponseBytes            = 128 << 10
	MaxLocalResponseBytes       = 16 << 10
	MaxHTTPSeconds              = 10
	MaxPollSeconds              = 5
	MaxBackoffSeconds           = 60
	MaxRevision           int64 = 9007199254740991
)

// Fixed error codes carry no server text, payload, proof or credential.
type Failure struct{ Code string }

func (f *Failure) Error() string { return f.Code }
func Fail(code string) error     { return &Failure{Code: code} }
func Code(err error) string {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	return "webhook_unavailable"
}

type Identity struct{ AccountID, MachineID string }
type Hook struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}
type Enrollment struct {
	Protocol string `json:"protocol"`
	Revision int64  `json:"revision"`
	Hooks    []Hook `json:"hooks"`
}
type Delivery struct {
	ID         string            `json:"id"`
	HookID     string            `json:"hook_id"`
	Revision   int64             `json:"revision"`
	Headers    map[string]string `json:"headers"`
	BodyBase64 string            `json:"body_base64"`
	LeaseToken string            `json:"lease_token"`
}
type Ack struct {
	Protocol   string `json:"protocol"`
	LeaseToken string `json:"lease_token"`
	Status     int    `json:"status"`
	Code       string `json:"code"`
	RunID      string `json:"run_id,omitempty"`
}
type Metadata struct {
	Protocol   string `json:"protocol"`
	DeliveryID string `json:"delivery_id"`
	State      string `json:"state"`
	Code       string `json:"code,omitempty"`
	RunID      string `json:"run_id,omitempty"`
}
type Cloud interface {
	Enroll(context.Context, Enrollment) error
	Claim(context.Context) (*Delivery, error)
	Acknowledge(context.Context, string, Ack) (Metadata, error)
	Inspect(context.Context, string) (Metadata, error)
	PublicURL(machineID, routineID string) string
}

func ASCII(value string, bound int) bool {
	if len(value) == 0 || len(value) > bound {
		return false
	}
	for _, b := range []byte(value) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

func Canonical(hooks []Hook) ([]Hook, error) {
	if len(hooks) > MaxHooks {
		return nil, Fail("webhook_hooks_capacity")
	}
	out := append([]Hook{}, hooks...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i, h := range out {
		if !ASCII(h.ID, MaxRoutineIDBytes) || len(h.Key) < 32 || !ASCII(h.Key, MaxEnrollmentBytes) || i > 0 && out[i-1].ID == h.ID {
			return nil, Fail("webhook_invalid")
		}
	}
	return out, nil
}

// Decode refuses aliases, unsupported/duplicate proof headers and noncanonical
// base64 before any local request is made. It never parses or rewrites event JSON.
func (d Delivery) Decode() ([]byte, http.Header, error) {
	bad := func() ([]byte, http.Header, error) { return nil, nil, Fail("webhook_invalid") }
	if !ASCII(d.ID, MaxHeaderBytes) || !ASCII(d.HookID, MaxRoutineIDBytes) || !ASCII(d.LeaseToken, MaxHeaderBytes) || d.Revision < 1 || d.Revision > MaxRevision || len(d.BodyBase64) > base64.StdEncoding.EncodedLen(MaxEventBytes) {
		return bad()
	}
	body, err := base64.StdEncoding.Strict().DecodeString(d.BodyBase64)
	if err != nil || len(body) > MaxEventBytes || base64.StdEncoding.EncodeToString(body) != d.BodyBase64 {
		return bad()
	}
	headers := http.Header{}
	for k, v := range d.Headers {
		name := strings.ToLower(k)
		switch name {
		case "authorization", "x-hub-signature-256":
		case "x-delivery-id", "x-github-delivery", "idempotency-key", "x-github-event":
		default:
			return bad()
		}
		graphic := v
		if name == "authorization" {
			if !strings.HasPrefix(v, "Bearer ") {
				return bad()
			}
			graphic = strings.TrimPrefix(v, "Bearer ")
		}
		if len(v) > MaxHeaderBytes || !ASCII(graphic, MaxHeaderBytes) || headers.Get(name) != "" {
			return bad()
		}
		headers.Set(name, v)
	}
	if headers.Get("X-Delivery-Id") == "" || headers.Get("Authorization") == "" && headers.Get("X-Hub-Signature-256") == "" {
		return bad()
	}
	return body, headers, nil
}

// TrustedBase is configuration, never sender input. HTTP is allowed only for
// loopback fixtures; production public addresses require HTTPS.
func TrustedBase(base string) (*url.URL, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Path != "" && u.Path != "/" || u.RawPath != "" {
		return nil, Fail("webhook_unavailable")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return nil, Fail("webhook_unavailable")
	}
	u.Path = ""
	return u, nil
}
