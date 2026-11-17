package botcapabilities

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

func TestSelectedDefinitionsAreScopedAndRevocationChangesTheFingerprint(t *testing.T) {
	dir := t.TempDir()
	if e := os.Mkdir(filepath.Join(dir, "project-assistants"), 0700); e != nil {
		t.Fatal(e)
	}
	scope := pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.PlaceProject}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "source", BotID: "bot"}, Project: scope, Agent: pa.Codex}
	skill := squad.Skill{SkillID: "fixture", Name: squad.Names{En: "Synthetic installed skill"}, Content: "Synthetic selected skill instructions."}
	connector := Connector{ID: "fixture", Name: "fixture-connector", URL: "http://127.0.0.1:1/mcp", Agent: pa.Codex, Project: scope, Enabled: true}
	save := func() {
		b, _ := json.Marshal([]Connector{connector})
		if e := os.WriteFile(filepath.Join(dir, "project-assistants", "connectors.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	save()
	i := Inventory{State: dir, Skills: func(context.Context) ([]squad.Skill, error) { return []squad.Skill{skill}, nil }}
	c, e := i.Catalog(context.Background(), bot, "")
	if e != nil || len(c.Items) != 2 {
		t.Fatal(c, e)
	}
	for _, o := range c.Items {
		ref := pa.ExtensionRef{ID: o.ID, Fingerprint: o.Fingerprint}
		if o.Kind == "skill" {
			bot.Extensions.Skills = append(bot.Extensions.Skills, ref)
		} else {
			bot.Extensions.Connectors = append(bot.Extensions.Connectors, ref)
		}
	}
	resolved, e := i.Resolve(context.Background(), bot, "")
	if e != nil || len(resolved.Connectors) != 1 || resolved.Skills == "" {
		t.Fatal(resolved, e)
	}
	other := bot
	other.Project.ProjectID = "other"
	if _, e = i.Resolve(context.Background(), other, ""); pa.Code(e) != pa.Revoked {
		t.Fatal("foreign Project borrowed a connector", e)
	}
	other = bot
	other.Agent = pa.ClaudeCode
	if _, e = i.Resolve(context.Background(), other, ""); pa.Code(e) != pa.Revoked {
		t.Fatal("foreign Agent borrowed a connector", e)
	}
	skill.Content = "A changed installed definition"
	if _, e = i.Resolve(context.Background(), bot, ""); pa.Code(e) != pa.Revoked {
		t.Fatal("stale skill reached context", e)
	}
	bot.Extensions.Skills = nil
	connector.Enabled = false
	save()
	if _, e = i.Resolve(context.Background(), bot, ""); pa.Code(e) != pa.Revoked {
		t.Fatal("revoked connector reached launch", e)
	}
	bot.Extensions.Connectors = nil
	if out, e := i.Resolve(context.Background(), bot, ""); e != nil || out.Skills != "" || len(out.Connectors) != 0 {
		t.Fatal("deselection retained a capability", out, e)
	}
	if e = os.WriteFile(filepath.Join(dir, "project-assistants", "connectors.json"), []byte(`[{"URL":"http://fixture.invalid","env":{"PATH":"bad"}}]`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = i.Catalog(context.Background(), bot, ""); e == nil {
		t.Fatal("arbitrary connector environment accepted")
	}
}
