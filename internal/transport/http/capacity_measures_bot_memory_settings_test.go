package http

import "testing"

func TestBotSettingsCapacityReadsDurableValuesAndUnknown(t *testing.T) {
	f := newPAServerFixture(t, true)
	measures := f.s.projectAssistantCapacityMeasures()
	for _, name := range []string{"bots.config_options", "bots.config_value_bytes", "bots.login_profiles", "bots.extensions"} {
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
	if _, e := db.Exec(`INSERT INTO project_assistant_login_profiles(source,id,agent,name,state,revision,removed) VALUES('synthetic','one','codex','Fixture one','signed_out',1,0),('synthetic','two','codex','Fixture two','signed_out',1,0)`); e != nil {
		t.Fatal(e)
	}
	if r := measures["bots.login_profiles"](); !r.Known || r.Used != 2 {
		t.Fatal("real persisted profile count", r)
	}
	if _, e := db.Exec(`UPDATE project_assistant_bots SET data=json_set(CAST(data AS TEXT),'$.Bot.extensions',json('{"skills":[{"id":"one","fingerprint":"synthetic"}],"connectors":[{"id":"two","fingerprint":"synthetic"}]}'))`); e != nil {
		t.Fatal(e)
	}
	if r := measures["bots.extensions"](); !r.Known || r.Used != 2 {
		t.Fatal("real persisted extension count", r)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bots.config_options", "bots.config_value_bytes", "bots.login_profiles", "bots.extensions"} {
		r := measures[name]()
		if r.Known || r.Err == "" {
			t.Fatal("unreadable settings invented zero", name, r)
		}
	}
}
