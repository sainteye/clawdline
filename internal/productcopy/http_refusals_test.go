package productcopy

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestHTTPRefusalKeyNamesOnlyKnownExactEnglishDetails(t *testing.T) {
	const fixed = "that is not a session action"
	digest := sha256.Sum256([]byte(fixed))
	if got, want := HTTPRefusalKey(fixed), fmt.Sprintf("http.%x", digest[:8]); got != want {
		t.Fatalf("fixed detail key = %q, want %q", got, want)
	}
	if got := HTTPRefusalKey("that is not a session action for this account"); got != "" {
		t.Fatalf("dynamic detail gained a key: %q", got)
	}
}

func TestHTTPRefusalTextRequiresExactWireDetailAndKnownKey(t *testing.T) {
	const english = "that is not a session action"
	key := HTTPRefusalKey(english)
	if got := HTTPRefusalText("zh-TW", key, english); got == english || !strings.Contains(got, "Session") {
		t.Fatalf("Traditional Chinese refusal = %q", got)
	}
	for _, test := range []struct{ language, key, detail string }{
		{"de", "", english},
		{"de", key, "an unrelated detail"},
		{"ru", key, english},
		{"de", "http.unknown", english},
	} {
		if got := HTTPRefusalText(test.language, test.key, test.detail); got != test.detail {
			t.Errorf("%+v rendered %q", test, got)
		}
	}
	if translated, total := HTTPRefusalCoverage("zh-Hant"); translated != 1283 || total != 1283 {
		t.Fatalf("Traditional Chinese coverage = %d/%d", translated, total)
	}
	const handoffDetail = "The handoff package could not be written; nothing was opened."
	handoffKey := HTTPRefusalKey(handoffDetail)
	if handoffKey == "" || HTTPRefusalText("zh-Hant", handoffKey, handoffDetail) == handoffDetail ||
		HTTPRefusalText("ja", handoffKey, handoffDetail) != handoffDetail {
		t.Fatal("handoff storage refusal must translate in Traditional Chinese and fall back in Japanese")
	}
	const updateDetail = "force needs the version it installs"
	updateKey := HTTPRefusalKey(updateDetail)
	if updateKey == "" || HTTPRefusalText("zh-Hant", updateKey, updateDetail) == updateDetail ||
		HTTPRefusalText("ja", updateKey, updateDetail) != updateDetail {
		t.Fatal("new update refusal must translate in Traditional Chinese and fall back to English in Japanese")
	}
}

func TestHTTPRefusalCatalogRejectsBrokenPresentEntriesAtomically(t *testing.T) {
	english := map[string]string{"old": "Use {name}\x1fDo not use {name}", "new": "New copy"}
	baseline := map[string]bool{"old": true}
	for _, selected := range []map[string]string{
		{"new": "新しい文"},
		{"old": "名前を使う\x1f名前を使わない"},
		{"old": "名前を使う {name} {name}\x1f名前を使わない {name}"},
		{"old": "名前を使う {name}\x1f名前を使わない {name}", "extra": "余分"},
		{"old": "名前を使う {name}\x1f名前を使わない {name}", "new": ""},
	} {
		if validHTTPRefusalCatalog(english, selected, baseline, false) {
			t.Errorf("accepted malformed catalog: %#v", selected)
		}
	}
	partial := map[string]string{"old": "名前を使う {name}\x1f名前を使わない {name}"}
	if !validHTTPRefusalCatalog(english, partial, baseline, false) || validHTTPRefusalCatalog(english, partial, baseline, true) {
		t.Fatal("secondary new-key fallback or Traditional Chinese completeness changed")
	}
}

func TestNoteWithdrawalRefusalsHaveTraditionalChineseCopy(t *testing.T) {
	for _, detail := range []string{"A withdrawal reason is required within the note text limit.", "This note is already closed; its existing outcome is preserved."} {
		key := HTTPRefusalKey(detail)
		if key == "" || HTTPRefusalText("zh-Hant", key, detail) == detail {
			t.Fatalf("missing withdrawal translation: %s", detail)
		}
	}
}

func TestAntigravityFirstInputRefusalsHaveTraditionalChineseCopy(t *testing.T) {
	for _, detail := range []string{
		"Antigravity's prepared launch could not be verified. No input was sent.",
		"This Antigravity launch has already reserved its first input. Refresh the Session before continuing.",
		"The first-input attempt could not be confirmed. Refresh the Session; it will not be retried automatically.",
	} {
		key := HTTPRefusalKey(detail)
		if key == "" || HTTPRefusalText("zh-Hant", key, detail) == detail || HTTPRefusalText("ja", key, detail) != detail {
			t.Fatalf("first-input translation or fallback missing: %q", detail)
		}
	}
}
