package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGuideRoutesShipsCurrentCatalogInBothLanguages(t *testing.T) {
	for _, args := range [][]string{{"en", "routes"}, {"zh-Hant", "routes"}} {
		var out, errs bytes.Buffer
		if code := printGuide(&out, &errs, args); code != 0 {
			t.Fatalf("guide %v: code %d: %s", args, code, errs.String())
		}
		body := out.String()
		for _, want := range []string{"guide-version: ", "/v1/board", "clawdline guide board", "docs/user/board.md"} {
			if !strings.Contains(body, want) {
				t.Errorf("guide %v missing %q", args, want)
			}
		}
	}
}
