package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// TestModelChoiceACPPeer runs only as the owned child of the tests below: a
// scripted Codex adapter that records every set_config_option it is sent. It
// makes no vendor, model or network request.
func TestModelChoiceACPPeer(t *testing.T) {
	if os.Getenv("CLAWDLINE_MODEL_CHOICE_PEER") == "" {
		return
	}
	shape := os.Getenv("CLAWDLINE_MODEL_CHOICE_PEER")
	model, effort := "gpt-5.4-mini/high", "medium"
	if shape == "plain" {
		model = "model-sol"
	}
	config := func() map[string]any {
		if shape == "plain" {
			return map[string]any{"configOptions": []any{
				map[string]any{"id": "model", "category": "model", "type": "select", "name": "Model", "currentValue": model, "options": []any{
					map[string]string{"value": "model-sol", "name": "6.2 Sol"}, map[string]string{"value": "model-astra", "name": "6.2 Astra"}}},
				map[string]any{"id": "reasoning_effort", "category": "thought_level", "type": "select", "name": "Effort", "currentValue": effort, "options": []any{
					map[string]string{"value": "medium", "name": "Medium"}, map[string]string{"value": "high", "name": "High"}}},
			}}
		}
		return map[string]any{"configOptions": []any{
			map[string]any{"id": "model", "category": "model", "type": "select", "name": "Model", "currentValue": model, "options": []any{
				map[string]string{"value": "gpt-5.5/medium", "name": "gpt-5.5 (medium)"}, map[string]string{"value": "gpt-5.5/high", "name": "gpt-5.5 (high)"},
				map[string]string{"value": "gpt-5.4-mini/high", "name": "gpt-5.4-mini (high)"}}},
		}}
	}
	record, _ := os.OpenFile(os.Getenv("CLAWDLINE_MODEL_CHOICE_RECORD"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer record.Close()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var f struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch f.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": adapterPackage(pa.Codex), "version": "scripted"}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}, "sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}}}
		case "session/new":
			v := config()
			v["sessionId"] = "model-choice-session"
			result = v
		case "session/set_config_option":
			var id, value string
			_ = json.Unmarshal(f.Params["configId"], &id)
			_ = json.Unmarshal(f.Params["value"], &value)
			_, _ = record.WriteString(id + "=" + value + "\n")
			_ = record.Sync()
			if id == "model" {
				model = value
			} else if id == "reasoning_effort" {
				effort = value
			}
			result = config()
		case "session/cancel":
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": result})
	}
	os.Exit(0)
}

func modelChoiceProvider(t *testing.T, shape string) (*Provider, app.OpenRequest, string) {
	t.Helper()
	p, q, _ := configuredProvider(t, pa.Codex, "unused")
	record := filepath.Join(t.TempDir(), "set.txt")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	options := p.o
	options.Configurations = map[pa.Agent]Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestModelChoiceACPPeer$"},
		Environment: []string{"CLAWDLINE_MODEL_CHOICE_PEER=" + shape, "CLAWDLINE_MODEL_CHOICE_RECORD=" + record, "GORACE=atexit_sleep_ms=0"}}}
	p, err = NewProvider(options)
	if err != nil {
		t.Fatal(err)
	}
	return p, q, record
}

func recorded(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// A Bot save validates its choice in a discovery session; a catalog model
// without an effort is Codex's per-effort entry with the effort that runs.
func TestASavedCatalogModelIsValidatedAsItsACPValue(t *testing.T) {
	p, q, record := modelChoiceProvider(t, "per-effort")
	catalog, err := p.Configuration(context.Background(), q.Bot, q.Directory, "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if got := recorded(t, record); got != "model=gpt-5.5/high" || catalog.Model != "gpt-5.5/high" {
		t.Fatalf("set %q, model %q", got, catalog.Model)
	}
	if _, err = p.Configuration(context.Background(), q.Bot, q.Directory, "gpt-9"); pa.Code(err) != pa.Unsupported {
		t.Fatalf("an unlisted model was not refused: %v", err)
	}
}

// The Bot's own session applies the saved choice: a catalog model with an
// effort the agent takes as its own option sets both.
func TestABotSessionAppliesACatalogModelAndEffort(t *testing.T) {
	p, q, record := modelChoiceProvider(t, "plain")
	q.Bot.RuntimeSettings = pa.RuntimeSettings{Model: "model-astra/high"}
	if _, err := p.Capabilities(context.Background(), q.Bot); err != nil {
		t.Fatal(err)
	}
	session, err := p.Open(context.Background(), q, func(context.Context, app.ProtocolEvent) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.o.Limits.CleanupTimeout)
		defer cancel()
		_ = session.Close(ctx)
	}()
	if got := recorded(t, record); got != "model=model-astra\nreasoning_effort=high" {
		t.Fatalf("set %q", got)
	}
}

// A saved ACP value from before the catalog is applied exactly as saved.
func TestASavedACPValueIsAppliedAsSaved(t *testing.T) {
	p, q, record := modelChoiceProvider(t, "per-effort")
	if _, err := p.Configuration(context.Background(), q.Bot, q.Directory, "gpt-5.5/medium"); err != nil {
		t.Fatal(err)
	}
	if got := recorded(t, record); got != "model=gpt-5.5/medium" {
		t.Fatalf("set %q", got)
	}
}
