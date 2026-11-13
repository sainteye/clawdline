package cloudops

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

// The vocabulary table (internal/domain/cloudvocab) against this catalog.
//
// Each row is driven through the op's own decode and route, not compared with
// a second description of them: every shape the table lists must decode, and
// must become exactly the local request the row says; every body one field
// away from a listed shape must be refused, so a field added to or dropped
// from a decoder and not from the table fails here. The console half of the
// same table is `web/console/src/cloud/vocabulary.test.ts`.
func TestTheVocabularyTableIsTheBridgesCatalog(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]cloudvocab.Operation{}
	for _, row := range ops {
		rows[row.Op] = row
	}
	for _, word := range missing(catalog, rows) {
		t.Errorf("this machine registers %q and the vocabulary table has no row for it", word)
	}
	for _, word := range missing(rows, catalog) {
		t.Errorf("the vocabulary table has a row for %q and this machine registers no such word", word)
	}
	// A body one field larger is tried with every field any word carries, so
	// a decoder that starts taking a neighbour's field fails here.
	fields := map[string]bool{"unexpected_field": true}
	examples := map[string]any{"unexpected_field": "x"}
	for _, row := range ops {
		for _, shape := range row.Shapes {
			for _, field := range shape.Body {
				fields[field] = true
			}
		}
		for key, value := range row.Example {
			if _, ok := examples[key]; !ok {
				examples[key] = value
			}
		}
	}
	implemented := set(Implemented())
	for _, row := range ops {
		o, ok := catalog[row.Op]
		if !ok {
			continue
		}
		t.Run(row.Op, func(t *testing.T) { checkVocabularyRow(t, row, o, implemented[row.Op], fields, examples) })
	}
}

func checkVocabularyRow(t *testing.T, row cloudvocab.Operation, o op, implemented bool, fields map[string]bool, examples map[string]any) {
	if got := row.Kind == cloudvocab.Read; got != o.read {
		t.Errorf("the table says kind %s and the op says read=%v", row.Kind, o.read)
	}
	if want := !o.read && !o.readLevel; row.WriteGate != want {
		t.Errorf("the table says write_gate=%v and the op asks for the write switch: %v", row.WriteGate, want)
	}
	if asked := row.Console != cloudvocab.NoMachineRoute; asked != implemented {
		t.Errorf("the table says console %s and this machine implements it: %v", row.Console, implemented)
	}
	specials := vocabularySpecials(o)
	// An open body is the one special no op field names: it is what the
	// decoder does with a field it was never told about.
	if len(row.Shapes) > 0 {
		with := map[string]any{"unexpected_field": "x"}
		for key, value := range row.Example {
			with[key] = value
		}
		if _, ok := decodeVocabularyShape(t, o, append(append([]string{}, row.Shapes[0].Body...), "unexpected_field"), with); ok {
			specials = append(specials, "open_body")
			sort.Strings(specials)
		}
	}
	open := false
	for _, special := range specials {
		open = open || special == "open_body"
	}
	if got, want := strings.Join(row.Special, ","), strings.Join(specials, ","); got != want {
		t.Errorf("the table says special [%s] and the op is [%s]", got, want)
	}
	if (row.Local != nil) != (o.route != nil) {
		t.Fatalf("the table says local=%v and the op has a route: %v", row.Local != nil, o.route != nil)
	}

	listed := map[string]bool{}
	for _, shape := range row.Shapes {
		listed[strings.Join(shape.Body, ",")] = true
	}
	for _, shape := range row.Shapes {
		example := row.Example
		if shape.Example != nil {
			example = shape.Example
		}
		p, ok := decodeVocabularyShape(t, o, shape.Body, example)
		if !ok {
			t.Errorf("shape %v is refused", shape.Body)
			continue
		}
		if o.route != nil {
			checkVocabularyLocal(t, row, shape, example, o.route(p))
		}
		// One field away from an accepted body is refused, unless the table
		// lists that body too.
		for _, neighbour := range neighbours(shape.Body, fields) {
			if listed[strings.Join(neighbour, ",")] || open && len(neighbour) > len(shape.Body) {
				continue
			}
			with := map[string]any{}
			for _, source := range []map[string]any{examples, row.Example, example} {
				for key, value := range source {
					with[key] = value
				}
			}
			if _, ok := decodeVocabularyShape(t, o, neighbour, with); ok {
				t.Errorf("body %v is accepted and the table does not list it", neighbour)
			}
		}
	}
}

func decodeVocabularyShape(t *testing.T, o op, fields []string, example map[string]any) (plan, bool) {
	t.Helper()
	object := map[string]any{"type": o.name}
	for _, field := range fields {
		object[field] = example[field]
	}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("example %v: %v", fields, err)
	}
	parsed, err := decodeBody(raw)
	if err != nil {
		t.Fatalf("example %v: %v", fields, err)
	}
	return o.decode(parsed)
}

func checkVocabularyLocal(t *testing.T, row cloudvocab.Operation, shape cloudvocab.Shape, example map[string]any, req LocalRequest) {
	t.Helper()
	if req.Method != row.Local.Method {
		t.Errorf("shape %v: the table says %s and the route is %s", shape.Body, row.Local.Method, req.Method)
	}
	if !vocabularyPathMatches(shape.Path, req.Path, example) {
		t.Errorf("shape %v: the table says path %s and the route is %s", shape.Body, shape.Path, req.Path)
	}
	if device := req.Header[actorHeader] == actorDevice; device != row.Local.Device {
		t.Errorf("shape %v: the table says device=%v and the route says %v", shape.Body, row.Local.Device, device)
	}
	headers := []string{}
	for name := range req.Header {
		if name != actorHeader {
			headers = append(headers, name)
		}
	}
	sort.Strings(headers)
	if got, want := strings.Join(headers, ","), strings.Join(row.Local.Headers, ","); got != want {
		t.Errorf("shape %v: the table says headers [%s] and the route sets [%s]", shape.Body, want, got)
	}
	query := []string{}
	for name := range req.Query {
		query = append(query, name)
	}
	sort.Strings(query)
	if got, want := strings.Join(query, ","), strings.Join(shape.Query, ","); got != want {
		t.Errorf("shape %v: the table says query [%s] and the route asks [%s]", shape.Body, want, got)
	}
	var local *[]string
	if req.Body != nil {
		var object map[string]any
		if err := json.Unmarshal(req.Body, &object); err != nil {
			t.Fatalf("shape %v: the local body is not a JSON object: %q", shape.Body, req.Body)
		}
		keys := []string{}
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		local = &keys
	}
	switch {
	case (local == nil) != (shape.LocalBody == nil):
		t.Errorf("shape %v: the table says local body %v and the route sends %v", shape.Body, shape.LocalBody != nil, local != nil)
	case local != nil && strings.Join(*local, ",") != strings.Join(*shape.LocalBody, ","):
		t.Errorf("shape %v: the table says local body [%s] and the route sends [%s]", shape.Body,
			strings.Join(*shape.LocalBody, ","), strings.Join(*local, ","))
	}
}

var vocabularyPlaceholder = regexp.MustCompile(`\{([a-z_]+(?:\.[a-z_]+)*)(\*?)\}`)

// vocabularyPathMatches reads a path template: a `{field}` segment must carry
// that example field, escaped as the route escapes it.
func vocabularyPathMatches(template, path string, example map[string]any) bool {
	names := []string{}
	pattern := "^"
	rest := template
	for {
		at := vocabularyPlaceholder.FindStringSubmatchIndex(rest)
		if at == nil {
			pattern += regexp.QuoteMeta(rest)
			break
		}
		pattern += regexp.QuoteMeta(rest[:at[0]])
		names = append(names, rest[at[2]:at[3]])
		if at[5] > at[4] {
			pattern += "(.+)"
		} else {
			pattern += "([^/]+)"
		}
		rest = rest[at[1]:]
	}
	found := regexp.MustCompile(pattern + "$").FindStringSubmatch(path)
	if found == nil {
		return false
	}
	for i, name := range names {
		value, err := url.PathUnescape(found[i+1])
		if err != nil {
			return false
		}
		switch want := body(example).genAt(name).(type) {
		case string:
			if value != want {
				return false
			}
		case json.Number:
			if value != want.String() {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// neighbours is every key set one field away from shape: one dropped, or one
// of fields added.
func neighbours(shape []string, fields map[string]bool) [][]string {
	out := [][]string{}
	has := map[string]bool{}
	for i, field := range shape {
		has[field] = true
		out = append(out, append(append([]string{}, shape[:i]...), shape[i+1:]...))
	}
	for field := range fields {
		if !has[field] {
			added := append(append([]string{}, shape...), field)
			sort.Strings(added)
			out = append(out, added)
		}
	}
	return out
}

// vocabularySpecials is every way an op is handled outside decode-then-route,
// by the names the table uses, sorted.
func vocabularySpecials(o op) []string {
	out := []string{}
	add := func(when bool, name string) {
		if when {
			out = append(out, name)
		}
	}
	add(o.anyClass, "any_class")
	add(contentReadWord(o.name), "content_read")
	add(o.divergence != "", "divergence")
	add(o.guard != nil, "guard")
	add(o.partial != nil, "partial_answer")
	add(o.readLevel, "read_level")
	add(o.name == "session-receipt", "receipt_lookup")
	if o.refusal != nil {
		out = append(out, "refused:"+o.refusal.Code)
	}
	add(o.sessions, "session_publisher")
	add(sessionMutation(o.name), "session_receipt")
	add(o.shape != nil, "shaped_answer")
	add(o.route == nil && !o.sessions && o.name != "session-receipt" && o.name != viewerEventsWord, "unavailable")
	add(o.name == viewerEventsWord, "viewer_events")
	sort.Strings(out)
	return out
}

func set(words []string) map[string]bool {
	out := map[string]bool{}
	for _, word := range words {
		out[word] = true
	}
	return out
}

// missing is every key of `want` that `have` does not hold, sorted, whichever
// of the two map shapes each side is.
func missing[A any, B any](want map[string]A, have map[string]B) []string {
	out := []string{}
	for word := range want {
		if _, ok := have[word]; !ok {
			out = append(out, word)
		}
	}
	sort.Strings(out)
	return out
}
