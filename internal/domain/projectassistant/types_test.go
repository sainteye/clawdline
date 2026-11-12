package projectassistant

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

type wireSchema struct {
	Ref        string                 `json:"$ref"`
	Type       string                 `json:"type"`
	Enum       []string               `json:"enum"`
	Properties map[string]*wireSchema `json:"properties"`
	Required   []string               `json:"required"`
	Items      *wireSchema            `json:"items"`
}

func validateWire(value any, s *wireSchema, defs map[string]*wireSchema, path string) error {
	if s.Ref != "" {
		s = defs[strings.TrimPrefix(s.Ref, "#/$defs/")]
	}
	if s == nil {
		return fmt.Errorf("%s: missing schema", path)
	}
	switch s.Type {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: not object", path)
		}
		for _, p := range s.Required {
			if _, ok := obj[p]; !ok {
				return fmt.Errorf("%s: missing %s", path, p)
			}
		}
		for p, v := range obj {
			child, ok := s.Properties[p]
			if !ok {
				return fmt.Errorf("%s: unknown %s", path, p)
			}
			if err := validateWire(v, child, defs, path+"."+p); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: not array", path)
		}
		for _, v := range items {
			if err := validateWire(v, s.Items, defs, path+"[]"); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: not string", path)
		}
		if len(s.Enum) > 0 {
			found := false
			for _, v := range s.Enum {
				found = found || v == text
			}
			if !found {
				return fmt.Errorf("%s: unknown enum %q", path, text)
			}
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || n != float64(int64(n)) {
			return fmt.Errorf("%s: not integer", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: not boolean", path)
		}
	default:
		return fmt.Errorf("%s: unsupported schema type %s", path, s.Type)
	}
	return nil
}
func TestDomainWireShapesAndGeneratedGoAgree(t *testing.T) {
	raw, err := os.ReadFile("../../../api/v1/project-assistants.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]*wireSchema `json:"$defs"`
	}
	decode(t, raw, &schema)
	f := testFence()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	bot := Bot{Key: f.Key, Project: f.Project, ConversationID: f.ConversationID, Name: "Helper", Appearance: Appearance{Blob, EntryColor}, Agent: Codex, PermissionPolicy: Ask, Instructions: InstructionSnapshot{Revision: 1, Text: "Plan only", RoleSnapshotRef: "role@1"}, Version: 1, CreatedAt: now, UpdatedAt: now}
	message := Message{ID: "message-a", Fence: f, Seq: 1, TurnID: "turn-a", Kind: AssistantMessage, Text: "Visible", At: now}
	for name, value := range map[string]any{
		"Bot": bot, "Message": message, "Capabilities": testCapabilities(),
		"Event":        Event{Version: ContractVersion, ID: "event-a", Seq: 1, Fence: f, Kind: MessageCreated, At: now, Message: &message},
		"SourceStatus": SourceStatus{MachineID: "machine-a", State: SourceOnline, Readable: true, Writable: true, Fresh: true, ObservedAt: now, Access: Interactive, Capabilities: []Capabilities{testCapabilities()}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var wire any
			decode(t, raw, &wire)
			if err := validateWire(wire, schema.Defs["ProjectAssistant"+name], schema.Defs, name); err != nil {
				t.Fatal(err)
			}
		})
	}
	encoded, err := json.Marshal(bot)
	if err != nil {
		t.Fatal(err)
	}
	var generated contract.ProjectAssistantBot
	decode(t, encoded, &generated)
	encoded, err = json.Marshal(generated)
	if err != nil {
		t.Fatal(err)
	}
	var restored Bot
	decode(t, encoded, &restored)
	if !reflect.DeepEqual(restored, bot) {
		t.Fatalf("generated round trip lost settings: %+v", restored)
	}
	// A malformed cross-language fixture must fail for the intended reason.
	var bad any
	decode(t, []byte(`{"shape":"invented","color":"#e39567"}`), &bad)
	if err := validateWire(bad, schema.Defs["ProjectAssistantAppearance"], schema.Defs, "appearance"); err == nil || !strings.Contains(err.Error(), "unknown enum") {
		t.Fatal("schema validation accepted unknown appearance", err)
	}
}
func TestSameNamesNeverEstablishBotOrProjectIdentity(t *testing.T) {
	a := Bot{Key: BotKey{"machine-a", "bot-a"}, Name: "Helper", Project: ProjectScope{"machine-a", "project-a", "repo-a", RepositoryProject}}
	b := a
	b.Key.MachineID = "machine-b"
	b.Project.MachineID = "machine-b"
	c := a
	c.Key.BotID = "bot-b"
	set := map[BotKey]Bot{a.Key: a, b.Key: b, c.Key: c}
	if len(set) != 3 {
		t.Fatal("same names collapsed distinct Bots")
	}
	if a.Project == b.Project {
		t.Fatal("same named Project collapsed across machines")
	}
}
func TestReplayEventsRequireOneMatchingPayload(t *testing.T) {
	f := testFence()
	e := Event{Version: ContractVersion, ID: "event-a", Seq: 1, Fence: f, Kind: TraceUpdated, Trace: &Trace{TurnID: "turn-a", Text: "Synthetic trace"}}
	if err := ValidateEvent(e); err != nil {
		t.Fatal(err)
	}
	e.Message = &Message{ID: "message-a"}
	if Code(ValidateEvent(e)) != Invalid {
		t.Fatal("ambiguous replay payload accepted")
	}
	e.Trace = nil
	if Code(ValidateEvent(e)) != Invalid {
		t.Fatal("wrong typed replay payload accepted")
	}
	e.Message = nil
	e.Kind = FeatureDisabled
	if err := ValidateEvent(e); err != nil {
		t.Fatal(err)
	}
	e.Version = "future-version"
	if Code(ValidateEvent(e)) != Invalid {
		t.Fatal("unknown event version accepted")
	}
}

func TestSharedEventEnvelopeFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			ID       string
			Input    Event
			Expected struct{ Code ErrorCode }
		} `json:"event_cases"`
	}
	decode(t, raw, &fixture)
	if len(fixture.Cases) == 0 {
		t.Fatal("no event fixtures")
	}
	for _, c := range fixture.Cases {
		t.Run(c.ID, func(t *testing.T) {
			if err := ValidateEvent(c.Input); Code(err) != c.Expected.Code {
				t.Fatalf("%v, want %s", err, c.Expected.Code)
			}
		})
	}
}
