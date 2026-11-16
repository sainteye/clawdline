package projectassistant

import (
	"encoding/json"
	"strings"
	"testing"
)

// upstreamEventBounds are the values pinned in Codync schedule.rs; the product
// registers its own when it wires event triggers.
var upstreamEventBounds = RoutineEventLimits{EventNameBytes: 200, Filters: 20, FilterKeyBytes: 200, FilterValueBytes: 4000}

func TestRoutineEventSpecValidation(t *testing.T) {
	ok := RoutineEventSpec{Source: "github", Event: "pull_request.opened", Filters: map[string]string{"repo": "o/r"}}
	if err := ok.Validate(upstreamEventBounds); err != nil {
		t.Fatalf("valid spec refused: %v", err)
	}
	for _, source := range RoutineEventSources() {
		if err := (RoutineEventSpec{Source: source, Event: "*"}).Validate(upstreamEventBounds); err != nil {
			t.Fatalf("pinned source %q refused: %v", source, err)
		}
	}
	tooMany := map[string]string{}
	for i := range 21 {
		tooMany[strings.Repeat("k", i+1)] = "v"
	}
	atFilters := map[string]string{}
	for i := range 20 {
		atFilters[strings.Repeat("k", i+1)] = "v"
	}
	if err := (RoutineEventSpec{Source: "slack", Event: "x", Filters: atFilters}).Validate(upstreamEventBounds); err != nil {
		t.Fatalf("filters at the bound refused: %v", err)
	}
	at := RoutineEventSpec{Source: "slack", Event: strings.Repeat("e", 200), Filters: map[string]string{strings.Repeat("k", 200): strings.Repeat("v", 4000)}}
	if err := at.Validate(upstreamEventBounds); err != nil {
		t.Fatalf("values at the bound refused: %v", err)
	}
	cases := []struct {
		name  string
		spec  RoutineEventSpec
		code  ErrorCode
		field string
	}{
		{"unknown source", RoutineEventSpec{Source: "GitHub", Event: "push"}, Unsupported, "routine_event_source"},
		{"empty source", RoutineEventSpec{Event: "push"}, Unsupported, "routine_event_source"},
		{"empty event", RoutineEventSpec{Source: "github"}, Invalid, "routine_event_name"},
		{"long event", RoutineEventSpec{Source: "github", Event: strings.Repeat("e", 201)}, Invalid, "routine_event_name"},
		{"non-UTF-8 event", RoutineEventSpec{Source: "github", Event: "\xff"}, Invalid, "routine_event_name"},
		{"too many filters", RoutineEventSpec{Source: "github", Event: "push", Filters: tooMany}, Invalid, "routine_event_filters"},
		{"empty key", RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{"": "v"}}, Invalid, "routine_event_filter_key"},
		{"long key", RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{strings.Repeat("k", 201): "v"}}, Invalid, "routine_event_filter_key"},
		{"long value", RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{"k": strings.Repeat("v", 4001)}}, Invalid, "routine_event_filter_value"},
		{"non-UTF-8 value", RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{"k": "\xc3"}}, Invalid, "routine_event_filter_value"},
	}
	for _, c := range cases {
		err := c.spec.Validate(upstreamEventBounds)
		f, _ := err.(*Failure)
		if f == nil || f.Code != c.code || f.Field != c.field {
			t.Errorf("%s: got %v, want %s %s", c.name, err, c.code, c.field)
		}
	}
	for _, bad := range []RoutineEventLimits{{}, {EventNameBytes: 1, Filters: -1, FilterKeyBytes: 1, FilterValueBytes: 1}, {EventNameBytes: 1, FilterKeyBytes: 1}} {
		if err := ok.Validate(bad); Code(err) != Invalid {
			t.Errorf("unusable bounds %+v accepted: %v", bad, err)
		}
	}
	// A spec with no filters is valid under a bound of zero filters.
	if err := (RoutineEventSpec{Source: "email", Event: "*"}).Validate(RoutineEventLimits{EventNameBytes: 1, FilterKeyBytes: 1, FilterValueBytes: 1}); err != nil {
		t.Fatalf("zero-filter bound refused an unfiltered spec: %v", err)
	}
	if got := RoutineEventSources(); len(got) != 8 {
		t.Fatalf("pinned sources changed: %v", got)
	} else {
		got[0] = "mutated"
		if RoutineEventSources()[0] != "slack" {
			t.Fatal("the source list is mutable through its accessor")
		}
	}
}

func TestRoutineEventSpecDecodesTheUpstreamTaggedShape(t *testing.T) {
	var tagged struct {
		Type string `json:"type"`
		RoutineEventSpec
	}
	raw := `{"type":"event","source":"github","event":"push","filters":{"repo":"o/r"}}`
	if err := json.Unmarshal([]byte(raw), &tagged); err != nil {
		t.Fatal(err)
	}
	if tagged.Type != "event" || tagged.Source != "github" || tagged.Event != "push" || tagged.Filters["repo"] != "o/r" {
		t.Fatalf("decoded %+v", tagged)
	}
	out, _ := json.Marshal(RoutineEventSpec{Source: "github", Event: "push"})
	if string(out) != `{"source":"github","event":"push"}` {
		t.Fatalf("encoded %s", out)
	}
}

func TestRoutineEventMatching(t *testing.T) {
	event := func(raw string) RoutineEvent {
		var e RoutineEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	spec := RoutineEventSpec{Source: "github", Event: "pull_request.opened", Filters: map[string]string{"repo": "owner/repo", "messageContains": "login"}}
	good := event(`{"source":"github","event":"pull_request.opened","repo":"owner/repo","text":"Fix login flow"}`)
	if !spec.Matches(good) {
		t.Fatal("matching event refused")
	}
	misses := map[string]string{
		"other source":         `{"source":"slack","event":"pull_request.opened","repo":"owner/repo","text":"Fix login flow"}`,
		"missing source":       `{"event":"pull_request.opened","repo":"owner/repo","text":"Fix login flow"}`,
		"other event":          `{"source":"github","event":"pull_request.closed","repo":"owner/repo","text":"Fix login flow"}`,
		"event prefix only":    `{"source":"github","event":"pull_request","repo":"owner/repo","text":"Fix login flow"}`,
		"other repo":           `{"source":"github","event":"pull_request.opened","repo":"other/repo","text":"Fix login flow"}`,
		"repo case differs":    `{"source":"github","event":"pull_request.opened","repo":"Owner/repo","text":"Fix login flow"}`,
		"missing repo":         `{"source":"github","event":"pull_request.opened","text":"Fix login flow"}`,
		"text lacks substring": `{"source":"github","event":"pull_request.opened","repo":"owner/repo","text":"Fix logout"}`,
		"text case differs":    `{"source":"github","event":"pull_request.opened","repo":"owner/repo","text":"Fix LOGIN"}`,
		"missing text":         `{"source":"github","event":"pull_request.opened","repo":"owner/repo"}`,
		"numeric repo":         `{"source":"github","event":"pull_request.opened","repo":7,"text":"Fix login"}`,
		"object repo":          `{"source":"github","event":"pull_request.opened","repo":{"full_name":"owner/repo"},"text":"Fix login"}`,
		"null text":            `{"source":"github","event":"pull_request.opened","repo":"owner/repo","text":null}`,
		"array text":           `{"source":"github","event":"pull_request.opened","repo":"owner/repo","text":["login"]}`,
		"numeric source":       `{"source":1,"event":"pull_request.opened","repo":"owner/repo","text":"login"}`,
		"boolean event":        `{"source":"github","event":true,"repo":"owner/repo","text":"login"}`,
		"nested field only":    `{"source":"github","event":"pull_request.opened","payload":{"repo":"owner/repo"},"text":"login"}`,
	}
	for name, raw := range misses {
		if spec.Matches(event(raw)) {
			t.Errorf("%s matched", name)
		}
	}
	wild := RoutineEventSpec{Source: "github", Event: "*"}
	if !wild.Matches(event(`{"source":"github","event":"anything"}`)) || !wild.Matches(event(`{"source":"github"}`)) {
		t.Fatal("wildcard did not match every event of its source")
	}
	if wild.Matches(event(`{"source":"slack","event":"anything"}`)) {
		t.Fatal("wildcard matched another source")
	}
	if !(RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{"messageContains": ""}}).Matches(event(`{"source":"github","event":"push","text":""}`)) {
		t.Fatal("an empty messageContains must match any string text, as upstream")
	}
	if (RoutineEventSpec{Source: "github", Event: "push", Filters: map[string]string{"messageContains": ""}}).Matches(event(`{"source":"github","event":"push"}`)) {
		t.Fatal("an empty messageContains matched an event without text")
	}
	// A literal "*" event in data is only a value: a non-wildcard spec still needs equality.
	if (RoutineEventSpec{Source: "github", Event: "push"}).Matches(event(`{"source":"github","event":"*"}`)) {
		t.Fatal("an event named * matched a specific spec")
	}
	if (RoutineEventSpec{Source: "github", Event: "push"}).Matches(nil) {
		t.Fatal("nil event matched")
	}
}
