package transcript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClaudeModelsFollowTheInstalledPickerCatalog(t *testing.T) {
	home := t.TempDir()
	state := `{
		"additionalModelOptionsCache":[{"value":"claude-fable-5-1[1m]","label":"Fable"}],
		"clientDataCacheSlots":{
			"opus":{"model":"claude-opus-5-5"},
			"sonnet":{"model":"claude-sonnet-5"},
			"haiku":{"model":"claude-haiku-4-5-20251001"}
		}
	}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "claude")
	installed := "claude-opus-5-5[1m] claude-fable-5-1 claude-fable-5 " +
		"claude-sonnet-5[1m] claude-sonnet-4-6 claude-haiku-4-5-20251001 " +
		"claude-opus-5[1m] claude-opus-4-8[1m] claude-opus-4-7[1m] " +
		"claude-opus-4-6[1m] claude-opus-4-5 claude-opus-4-20250514 claude-opus-4-1"
	if err := os.WriteFile(executable, []byte(installed), 0o700); err != nil {
		t.Fatal(err)
	}

	want := []Model{
		{ID: "claude-opus-5-5", Name: "Opus 5.5", Command: "opus"},
		{ID: "claude-fable-5-1", Name: "Fable 5.1", Command: "fable"},
		{ID: "claude-sonnet-5", Name: "Sonnet 5", Command: "sonnet"},
		{ID: "claude-haiku-4-5", Name: "Haiku 4.5", Command: "haiku"},
		{ID: "claude-fable-5", Name: "Fable 5", Command: "claude-fable-5"},
		{ID: "claude-opus-5", Name: "Opus 5", Command: "opus5"},
		{ID: "claude-opus-4-8", Name: "Opus 4.8", Command: "opus48"},
		{ID: "claude-opus-4-7", Name: "Opus 4.7", Command: "opus47"},
		{ID: "claude-opus-4-6", Name: "Opus 4.6", Command: "opus46"},
		{ID: "claude-opus-4-5", Name: "Opus 4.5", Command: "opus45"},
	}
	if got := claudeModels(home, executable); !reflect.DeepEqual(got, want) {
		t.Fatalf("models:\n got %#v\nwant %#v", got, want)
	}
}

func TestClaudeModelsDoNotGuessVersionsWithoutClaudeCode(t *testing.T) {
	want := []Model{
		{Name: "Opus", Command: "opus"},
		{Name: "Fable", Command: "fable"},
		{Name: "Sonnet", Command: "sonnet"},
		{Name: "Haiku", Command: "haiku"},
	}
	if got := claudeModels(t.TempDir(), filepath.Join(t.TempDir(), "missing")); !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback: %#v", got)
	}
}

func TestClaudeModelCatalogDoesNotPromoteAnInstalledButUnavailablePreview(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{
		"model":"claude-opus-5-5",
		"additionalModelOptionsCache":[{"value":"claude-fable-5-1[1m]"}],
		"projects":{"a-name":{"model":"claude-opus-6"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(executable, []byte(
		"claude-opus-6 claude-opus-5-5 claude-fable-6 claude-fable-5-1 "+
			"claude-sonnet-5 claude-haiku-4-5"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := claudeModels(home, executable)
	for _, model := range got {
		if model.ID == "claude-opus-6" || model.ID == "claude-fable-6" {
			t.Fatalf("unavailable preview escaped the account cache: %#v", got)
		}
	}
}
