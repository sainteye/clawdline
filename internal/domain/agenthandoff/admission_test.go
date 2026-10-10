package agenthandoff

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func validRequestAndFacts() (Request, []byte, Facts) {
	body := []byte("hello")
	bodySum := sha256.Sum256(body)
	request := Request{
		RequestID: "request-1", Kind: Message,
		Source:     Endpoint{"machine-a", "session-a", "11111111111111111111111111111111"},
		Target:     Endpoint{"machine-b", "session-b", "22222222222222222222222222222222"},
		GrantID:    "grant-1",
		BodyDigest: hex.EncodeToString(bodySum[:]),
	}
	facts := Facts{
		LocalMachineID: "machine-b",
		Principal:      Principal{MachineID: "machine-a", KeyFingerprint: "peer-key-1", PairID: "pair-1", MachinePeer: true},
		PairActive:     true,
		GrantReadable:  true,
		Grant: Grant{
			ID: "grant-1", PairID: "pair-1", SourceMachineID: "machine-a", SourceSessionID: "session-a",
			SourceGeneration: "11111111111111111111111111111111",
			TargetMachineID:  "machine-b", TargetSessionID: "session-b",
			TargetGeneration:   "22222222222222222222222222222222",
			PeerKeyFingerprint: "peer-key-1", AllowMessage: true, AllowHandoff: true,
			ExpiresAt: time.Unix(2000, 0),
		},
		PeerCapability: true, LocalCapability: true, MachineWritesAllowed: true, TargetCurrent: true,
		Now: time.Unix(1000, 0),
	}
	return request, body, facts
}

func TestAdmissionRequiresExplicitMachinePeerAndCurrentGrant(t *testing.T) {
	request, body, facts := validRequestAndFacts()
	if err := Admit(request, body, facts); err != nil {
		t.Fatalf("valid message refused: %v", err)
	}
	request.Kind = Handoff
	if err := Admit(request, body, facts); err != nil {
		t.Fatalf("valid handoff refused: %v", err)
	}
	if got := Admit(request, []byte("changed"), facts); got != InvalidRequest {
		t.Fatalf("tampered body = %v, want %v", got, InvalidRequest)
	}
	cases := []struct {
		name string
		edit func(*Request, *Facts)
		want Refusal
	}{
		{"no source Session", func(r *Request, _ *Facts) { r.Source.SessionID = "" }, InvalidRequest},
		{"no target generation", func(r *Request, _ *Facts) { r.Target.ExecutionGeneration = "" }, InvalidRequest},
		{"changed generation spelling", func(r *Request, _ *Facts) { r.Target.ExecutionGeneration = "ABC" }, InvalidRequest},
		{"no body digest", func(r *Request, _ *Facts) { r.BodyDigest = "" }, InvalidRequest},
		{"wrong body digest", func(r *Request, _ *Facts) {
			r.BodyDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}, InvalidRequest},
		{"same account viewer", func(_ *Request, f *Facts) { f.Principal.MachinePeer = false }, SourceUnverified},
		{"claimed different source", func(r *Request, _ *Facts) { r.Source.MachineID = "machine-c" }, SourceUnverified},
		{"wrong receiver", func(_ *Request, f *Facts) { f.LocalMachineID = "machine-c" }, WrongMachine},
		{"inactive pair", func(_ *Request, f *Facts) { f.PairActive = false }, PairInactive},
		{"unreadable grants", func(_ *Request, f *Facts) { f.GrantReadable = false }, GrantUnavailable},
		{"revoked grant", func(_ *Request, f *Facts) { f.Grant.Revoked = true }, GrantRevoked},
		{"expired grant", func(_ *Request, f *Facts) { f.Now = time.Unix(2000, 0) }, GrantExpired},
		{"different pair", func(_ *Request, f *Facts) { f.Principal.PairID = "pair-2" }, GrantDenied},
		{"different source Session", func(r *Request, _ *Facts) { r.Source.SessionID = "session-c" }, GrantDenied},
		{"different source generation", func(r *Request, _ *Facts) { r.Source.ExecutionGeneration = "33333333333333333333333333333333" }, GrantDenied},
		{"different target Session", func(r *Request, _ *Facts) { r.Target.SessionID = "session-c" }, GrantDenied},
		{"different target generation", func(r *Request, _ *Facts) { r.Target.ExecutionGeneration = "33333333333333333333333333333333" }, GrantDenied},
		{"different pinned key", func(_ *Request, f *Facts) { f.Principal.KeyFingerprint = "new-key" }, GrantDenied},
		{"message scope missing", func(r *Request, f *Facts) { r.Kind = Message; f.Grant.AllowMessage = false }, GrantDenied},
		{"handoff scope missing", func(r *Request, f *Facts) { r.Kind = Handoff; f.Grant.AllowHandoff = false }, GrantDenied},
		{"peer capability absent", func(_ *Request, f *Facts) { f.PeerCapability = false }, CapabilityUnavailable},
		{"local capability absent", func(_ *Request, f *Facts) { f.LocalCapability = false }, CapabilityUnavailable},
		{"writes off", func(_ *Request, f *Facts) { f.MachineWritesAllowed = false }, MachineWritesDisabled},
		{"generation changed or unknown", func(_ *Request, f *Facts) { f.TargetCurrent = false }, ExecutionUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, body, f := validRequestAndFacts()
			tc.edit(&r, &f)
			if got := Admit(r, body, f); got != tc.want {
				t.Fatalf("Admit = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMachinePairCoversCurrentSessionsAndBothKinds(t *testing.T) {
	request, body, facts := validRequestAndFacts()
	request.GrantID = facts.Principal.PairID
	request.Source.SessionID = "another-source"
	request.Target.SessionID = "another-target"
	facts.MachineAccess, facts.GrantReadable = true, false
	for _, kind := range []Kind{Message, Handoff} {
		request.Kind = kind
		if err := Admit(request, body, facts); err != nil {
			t.Fatalf("%s on signed machine pair refused: %v", kind, err)
		}
	}
	facts.TargetCurrent = false
	if got := Admit(request, body, facts); got != ExecutionUnverified {
		t.Fatalf("changed target execution = %v", got)
	}
	facts.TargetCurrent = true
	facts.PairActive = false
	if got := Admit(request, body, facts); got != PairInactive {
		t.Fatalf("revoked pair = %v", got)
	}
	facts.PairActive = true
	request.GrantID = "another-pair"
	if got := Admit(request, body, facts); got != GrantDenied {
		t.Fatalf("different pair = %v", got)
	}
}

func TestPeerTextMustRoundTripThroughTheDurableInbox(t *testing.T) {
	request, _, _ := validRequestAndFacts()
	body := []byte{0xff}
	sum := sha256.Sum256(body)
	request.BodyDigest = hex.EncodeToString(sum[:])
	if got := Validate(request, body); got != InvalidRequest {
		t.Fatalf("invalid UTF-8 body with a valid digest = %v, want %v", got, InvalidRequest)
	}
}

func TestReceiptIdentityConflictsOnChangedRequest(t *testing.T) {
	request, _, _ := validRequestAndFacts()
	actor, key, digest := ReceiptIdentity(request)
	actor2, key2, digest2 := ReceiptIdentity(request)
	if actor != actor2 || key != key2 || digest != digest2 {
		t.Fatal("the same request did not produce one durable receipt identity")
	}
	changes := []struct {
		name string
		edit func(*Request)
	}{
		{"generation", func(r *Request) { r.Target.ExecutionGeneration = "33333333333333333333333333333333" }},
		{"grant", func(r *Request) { r.GrantID = "grant-2" }},
		{"operation", func(r *Request) { r.Kind = Handoff }},
		{"body", func(r *Request) { r.BodyDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			changed := request
			tc.edit(&changed)
			a, k, d := ReceiptIdentity(changed)
			if a != actor || k != key || d == digest {
				t.Fatalf("changed request escaped the original receipt lane: actor=%q key=%q digest=%q", a, k, d)
			}
		})
	}
}
