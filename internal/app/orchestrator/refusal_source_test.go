package orchestrator

import (
	"encoding/json"
	"testing"
)

func TestBundledPersonaAndEffortListsAreFixedRefusals(t *testing.T) {
	for _, admit := range []func() error{
		func() error { _, err := admitPersona(json.RawMessage(`"wizard"`)); return err },
		func() error { _, err := admitReasoningEffort(json.RawMessage(`"medium"`), "codex"); return err },
	} {
		ref, ok := admit().(Refusal)
		if !ok || ref.RawMessage || ref.Message == "" {
			t.Fatalf("bundled list refusal lost its fixed source: %#v", ref)
		}
	}
}

func TestWaitValidationCarriesFixedAndRuntimeMessageOrigins(t *testing.T) {
	_, err := normaliseWait(WaitRequest{})
	fixed, ok := err.(Refusal)
	if !ok || fixed.RawMessage || fixed.Message != "repository must be an absolute path." {
		t.Fatalf("fixed validation refusal = %#v", err)
	}

	_, err = normaliseWait(WaitRequest{
		Repository: "/repo", Paths: []string{"../outside"},
		Owner: "owner", Waiter: "waiter", Reason: "needs work", ReleaseCondition: "after merge",
	})
	runtime, ok := err.(Refusal)
	if !ok || !runtime.RawMessage {
		t.Fatalf("path-derived validation refusal = %#v", err)
	}
}

func TestLandingProofPreservesFixedAndRuntimeMessageOrigins(t *testing.T) {
	const detail = "The delivery branch could not be read."
	fixed := unverified(UnverifiedDelivery, detail)
	runtime := unverifiedRaw(UnverifiedDelivery, detail)
	if fixed.RawMessage || !runtime.RawMessage || fixed.Code != runtime.Code ||
		fixed.Status != runtime.Status || fixed.Message != runtime.Message ||
		fixed.Extra["reason"] != runtime.Extra["reason"] {
		t.Fatalf("landing proof origins diverged: fixed=%#v runtime=%#v", fixed, runtime)
	}
}
