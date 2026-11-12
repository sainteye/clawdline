package cloudvocab

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNestedRouteFieldsAreDeclaredObjects(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	var row Operation
	for _, candidate := range ops {
		if candidate.Op == "project-assistants.send" {
			row = candidate
		}
	}
	if row.Op == "" {
		t.Fatal("Bot send is absent")
	}
	if field, ok := FieldAt(row.Decode.Fields, "command.key.bot_id"); !ok || field.Type != TypeString {
		t.Fatal("the generated route cannot read the declared Bot key")
	}
	for _, path := range []string{"command.credential", "command.key.bot_id.secret", "fence.project.cwd"} {
		if _, ok := FieldAt(row.Decode.Fields, path); ok {
			t.Fatalf("undeclared field %s", path)
		}
		cloned, _ := json.Marshal(row)
		var changed Operation
		if err := json.Unmarshal(cloned, &changed); err != nil {
			t.Fatal(err)
		}
		changed.Decode.Route.Headers["Idempotency-Key"] = Value{From: path}
		if err := validateDecode(changed); err == nil {
			t.Fatalf("undeclared source %s accepted", path)
		}
		changed.Decode.Route.Headers["Idempotency-Key"] = Value{From: "command.idempotency_key"}
		changed.Decode.Route.Path = "/v1/project-assistants/{" + path + "}"
		if err := validateDecode(changed); err == nil {
			t.Fatalf("undeclared path %s accepted", path)
		}
	}
}

func TestTheEmbeddedTableReads(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) < 100 {
		t.Fatalf("the table holds %d operations; the bridge has well over a hundred", len(ops))
	}
}

// A table that contradicts itself is refused whole, never read in part.
func TestATableThatContradictsItselfIsRefused(t *testing.T) {
	for name, edit := range map[string]func([]byte) []byte{
		"an unknown field": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"write_gate"`), []byte(`"write_gates"`), 1)
		},
		"a word twice": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"op": "archived-sessions"`), []byte(`"op": "archive-session"`), 1)
		},
		"a kind nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"kind": "read"`), []byte(`"kind": "peek"`), 1)
		},
		"another version": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 2`), 1)
		},
		"a check nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"checks": ["generation"]`), []byte(`"checks": ["generations"]`), 1)
		},
		"a special nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"special": ["content_read"]`), []byte(`"special": ["content_reads"]`), 1)
		},
		"a refusal special that is free text": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"refused:cloud_dispatch_unpinned"`), []byte(`"refused: no wire shape"`), 1)
		},
		"a rule for a field no shape names": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"decode": {"reply": "machine", "route": {"path": "/v1/sessions/archived"}}`),
				[]byte(`"decode": {"reply": "machine", "fields": {"ghost": {"type": "string"}}, "route": {"path": "/v1/sessions/archived"}}`), 1)
		},
		"a row with no decode": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"decode": {"reply": "machine", "route": {"path": "/v1/sessions/archived"}}`),
				[]byte(`"decode": null`), 1)
		},
		"a hand row that states a decode too": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"decode": {"hand": "its body is open`), []byte(`"decode": {"reply": "machine", "hand": "its body is open`), 1)
		},
		"a route path naming a field no shape names": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"route": {"path": "/v1/sessions/archived"}`), []byte(`"route": {"path": "/v1/sessions/{ghost}"}`), 1)
		},
		"a string rule on an integer": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"limit": {"type": "integer", "min": 1, "max": 1000}`),
				[]byte(`"limit": {"type": "integer", "min": 1, "max": 1000, "max_bytes": 4}`), 1)
		},
		"an unrouted word with no sentence": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"console_sentence": "This machine does not take a diagnostic report over Clawdline Cloud.",`), nil, 1)
		},
		"a sentence too short to act on": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"console_sentence": "This machine does not take a diagnostic report over Clawdline Cloud."`),
				[]byte(`"console_sentence": "Not carried."`), 1)
		},
		"a carried word that says why it is not": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"console_route": "GET /v1/capacity",`),
				[]byte(`"console_route": "GET /v1/capacity", "console_sentence": "The capacity block is not read over Clawdline Cloud yet: open it on the machine.",`), 1)
		},
		"a device word that claims this machine's exemption": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"local": {"method": "POST", "local_exemption": true,`),
				[]byte(`"local": {"method": "POST", "device": true, "local_exemption": true,`), 1)
		},
		"a wrapper check nobody knows": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"strip_check": "generation"`), []byte(`"strip_check": "generations"`), 1)
		},
	} {
		edited := edit(vocabularyJSON)
		if bytes.Equal(edited, vocabularyJSON) {
			t.Fatalf("%s: the edit did not apply", name)
		}
		if _, err := parse(edited); err == nil || !strings.HasPrefix(err.Error(), "cloudvocab:") {
			t.Errorf("%s was read: %v", name, err)
		}
	}
}
