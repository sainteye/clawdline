package cloudvocab

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Decode is how this machine reads one word's body, as data: which fields
// each may hold, where the answer goes, and the local request the fields
// become. `tools/contract-gen` turns every Decode that is not Hand into a Go
// decoder (internal/app/cloudops/zz_decode_generated.go).
//
// A Decode describes the decoder, not the production wrapper around it: the
// bridge strips `execution_generation` before a session-receipt word is
// decoded, and `machine_id` before a content read is; Wrapper states those
// rules.
type Decode struct {
	// Hand is the one-line reason this word's decoder stays hand-written.
	// A hand row states nothing else.
	Hand  string `json:"hand,omitempty"`
	Reply Reply  `json:"reply,omitempty"`
	// Name is a Session reply's answer name; `{field}` is that field's value.
	Name string `json:"name,omitempty"`
	// NameSuffix is appended to the name when its field is present.
	NameSuffix *NameSuffix `json:"name_suffix,omitempty"`
	// NameSuffixes adds further conditional suffixes while preserving old tables.
	NameSuffixes []NameSuffix `json:"name_suffixes,omitempty"`
	// Fields is the rule for every body field a shape names, except the
	// `session` and `request` the reply reads itself.
	Fields map[string]Field `json:"fields,omitempty"`
	// Route is the local request, for a word with one (Operation.Local).
	Route *Route `json:"route,omitempty"`
}

// Reply is where a word's answer goes and which identity fields it reads.
type Reply string

const (
	// MachineRead is a read answered on the machine reply channel:
	// `session` is that channel and `request` names the answer.
	MachineRead Reply = "machine"
	// SessionRead is a read on the session it names, under Decode.Name.
	SessionRead Reply = "session"
	// SessionRequestRead is a read on the session it names, under the
	// answer name its `request` gives.
	SessionRequestRead Reply = "session_request"
	// MachineAction is a machine command; `request` is optional.
	MachineAction Reply = "action"
	// SessionAction is a command on the session it names.
	SessionAction Reply = "session_action"
)

// AnswerNameSuffixes preserves the legacy suffix before any additional suffixes.
func (d *Decode) AnswerNameSuffixes() []NameSuffix {
	suffixes := make([]NameSuffix, 0, len(d.NameSuffixes)+1)
	if d.NameSuffix != nil {
		suffixes = append(suffixes, *d.NameSuffix)
	}
	return append(suffixes, d.NameSuffixes...)
}

// NameSuffix is text appended to the answer name when Field is present.
type NameSuffix struct {
	Field string `json:"field"`
	Text  string `json:"text"`
}

// FieldType is what JSON value a field holds.
type FieldType string

const (
	TypeString  FieldType = "string"
	TypeInteger FieldType = "integer"
	TypeBool    FieldType = "bool"
	TypeObject  FieldType = "object"
	TypeList    FieldType = "list"
	TypeAny     FieldType = "any"
)

// Field is the rule one body value is held to. A value that breaks it makes
// the whole body malformed.
type Field struct {
	Type     FieldType `json:"type"`
	NonEmpty bool      `json:"non_empty,omitempty"`
	// MaxBytes bounds a string's length, or an object's encoded size.
	MaxBytes int `json:"max_bytes,omitempty"`
	// Length is a string's exact length in bytes.
	Length int    `json:"length,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	// Min and Max bound an integer.
	Min *int64 `json:"min,omitempty"`
	Max *int64 `json:"max,omitempty"`
	// Enum is every string the field may hold.
	Enum []string `json:"enum,omitempty"`
	// Equals is the one value a bool may hold.
	Equals *bool `json:"equals,omitempty"`
	// Checks are named validators, each in Checks.
	Checks []string `json:"checks,omitempty"`
	// Nullable admits null beside the type (object properties only).
	Nullable bool `json:"nullable,omitempty"`
	// SameAs is a top-level body field this string must equal.
	SameAs string `json:"same_as,omitempty"`
	// Items is each list element's rule; MinItems and MaxItems bound the list.
	Items    *Field `json:"items,omitempty"`
	MinItems *int   `json:"min_items,omitempty"`
	MaxItems *int   `json:"max_items,omitempty"`
	// Properties are an object's known keys; Required must be present, and a
	// Closed object refuses any other key. MinKeys and MaxKeys bound its size.
	Properties map[string]Field `json:"properties,omitempty"`
	Required   []string         `json:"required,omitempty"`
	Closed     bool             `json:"closed,omitempty"`
	MinKeys    int              `json:"min_keys,omitempty"`
	MaxKeys    int              `json:"max_keys,omitempty"`
	// Into names the identity field this value also becomes.
	Into Into `json:"into,omitempty"`
	// Copies names, for a limit above, the Go constant its number is a copy
	// of (`max_bytes` → `projectfiles.MaxWriteBytes`), so a test can hold the
	// two equal and a constant cannot change while the table keeps the old
	// number. A limit no constant states names none.
	Copies map[string]string `json:"copies,omitempty"`
}

// CopiedLimit is one limit a Field states and the constant it copies.
type CopiedLimit struct {
	// Path is the field, then `.property` or `[]` down to the rule.
	Path     string
	Limit    string
	Value    int64
	Constant string
}

// CopiedLimits is every limit the rule f at path copies, nested rules
// included.
func CopiedLimits(path string, f Field) []CopiedLimit {
	out := []CopiedLimit{}
	for _, limit := range sortedLimitNames(f.Copies) {
		value, _ := f.limit(limit)
		out = append(out, CopiedLimit{Path: path, Limit: limit, Value: value, Constant: f.Copies[limit]})
	}
	if f.Items != nil {
		out = append(out, CopiedLimits(path+"[]", *f.Items)...)
	}
	keys := make([]string, 0, len(f.Properties))
	for key := range f.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, CopiedLimits(path+"."+key, f.Properties[key])...)
	}
	return out
}

func sortedLimitNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// limit is the number a limit name states, and whether f states it.
func (f Field) limit(name string) (int64, bool) {
	count := func(n int) (int64, bool) { return int64(n), n > 0 }
	pointer := func(n *int64) (int64, bool) {
		if n == nil {
			return 0, false
		}
		return *n, true
	}
	items := func(n *int) (int64, bool) {
		if n == nil {
			return 0, false
		}
		return int64(*n), true
	}
	switch name {
	case "max_bytes":
		return count(f.MaxBytes)
	case "length":
		return count(f.Length)
	case "min":
		return pointer(f.Min)
	case "max":
		return pointer(f.Max)
	case "min_items":
		return items(f.MinItems)
	case "max_items":
		return items(f.MaxItems)
	case "min_keys":
		return count(f.MinKeys)
	case "max_keys":
		return count(f.MaxKeys)
	}
	return 0, false
}

// Into is an identity field a body value is copied to.
type Into string

const (
	// IntoTarget is the session a request acts on.
	IntoTarget Into = "target"
	// IntoExecutionGeneration is the execution the request is pinned to.
	IntoExecutionGeneration Into = "execution_generation"

	// The rest are the few values the answer side reads after the local
	// request is built, so they travel in the plan beside it: a picture's
	// id (named in its payload), an info read's parts (which the content
	// read rail admits only in full), a menu answer's key and question and
	// whether it names typed words (which its guard checks), and a viewer
	// event batch (which is written here rather than routed).
	IntoID       Into = "id"
	IntoParts    Into = "parts"
	IntoKey      Into = "key"
	IntoExpect   Into = "expect"
	IntoTyped    Into = "typed"
	IntoDocument Into = "document"
)

// Checks is every named validator a Field may ask for, and what it holds a
// string to. The decoders implement them; a name outside this list is
// refused when the table is read.
var Checks = map[string]string{
	"printable":       "no control character",
	"request_name":    "a request name: non-empty, at most 128 bytes, printable",
	"session_name":    "a session name: non-empty, at most 256 bytes, printable",
	"task_id":         "a lowercase UUID",
	"fingerprint":     "a menu fingerprint (session.ValidFingerprint)",
	"generation":      "an execution generation: 32 lowercase hex digits",
	"document_path":   "a relative Markdown or text document path, at most six segments",
	"tree_file":       "a project tree file path (projectfiles.TreePath, file)",
	"tree_directory":  "a project tree directory path (projectfiles.TreePath, directory)",
	"usage_id":        "a usage ledger id",
	"verification_id": "a verification id",
	"compare_since":   "a comparison since: digits, then d or h or nothing",
	"work_until":      "a Unix time in seconds",
	"lower_hex":       "lowercase hexadecimal digits only",
	"absolute_path":   "an absolute filesystem path",
	"not_blank":       "something besides white space",
}

// Route is the local request a decoded body becomes. The method and the
// device actor come from Operation.Local.
type Route struct {
	// Path is a template: `{field}` is one escaped segment, `{field*}` a
	// document path escaped per segment, and `{/field?}` a slash and a
	// segment present only when the field is not empty.
	Path    string           `json:"path"`
	Query   map[string]Value `json:"query,omitempty"`
	Headers map[string]Value `json:"headers,omitempty"`
	// Body nil is no body at all.
	Body *Body `json:"body,omitempty"`
}

// Body is a local request body.
type Body struct {
	// Empty is the body `{}`.
	Empty bool `json:"empty,omitempty"`
	// Object is a body field whose object is the whole local body.
	Object string `json:"object,omitempty"`
	// Fields is a JSON object built from values.
	Fields map[string]Value `json:"fields,omitempty"`
}

// Value is where one local value comes from. A value whose field is absent
// is left out, unless it has a Default.
type Value struct {
	From  string `json:"from,omitempty"`
	Const any    `json:"const,omitempty"`
	// Default stands for an absent field.
	Default any `json:"default,omitempty"`
	// OmitEmpty leaves out "", false and 0.
	OmitEmpty bool `json:"omit_empty,omitempty"`
	// Only keeps the value only when it is this string.
	Only string `json:"only,omitempty"`
	// IfTrue spells a true bool as this string and leaves out false.
	IfTrue string `json:"if_true,omitempty"`
	// List carries the value as a one-element list.
	List bool `json:"list,omitempty"`
}

// Wrapper is what the bridge does around a word's decoder in production, by
// the `special` tag that selects it.
type Wrapper struct {
	// Special is the tag on the rows this applies to.
	Special string `json:"special"`
	// Channel is the envelope channel the rule applies on, when not every.
	Channel string `json:"channel,omitempty"`
	// Strip is a body field the bridge requires and removes before decoding.
	Strip string `json:"strip"`
	// StripCheck is the named check the stripped value must pass.
	StripCheck string `json:"strip_check,omitempty"`
	// StripRefusal is the code a missing or invalid stripped field gets.
	StripRefusal string `json:"strip_refusal"`
	// Require is a field the decoded body must have named, and RequireRefusal
	// the code its absence gets.
	Require        string `json:"require,omitempty"`
	RequireRefusal string `json:"require_refusal,omitempty"`
	// Headers are the headers the stripped value becomes on the local request.
	Headers []string `json:"headers,omitempty"`
}

// Idempotency is the key every local request that is not a GET carries.
type Idempotency struct {
	Header string `json:"header"`
	// From is the body field the key is, when the body named it: the
	// answer's request, which a hand row may read from another field.
	From string `json:"from"`
	// Fallback is the key when it did not, in the bridge's own spelling.
	Fallback string `json:"fallback"`
	// Except is the method that carries no key.
	Except string `json:"except"`
}

var templateField = regexp.MustCompile(`\{(/?)([a-z_]+)([*?]?)\}`)

// TemplateFields is every field a path or name template names.
func TemplateFields(template string) []string {
	out := []string{}
	for _, m := range templateField.FindAllStringSubmatch(template, -1) {
		out = append(out, m[2])
	}
	return out
}

func validateDecode(o Operation) error {
	d := o.Decode
	if d == nil {
		return fmt.Errorf("cloudvocab: %s has no decode", o.Op)
	}
	if d.Hand != "" {
		if d.Reply != "" || d.Name != "" || d.NameSuffix != nil || len(d.NameSuffixes) != 0 || d.Fields != nil || d.Route != nil {
			return fmt.Errorf("cloudvocab: %s is decoded by hand and states a decode too", o.Op)
		}
		return nil
	}
	owned := map[string]bool{}
	switch d.Reply {
	case MachineRead, SessionRequestRead, MachineAction, SessionAction:
		owned["session"], owned["request"] = true, true
	case SessionRead:
		owned["session"] = true
	default:
		return fmt.Errorf("cloudvocab: %s has reply %q", o.Op, d.Reply)
	}
	if (d.Name != "") != (d.Reply == SessionRead) {
		return fmt.Errorf("cloudvocab: %s names its answer and is not a session read, or the reverse", o.Op)
	}
	known := func(field string) bool { _, ok := d.Fields[field]; return ok || owned[field] }
	named := map[string]bool{}
	for _, s := range o.Shapes {
		for _, field := range s.Body {
			named[field] = true
			if !known(field) {
				return fmt.Errorf("cloudvocab: %s shape %v names %s, which its decode has no rule for", o.Op, s.Body, field)
			}
		}
	}
	for field, rule := range d.Fields {
		if owned[field] {
			return fmt.Errorf("cloudvocab: %s has a rule for %s, which its reply reads", o.Op, field)
		}
		if !named[field] {
			return fmt.Errorf("cloudvocab: %s has a rule for %s, which no shape names", o.Op, field)
		}
		if err := validateField(rule, known); err != nil {
			return fmt.Errorf("cloudvocab: %s field %s: %w", o.Op, field, err)
		}
	}
	for _, field := range TemplateFields(d.Name) {
		if !named[field] {
			return fmt.Errorf("cloudvocab: %s names its answer by %s, which no shape names", o.Op, field)
		}
	}
	for _, s := range d.AnswerNameSuffixes() {
		if !named[s.Field] {
			return fmt.Errorf("cloudvocab: %s suffixes its name by %s, which no shape names", o.Op, s.Field)
		}
		for _, field := range TemplateFields(s.Text) {
			if !named[field] {
				return fmt.Errorf("cloudvocab: %s suffixes its name with %s, which no shape names", o.Op, field)
			}
		}
	}
	if (d.Route != nil) != (o.Local != nil) {
		return fmt.Errorf("cloudvocab: %s has a local route and no decoded one, or the reverse", o.Op)
	}
	if r := d.Route; r != nil {
		if !strings.HasPrefix(r.Path, "/") {
			return fmt.Errorf("cloudvocab: %s route path %q is not absolute", o.Op, r.Path)
		}
		for _, field := range TemplateFields(r.Path) {
			if !named[field] {
				return fmt.Errorf("cloudvocab: %s route path names %s, which no shape names", o.Op, field)
			}
		}
		values := []Value{}
		headers := []string{}
		for name, v := range r.Headers {
			headers = append(headers, name)
			values = append(values, v)
		}
		sort.Strings(headers)
		if strings.Join(headers, ",") != strings.Join(o.Local.Headers, ",") {
			return fmt.Errorf("cloudvocab: %s route headers [%s] are not its local headers [%s]", o.Op,
				strings.Join(headers, ","), strings.Join(o.Local.Headers, ","))
		}
		for _, v := range r.Query {
			values = append(values, v)
		}
		if b := r.Body; b != nil {
			forms := 0
			if b.Empty {
				forms++
			}
			if b.Object != "" {
				forms++
				if rule, ok := d.Fields[b.Object]; !ok || rule.Type != TypeObject {
					return fmt.Errorf("cloudvocab: %s body is %s, which is not an object field", o.Op, b.Object)
				}
			}
			if b.Fields != nil {
				forms++
				for _, v := range b.Fields {
					values = append(values, v)
				}
			}
			if forms != 1 {
				return fmt.Errorf("cloudvocab: %s body is not exactly one of empty, object or fields", o.Op)
			}
		}
		for _, v := range values {
			if (v.From == "") == (v.Const == nil) {
				return fmt.Errorf("cloudvocab: %s has a value that is not exactly one of from or const", o.Op)
			}
			if v.From != "" && !named[v.From] {
				return fmt.Errorf("cloudvocab: %s takes a value from %s, which no shape names", o.Op, v.From)
			}
		}
	}
	return nil
}

func validateField(f Field, known func(string) bool) error {
	switch f.Type {
	case TypeString, TypeInteger, TypeBool, TypeObject, TypeList, TypeAny:
	default:
		return fmt.Errorf("type %q", f.Type)
	}
	for _, check := range f.Checks {
		if _, ok := Checks[check]; !ok {
			return fmt.Errorf("check %q is not one of cloudvocab.Checks", check)
		}
	}
	stringOnly := f.NonEmpty || f.MaxBytes > 0 && f.Type != TypeObject || f.Length > 0 || f.Prefix != "" ||
		len(f.Enum) > 0 || len(f.Checks) > 0 || f.SameAs != ""
	if stringOnly && f.Type != TypeString {
		return fmt.Errorf("a %s states a string rule", f.Type)
	}
	if (f.Min != nil || f.Max != nil) && f.Type != TypeInteger {
		return fmt.Errorf("a %s states an integer bound", f.Type)
	}
	if f.Equals != nil && f.Type != TypeBool {
		return fmt.Errorf("a %s states a bool value", f.Type)
	}
	if (f.Items != nil || f.MinItems != nil || f.MaxItems != nil) && f.Type != TypeList {
		return fmt.Errorf("a %s states list rules", f.Type)
	}
	if f.Type == TypeList && f.Items == nil {
		return fmt.Errorf("a list states no item rule")
	}
	objectOnly := f.Properties != nil || f.Required != nil || f.Closed || f.MinKeys > 0 || f.MaxKeys > 0 ||
		f.MaxBytes > 0 && f.Type != TypeString
	if objectOnly && f.Type != TypeObject {
		return fmt.Errorf("a %s states an object rule", f.Type)
	}
	if f.SameAs != "" && !known(f.SameAs) {
		return fmt.Errorf("same_as %s names no field", f.SameAs)
	}
	switch f.Into {
	case "", IntoTarget, IntoExecutionGeneration, IntoID, IntoParts, IntoKey, IntoExpect, IntoTyped:
		if f.Into != "" && f.Type != TypeString {
			return fmt.Errorf("a %s goes into %s, which holds a string", f.Type, f.Into)
		}
	case IntoDocument:
		if f.Type != TypeObject {
			return fmt.Errorf("a %s goes into %s, which holds an object", f.Type, f.Into)
		}
	default:
		return fmt.Errorf("into %q", f.Into)
	}
	for limit, constant := range f.Copies {
		if _, stated := f.limit(limit); !stated {
			return fmt.Errorf("copies %s for %s, a limit it does not state", constant, limit)
		}
		if constant == "" {
			return fmt.Errorf("copies no constant for %s", limit)
		}
	}
	if f.Items != nil {
		if err := validateField(*f.Items, known); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	for _, key := range f.Required {
		if _, ok := f.Properties[key]; !ok {
			return fmt.Errorf("requires %s, which it has no rule for", key)
		}
	}
	for key, p := range f.Properties {
		if err := validateField(p, known); err != nil {
			return fmt.Errorf("property %s: %w", key, err)
		}
	}
	return nil
}

// Specials is every tag a row's `special` may hold, and what it means. A
// `refused:<code>` tag names the refusal code the word always gets.
var Specials = map[string]string{
	"any_class":         "accepted under any envelope class, not only ctl",
	"content_read":      "served on the pinned content-read channel",
	"divergence":        "answered differently from the hosted console's producer",
	"guard":             "a decoded request can still be refused before routing",
	"open_body":         "a field the decoder was never told about is accepted",
	"partial_answer":    "a refusal that is also an answer crosses whole",
	"read_level":        "a command served with remote writes switched off",
	"receipt_lookup":    "answered from the session receipt store",
	"session_publisher": "answered by the Session publisher, not a route",
	"session_receipt":   "a session command admitted through a durable receipt",
	"shaped_answer":     "an answer that is not JSON is shaped into one",
	"unavailable":       "no local route answers it",
	"viewer_events":     "answered by the viewer event log",
}

var refusedSpecial = regexp.MustCompile(`^refused:[a-z][a-z0-9_]*$`)

func validSpecial(tag string) bool {
	_, ok := Specials[tag]
	return ok || refusedSpecial.MatchString(tag)
}

func validateWrappers(t table) error {
	for _, w := range t.Wrappers {
		if w.Special == "" || w.Strip == "" || w.StripRefusal == "" {
			return fmt.Errorf("cloudvocab: a wrapper states no special, strip or refusal")
		}
		if w.StripCheck != "" {
			if _, ok := Checks[w.StripCheck]; !ok {
				return fmt.Errorf("cloudvocab: wrapper %s check %q is not one of cloudvocab.Checks", w.Special, w.StripCheck)
			}
		}
		if (w.Require == "") != (w.RequireRefusal == "") {
			return fmt.Errorf("cloudvocab: wrapper %s requires a field with no refusal, or the reverse", w.Special)
		}
	}
	if i := t.Idempotency; i == nil || i.Header == "" || i.From == "" || i.Fallback == "" || i.Except == "" {
		return fmt.Errorf("cloudvocab: the table states no idempotency rule")
	}
	return nil
}
