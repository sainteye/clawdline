package cloudops

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// What the generated decoders (zz_decode_generated.go) are written in. Each
// one is the vocabulary table's `decode` for its word, spelled out by
// tools/contract-gen; these are the few pieces they share, so that a rule in
// the table is read by one function here rather than by a hundred copies.
//
// Every word the table states the decoder of is served by its generated
// decoder (register). `TestGeneratedDecodersKeepTheGolden` holds each one to
// what the hand-written decoder it replaced answered.

// genDecoder is one generated decoder: the plan's identity fields (where the
// answer goes and what it is pinned to), the local request, and whether the
// body was accepted at all.
type genDecoder func(b body) (plan, LocalRequest, bool)

// genRule is one body value's rule, as the table's `cloudvocab.Field` states it.
type genRule struct {
	kind       string
	nonEmpty   bool
	maxBytes   int
	length     int
	prefix     string
	min, max   *int64
	enum       []string
	equals     *bool
	checks     []func(string) bool
	nullable   bool
	sameAs     string
	items      *genRule
	minItems   *int
	maxItems   *int
	properties map[string]genRule
	required   []string
	closed     bool
	minKeys    int
	maxKeys    int
}

// withGeneratedDecoder gives o the decoder the table states for its word, and
// a route that hands on the local request that decoder built.
func withGeneratedDecoder(o op) op {
	decode := generatedDecoders[o.name]
	o.decode = func(b body) (plan, bool) {
		p, req, ok := decode(b)
		p.local = req
		return p, ok
	}
	if !generatedUnrouted[o.name] {
		o.route = routeLocal
	}
	return o
}

// routeLocal is the request a generated decoder built. Its header map is a
// copy: the bridge adds headers to what a route returns.
func routeLocal(p plan) LocalRequest {
	req := p.local
	if req.Header != nil {
		header := make(map[string]string, len(req.Header))
		for key, value := range req.Header {
			header[key] = value
		}
		req.Header = header
	}
	return req
}

func genInt(v int64) *int64 { return &v }
func genCount(v int) *int   { return &v }
func genBool(v bool) *bool  { return &v }

// genNumber is a table constant number, held as a decoded body holds one.
func genNumber(v string) json.Number { return json.Number(v) }

// genValid is whether the body's value for key keeps its rule. An absent key
// keeps every rule: which keys must be present is the shapes' question.
func (b body) genValid(key string, r genRule) bool {
	v, present := b[key]
	return !present || genValueValid(b, v, r)
}

func genValueValid(b body, v any, r genRule) bool {
	if v == nil && r.nullable {
		return true
	}
	switch r.kind {
	case "any":
		return true
	case "string":
		s, ok := v.(string)
		if !ok || r.nonEmpty && s == "" || r.maxBytes > 0 && len(s) > r.maxBytes ||
			r.length > 0 && len(s) != r.length || !strings.HasPrefix(s, r.prefix) {
			return false
		}
		if len(r.enum) > 0 {
			known := false
			for _, candidate := range r.enum {
				known = known || s == candidate
			}
			if !known {
				return false
			}
		}
		for _, check := range r.checks {
			if !check(s) {
				return false
			}
		}
		if r.sameAs != "" {
			value, present := b.genLookup(r.sameAs)
			other, ok := value.(string)
			return present && ok && other == s
		}
		return true
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		i, err := n.Int64()
		return err == nil && (r.min == nil || i >= *r.min) && (r.max == nil || i <= *r.max)
	case "bool":
		t, ok := v.(bool)
		return ok && (r.equals == nil || t == *r.equals)
	case "list":
		items, ok := v.([]any)
		if !ok || r.minItems != nil && len(items) < *r.minItems || r.maxItems != nil && len(items) > *r.maxItems {
			return false
		}
		for _, item := range items {
			if !genValueValid(b, item, *r.items) {
				return false
			}
		}
		return true
	case "object":
		object, ok := v.(map[string]any)
		if !ok || len(object) < r.minKeys || r.maxKeys > 0 && len(object) > r.maxKeys {
			return false
		}
		for _, key := range r.required {
			if _, present := object[key]; !present {
				return false
			}
		}
		for key, value := range object {
			rule, known := r.properties[key]
			if !known {
				if r.closed {
					return false
				}
				continue
			}
			if !genValueValid(b, value, rule) {
				return false
			}
		}
		if r.maxBytes > 0 {
			encoded, err := json.Marshal(object)
			return err == nil && len(encoded) <= r.maxBytes
		}
		return true
	}
	return false
}

// genText is a value as a path, a query or an answer name spells it.
func genText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return itoa(n)
		}
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return ""
}

// genLookup reads only a fixed field path emitted from the vocabulary table.
// Direct names retain their original behavior; nested paths traverse objects.
func (b body) genLookup(path string) (any, bool) {
	var value any = map[string]any(b)
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func (b body) genAt(path string) any { value, _ := b.genLookup(path); return value }

// genValue is where one local value comes from (`cloudvocab.Value`).
type genValue struct {
	from      string
	constant  any
	fallback  any
	omitEmpty bool
	only      string
	ifTrue    string
	list      bool
}

// genSource is the value v names in b, and whether it is sent at all.
func (b body) genSource(v genValue) (any, bool) {
	out := v.constant
	if v.from != "" {
		raw, present := b.genLookup(v.from)
		if !present {
			if v.fallback == nil {
				return nil, false
			}
			raw = v.fallback
		}
		out = raw
	}
	if v.omitEmpty {
		switch t := out.(type) {
		case string:
			if t == "" {
				return nil, false
			}
		case bool:
			if !t {
				return nil, false
			}
		case json.Number:
			if genText(t) == "0" {
				return nil, false
			}
		}
	}
	if v.only != "" && genText(out) != v.only {
		return nil, false
	}
	if v.ifTrue != "" {
		if t, _ := out.(bool); !t {
			return nil, false
		}
		out = v.ifTrue
	}
	if v.list {
		out = []any{out}
	}
	return out, true
}

// genQuery puts each sent value into a route's query.
func (b body) genQuery(values map[string]genValue) map[string]string {
	out := map[string]string{}
	for key, v := range values {
		if value, sent := b.genSource(v); sent {
			out[key] = genText(value)
		}
	}
	return out
}

// genFields is a local JSON body built from values.
func (b body) genFields(values map[string]genValue) []byte {
	out := map[string]any{}
	for key, v := range values {
		if value, sent := b.genSource(v); sent {
			out[key] = value
		}
	}
	return jsonBody(out)
}

// genObject is a body field's object as the local body, as it arrived.
func (b body) genObject(key string) []byte {
	out, err := json.Marshal(b[key])
	if err != nil {
		return []byte("{}")
	}
	return out
}

// The named checks of `cloudvocab.Checks`, each over a string.

func genRequestName(s string) bool { _, ok := requestName(s); return ok }
func genSessionName(s string) bool { _, ok := sessionName(s); return ok }
func genFingerprint(s string) bool { return session.ValidFingerprint(s) }
func genDocumentPath(s string) bool {
	_, ok := documentPath(s)
	return ok
}
func genTreeFile(s string) bool {
	_, err := projectfiles.TreePath(s, true)
	return err == nil
}
func genTreeDirectory(s string) bool {
	_, err := projectfiles.TreePath(s, false)
	return err == nil
}
func genUsageID(s string) bool        { return usageID.MatchString(s) && !strings.Contains(s, "..") }
func genVerificationID(s string) bool { return verificationID.MatchString(s) }
func genCompareSince(s string) bool   { return compareSince.MatchString(s) }
func genWorkUntil(s string) bool      { return workUntil.MatchString(s) }
func genAbsolutePath(s string) bool   { return filepath.IsAbs(s) }
func genNotBlank(s string) bool       { return strings.TrimSpace(s) != "" }
func genLowerHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
