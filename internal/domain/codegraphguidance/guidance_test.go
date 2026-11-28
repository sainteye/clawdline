package codegraphguidance

import (
	"strings"
	"testing"
)

func TestLaunchAndSessionGuidanceAreServedFromOneDocument(t *testing.T) {
	if Version() == "" {
		t.Fatal("missing guidance version")
	}
	launch := LaunchText()
	if !strings.Contains(launch, "clawdline codegraph query") || !strings.Contains(launch, "untrusted data") {
		t.Fatalf("unexpected launch text: %q", launch)
	}
	for _, lang := range []string{"en", "zh-Hant"} {
		if Text(lang, true) == "" || Text(lang, false) == "" {
			t.Fatalf("missing session guidance for %s", lang)
		}
	}
}
