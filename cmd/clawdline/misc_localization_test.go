package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMiscCatalogShipsCompleteNineLanguageBaseline(t *testing.T) {
	bundle := cliCatalogGroups["misc"]
	if len(bundle.baseline) != 217 {
		t.Fatalf("misc initial baseline has %d keys, expected 217", len(bundle.baseline))
	}
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("misc", language)
		minimum := len(bundle.baseline)
		if language == "en" || language == "zh-Hant" {
			minimum = total
		}
		if total < len(bundle.baseline) || translated < minimum {
			t.Errorf("misc catalog %s = %d/%d; expected at least %d translated keys", language, translated, total, minimum)
		}
	}
}

func TestMiscLocalizedRefusalKeepsOutcomeAndCommands(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	commandLanguage = "zh-Hant"
	var stderr bytes.Buffer
	if code := checkDispatchFlags(&stderr, dispatchOptions{}); code != 2 {
		t.Fatalf("missing title status = %d", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "--title") || !strings.Contains(got, "未派送任何任務") {
		t.Fatalf("localized dispatch refusal = %q", got)
	}
	commandLanguage = "en"
	stderr.Reset()
	checkDispatchFlags(&stderr, dispatchOptions{})
	if got := stderr.String(); got != "clawdline dispatch: --title is required. Nothing was dispatched.\n" {
		t.Fatalf("English dispatch refusal changed: %q", got)
	}
}

func TestMiscWebhookUsageKeepsMachineOutputContract(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	commandLanguage = "zh-Hant"
	var output bytes.Buffer
	webhookFireUsage(&output)
	got := output.String()
	if !strings.Contains(got, "用法：clawdline webhook fire") || !strings.Contains(got, "success|failed|not_delivered|refused|gave_up delivery=<id|->") {
		t.Fatalf("localized usage or machine contract changed: %q", got)
	}
}

func TestMiscPortHolderKeepsObservedIdentity(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	commandLanguage = "zh-Hant"
	got := describeHolderAnswers(context.Background(), "127.0.0.1", 7727,
		func(_ context.Context, path string) (int, []byte, error) {
			if strings.HasSuffix(path, "/v1/health") {
				return 200, []byte(`{"served_by":"raw-daemon-id"}`), nil
			}
			if strings.HasSuffix(path, "/") {
				return 200, nil, nil
			}
			return 0, nil, errors.New("unexpected path")
		})
	if !strings.Contains(got, "raw-daemon-id") || !strings.Contains(got, "在 / 提供主控台") {
		t.Fatalf("localized holder observation = %q", got)
	}
}
