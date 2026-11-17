package http

import "testing"

func TestBotSettingsCapacityReadsDurableValuesAndUnknown(t *testing.T) {
	f := newPAServerFixture(t, true)
	measures := f.s.projectAssistantCapacityMeasures()
	for _, name := range []string{"bots.config_options", "bots.config_value_bytes"} {
		r := measures[name]()
		if !r.Known || r.Used != 0 {
			t.Fatal("absent optional settings", name, r)
		}
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Bot.runtime_settings',json('{"model":"synthetic-large","options":{"effort":"high","fast":"true"}}'))`); err != nil {
		t.Fatal(err)
	}
	if r := measures["bots.config_options"](); !r.Known || r.Used != 2 {
		t.Fatal("real durable options", r)
	}
	if r := measures["bots.config_value_bytes"](); !r.Known || r.Used != 15 {
		t.Fatal("real durable UTF-8 value bytes", r)
	}
	if _, err := db.Exec(`UPDATE project_assistant_bots SET data=json_remove(CAST(data AS TEXT),'$.Bot.runtime_settings')`); err != nil {
		t.Fatal(err)
	}
	if r := measures["bots.config_options"](); !r.Known || r.Used != 0 {
		t.Fatal("cleared settings", r)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bots.config_options", "bots.config_value_bytes"} {
		r := measures[name]()
		if r.Known || r.Err == "" {
			t.Fatal("unreadable settings invented zero", name, r)
		}
	}
}
