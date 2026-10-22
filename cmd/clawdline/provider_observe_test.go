package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestProviderObserveRejectsArgumentsAndFailureWithoutStdout(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	for _, lang := range []string{"en", "zh-Hant"} {
		commandLanguage = lang
		for _, args := range [][]string{nil, {"antigravity", "--event"}, {"antigravity", "--pid", "123"}, {"antigravity", "--event", "PreInvocation", "--capability", "private"}} {
			var out, errs bytes.Buffer
			if code := runProviderObserve(context.Background(), args, "/synthetic-absent", nil, strings.NewReader(`{}`), &out, &errs); code != 2 || out.Len() != 0 || errs.Len() == 0 {
				t.Fatalf("invalid args code/output: %d %q %q", code, out.String(), errs.String())
			}
		}
		var out, errs bytes.Buffer
		if code := runProviderObserve(context.Background(), []string{"antigravity", "--event", "PreInvocation"}, "/synthetic-absent", nil, strings.NewReader(`{}`), &out, &errs); code != 1 || out.Len() != 0 || strings.Contains(errs.String(), "/synthetic-absent") {
			t.Fatalf("failed hook code/output: %d %q %q", code, out.String(), errs.String())
		}
		if lang == "zh-Hant" && !strings.Contains(errs.String(), "失敗") {
			t.Fatal("Traditional Chinese failure missing")
		}
	}
}
