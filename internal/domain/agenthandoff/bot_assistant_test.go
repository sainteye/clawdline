package agenthandoff

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"
)

func assistantWireFixture(t *testing.T) (AssistantCall, []byte, AssistantFacts, ed25519.PrivateKey) {
	t.Helper()
	h, body, _, _, _, _, _ := botWireFixture(t)
	now := time.Unix(1800000000, 0).UTC()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x55}, 32))
	call := AssistantCall{Version: BotWireVersion, RequestID: "assistant-request-1", Kind: "message", Target: h.Target,
		GrantID: "assistant-consent-1", GrantEpoch: 2, KeyID: "assistant-gateway-key-1", BodyDigest: h.BodyDigest,
		IssuedAt: now.Add(-time.Second).Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	call, err := SignAssistantCall(call, key)
	if err != nil {
		t.Fatal(err)
	}
	facts := AssistantFacts{GatewayConfigured: true, GlobalOptIn: true, FeatureEnabled: true,
		SourceWritable: true, TargetCurrent: true, TargetReachable: true, GrantReadable: true, Now: now,
		Grant: AssistantGrant{ID: call.GrantID, Epoch: call.GrantEpoch, KeyID: call.KeyID,
			PublicKey: key.Public().(ed25519.PublicKey), PrincipalID: "synthetic-assistant-principal",
			AccountID: h.Target.AccountID, MachineID: h.Target.MachineID, ProjectID: h.Target.ProjectID,
			BotIDs: []string{h.Target.BotID}, AllowList: true, AllowMessage: true, AllowRead: true,
			NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}
	return call, body, facts, key
}

func TestAssistantPrincipalRequiresDedicatedPinnedSignatureAndCurrentConsent(t *testing.T) {
	call, body, facts, key := assistantWireFixture(t)
	p, err := VerifyAssistantCall(call, body, facts)
	if err != nil || p.PrincipalID() != facts.Grant.PrincipalID || p.AccountID() != facts.Grant.AccountID ||
		p.GrantID() != facts.Grant.ID || p.Epoch() != facts.Grant.Epoch {
		t.Fatalf("verified principal: %#v %v", p, err)
	}
	checks := []struct {
		name   string
		change func(*AssistantFacts)
		want   Refusal
	}{
		{"no-gateway", func(f *AssistantFacts) { f.GatewayConfigured = false }, AssistantUnavailable},
		{"unreadable-store", func(f *AssistantFacts) { f.GrantReadable = false }, AssistantUnavailable},
		{"human-opt-in-off", func(f *AssistantFacts) { f.GlobalOptIn = false }, AssistantDenied},
		{"feature-disabled", func(f *AssistantFacts) { f.FeatureEnabled = false }, AssistantDenied},
		{"source-read-only", func(f *AssistantFacts) { f.SourceWritable = false }, AssistantDenied},
		{"target-switch-off", func(f *AssistantFacts) { f.TargetReachable = false }, AssistantDenied},
		{"target-capture-changed", func(f *AssistantFacts) { f.TargetCurrent = false }, AssistantStale},
		{"revoked-key", func(f *AssistantFacts) { f.Grant.Revoked = true }, AssistantRevoked},
		{"revoked-epoch", func(f *AssistantFacts) { f.Grant.Epoch++ }, AssistantStale},
		{"wrong-account", func(f *AssistantFacts) { f.Grant.AccountID = "account-other" }, AssistantDenied},
		{"wrong-machine", func(f *AssistantFacts) { f.Grant.MachineID = "machine-other" }, AssistantDenied},
		{"wrong-project", func(f *AssistantFacts) { f.Grant.ProjectID = "project-other" }, AssistantDenied},
		{"wrong-bot", func(f *AssistantFacts) { f.Grant.BotIDs = []string{"bot-other"} }, AssistantDenied},
		{"no-bot-scope", func(f *AssistantFacts) { f.Grant.BotIDs = nil }, AssistantDenied},
		{"message-not-granted", func(f *AssistantFacts) { f.Grant.AllowMessage = false }, AssistantDenied},
		{"future-grant", func(f *AssistantFacts) { f.Grant.NotBefore = f.Now.Add(time.Second) }, AssistantExpired},
		{"expired-grant", func(f *AssistantFacts) { f.Grant.ExpiresAt = f.Now }, AssistantExpired},
		{"expired-call", func(f *AssistantFacts) { f.Now = time.Unix(call.ExpiresAt, 0) }, AssistantExpired},
		{"future-call", func(f *AssistantFacts) { f.Now = time.Unix(call.IssuedAt-1, 0) }, AssistantExpired},
		{"missing-principal", func(f *AssistantFacts) { f.Grant.PrincipalID = "" }, AssistantUnverified},
		{"other-key", func(f *AssistantFacts) {
			f.Grant.PublicKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x66}, 32)).Public().(ed25519.PublicKey)
		}, AssistantUnverified},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			f := facts
			check.change(&f)
			got, err := VerifyAssistantCall(call, body, f)
			if err != check.want || got.PrincipalID() != "" {
				t.Fatalf("authority retained: %#v %v, want %v", got, err, check.want)
			}
		})
	}
	other, err := SignAssistantCall(call, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x77}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAssistantCall(other, body, facts); err != AssistantUnverified {
		t.Fatalf("untrusted gateway signed principal: %v", err)
	}
	call.Target.AccountID = "account-other"
	if _, err := VerifyAssistantCall(call, body, facts); err != AssistantUnverified {
		t.Fatalf("unsigned account edit: %v", err)
	}
	call, err = SignAssistantCall(call, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAssistantCall(call, body, facts); err != AssistantDenied {
		t.Fatalf("signed account outside grant: %v", err)
	}
}

func TestAssistantSignatureCannotAuthorizeOtherToolsOrReplyCaptures(t *testing.T) {
	call, body, facts, key := assistantWireFixture(t)
	for _, kind := range []string{"shell", "reply", "handoff", "set_assistants", "memory_write", ""} {
		changed := call
		changed.Kind = kind
		changed, err := SignAssistantCall(changed, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyAssistantCall(changed, body, facts); err != AssistantInvalid {
			t.Fatalf("tool %q got %v", kind, err)
		}
	}
	call.Kind, call.CorrelationID = "read", "original-request"
	call, err := SignAssistantCall(call, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAssistantCall(call, body, facts); err != nil {
		t.Fatal(err)
	}
	facts.Grant.AllowRead = false
	if _, err := VerifyAssistantCall(call, body, facts); err != AssistantDenied {
		t.Fatalf("read without scope: %v", err)
	}
	call.CorrelationID = "different-request"
	facts.Grant.AllowRead = true
	if _, err := VerifyAssistantCall(call, body, facts); err != AssistantUnverified {
		t.Fatalf("unsigned reply correlation: %v", err)
	}
}
