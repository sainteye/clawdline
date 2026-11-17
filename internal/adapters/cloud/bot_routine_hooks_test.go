package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
)

func TestBotRoutineHooksClientHTTP(t *testing.T) {
	body := []byte(" {\"number\":9007199254740993} \n")
	delivery := bh.Delivery{ID: "queue-id", HookID: "routine", Revision: 1, LeaseToken: "lease", Headers: map[string]string{"Authorization": "Bearer " + strings.Repeat("a", 64), "X-Delivery-Id": "original-sender-id"}, BodyBase64: base64.StdEncoding.EncodeToString(body)}
	calls := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-machine" {
			t.Error("machine authentication missing")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/bot-routine-hooks":
			var set bh.Enrollment
			if json.NewDecoder(r.Body).Decode(&set) != nil || set.Revision != 7 || set.Hooks[0].ID != "a" {
				t.Error("noncanonical enrollment")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": bh.Protocol, "revision": 7})
		case "/v1/bot-routine-hook-deliveries/claim":
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": bh.Protocol, "delivery": delivery})
		default:
			_ = json.NewEncoder(w).Encode(bh.Metadata{Protocol: bh.Protocol, DeliveryID: "queue-id", State: "host_accepted"})
		}
	}))
	defer server.Close()
	c := NewBotRoutineHookClient(server.URL, "synthetic-machine")
	c.HTTP = server.Client()
	ctx := context.Background()
	if err := c.Enroll(ctx, bh.Enrollment{Protocol: bh.Protocol, Revision: 7, Hooks: []bh.Hook{{ID: "z", Key: strings.Repeat("b", 64)}, {ID: "a", Key: strings.Repeat("a", 64), Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, headers, err := got.Decode()
	if err != nil || string(raw) != string(body) || headers.Get("X-Delivery-Id") != "original-sender-id" {
		t.Fatal("original bytes or identity changed", err)
	}
	ack := bh.Ack{Protocol: bh.Protocol, LeaseToken: got.LeaseToken, Status: 200, Code: "accepted", RunID: "actual-source-run"}
	if _, err = c.Acknowledge(ctx, got.ID, ack); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Inspect(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || c.PublicURL("machine", "routine") != server.URL+"/v1/hooks/machine/routine" {
		t.Fatal("incorrect wire routes or public URL", calls)
	}
}

func TestBotRoutineHooksRejectsMalformedAndUnavailable(t *testing.T) {
	valid := `{"protocol":"clawdline.bot_routine_hooks.v1","delivery":{"id":"queue","hook_id":"routine","revision":1,"headers":{"authorization":"Bearer aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","x-delivery-id":"original"},"body_base64":"e30=","lease_token":"lease"}}`
	for name, raw := range map[string]string{
		"protocol":               strings.Replace(valid, bh.Protocol, "future", 1),
		"base64":                 strings.Replace(valid, "e30=", "e31=", 1),
		"newline":                strings.Replace(valid, "e30=", "e3\\n0=", 1),
		"oversize":               strings.Replace(valid, "e30=", base64.StdEncoding.EncodeToString(make([]byte, bh.MaxEventBytes+1)), 1),
		"duplicate":              strings.Replace(valid, `"x-delivery-id":"original"`, `"x-delivery-id":"wrong","x-delivery-id":"original"`, 1),
		"duplicate-case":         strings.Replace(valid, `"x-delivery-id":"original"`, `"X-Delivery-Id":"wrong","x-delivery-id":"original"`, 1),
		"unsupported":            strings.Replace(valid, `"x-delivery-id"`, `"x-device-token"`, 1),
		"missing-identity":       strings.Replace(valid, `,"x-delivery-id":"original"`, "", 1),
		"unknown-field":          strings.Replace(valid, `"delivery":{`, `"extra":true,"delivery":{`, 1),
		"missing-delivery":       `{"protocol":"clawdline.bot_routine_hooks.v1"}`,
		"unknown-delivery-field": strings.Replace(valid, `"hook_id":"routine"`, `"hook_id":"routine","extra":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
			defer s.Close()
			if _, err := NewBotRoutineHookClient(s.URL, "machine").Claim(context.Background()); err == nil {
				t.Fatal("accepted malformed claim")
			}
		})
	}
	for _, status := range []int{404, 503, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			called := false
			destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			defer destination.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"code":"private-secret-error"}`))
			}))
			defer s.Close()
			_, err := NewBotRoutineHookClient(s.URL, "machine").Claim(context.Background())
			if bh.Code(err) != "webhook_unavailable" || called {
				t.Fatal("old/unavailable API or redirect was trusted", err)
			}
		})
	}
	for _, base := range []string{"", "http://example.test", "https://name:fixture-secret@example.test", "https://example.test/path", "https://example.test?key=fixture-secret"} {
		c := NewBotRoutineHookClient(base, "machine")
		if c.PublicURL("machine", "routine") != "" {
			t.Fatal("unsafe public URL")
		}
		if _, err := c.Claim(context.Background()); bh.Code(err) != "webhook_unavailable" {
			t.Fatal("unsafe base used")
		}
	}
}

func TestBotRoutineHooksCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer s.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := NewBotRoutineHookClient(s.URL, "machine").Claim(ctx); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
}
