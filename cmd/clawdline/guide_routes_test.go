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

func TestGuideCapacityShipsRegisteredBoundsInBothLanguages(t *testing.T) {
	for _, args := range [][]string{{"en", "capacity"}, {"zh-Hant", "capacity"}} {
		var out, errs bytes.Buffer
		if code := printGuide(&out, &errs, args); code != 0 {
			t.Fatalf("guide %v: code %d: %s", args, code, errs.String())
		}
		body := out.String()
		for _, want := range []string{"guide-version: ", "artifacts.images", "board.receipts", "work_gate_tasks_per_round", "GET /v1/capacity"} {
			if !strings.Contains(body, want) {
				t.Errorf("guide %v missing %q", args, want)
			}
		}
	}
}

func TestGuideRefusalsShipsTypedCodesInBothLanguages(t *testing.T) {
	for _, args := range [][]string{{"en", "refusals"}, {"zh-Hant", "refusals"}} {
		var out, errs bytes.Buffer
		if code := printGuide(&out, &errs, args); code != 0 {
			t.Fatalf("guide %v: code %d: %s", args, code, errs.String())
		}
		body := out.String()
		for _, want := range []string{"guide-version: ", "acceptance_required", "closeability_unknown", "guide refused <code>"} {
			if !strings.Contains(body, want) {
				t.Errorf("guide %v missing %q", args, want)
			}
		}
	}
}
