package cloud

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// RFC 8785 (JSON Canonicalization Scheme), in the subset the cloud protocol
// actually carries. These bytes are three things at once: the input to a
// signature, the only legal spelling of a pairing body, and every
// charged_bytes figure. One wrong byte anywhere shows up on the far end as
// "signature verification failed", silently, forever.
//
// NUMBERS ARE SAFE INTEGERS, DELIBERATELY, AND FAIL CLOSED. RFC 8785 §3.2.2.3
// wants numbers printed by ECMAScript's Number::toString. Go's strconv does
// not agree with it — where the two disagree is exactly where a float lands —
// and an almost-right float serializer does not fail loudly: it produces bytes
// the browser end can never reproduce. Every number this protocol carries
// today is an integer, so this unit accepts integers only, and only inside
// [-(2^53-1), 2^53-1]. Past 2^53 an integer is no longer injective into an
// ECMAScript Number, so that end would rewrite it. If a wider domain is ever
// genuinely needed, agree shared number vectors first and widen against them,
// never one implementation alone.
//
// Ported from ~/code/clawdline/Sources/CloudCanonicalJSON.swift (544 lines);
// see docs/cloud-wire.md §3 for the line-by-line citation.

// The bounds this unit enforces.
const (
	// MaxNestingDepth is a fail-closed sanity bound (RFC 8785 §5). Protocol
	// bodies are a handful of levels deep; reaching this is an attack or a bug.
	MaxNestingDepth = 256
	// MaxSafeInteger is ECMAScript Number.MAX_SAFE_INTEGER, 2^53-1.
	MaxSafeInteger int64 = 9007199254740991
	// MinSafeInteger is ECMAScript Number.MIN_SAFE_INTEGER, -(2^53-1).
	MinSafeInteger int64 = -9007199254740991
)

// What this unit refuses. Each one is a sentinel a caller may branch on; the
// wrapped text names the offending field or literal.
var (
	ErrFloatingPoint  = errors.New("a JSON number with a fraction or an exponent")
	ErrIntegerRange   = errors.New("an integer outside the safe-integer domain")
	ErrDuplicateKey   = errors.New("the same member name twice")
	ErrByteOrderMark  = errors.New("a leading byte order mark")
	ErrTrailingBytes  = errors.New("bytes after the single top-level value")
	ErrNotCanonical   = errors.New("not the canonical encoding of its own value")
	ErrMalformedJSON  = errors.New("malformed JSON")
	ErrInvalidUTF8    = errors.New("not well-formed UTF-8")
	ErrLoneSurrogate  = errors.New("an unpaired UTF-16 surrogate")
	ErrNestingTooDeep = errors.New("nested past the depth bound")
	ErrInvalidDomain  = errors.New("a signing domain outside printable ASCII")
)

// Kind is which of the six JSON shapes a Value holds.
type Kind uint8

// The six shapes. There is deliberately no float: with one, "floats must be
// refused" would be a runtime rule instead of an unrepresentable state.
const (
	KindNull Kind = iota
	KindBool
	KindInt
	KindString
	KindArray
	KindObject
)

// Value is one JSON value the cloud protocol can canonicalize.
//
// Int carries an int64 for construction convenience, so the safe-integer bound
// cannot be enforced at the construction site; ParseStrict enforces it on every
// inbound byte and SigningInput enforces it before anything is signed.
type Value struct {
	kind Kind
	b    bool
	i    int64
	s    string
	arr  []Value
	obj  map[string]Value
}

// Null is the JSON null.
func Null() Value { return Value{kind: KindNull} }

// Bool is a JSON true or false.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Int is a JSON number. The safe-integer bound is checked when it matters.
func Int(i int64) Value { return Value{kind: KindInt, i: i} }

// Str is a JSON string.
func Str(s string) Value { return Value{kind: KindString, s: s} }

// Base64 is the one intended way to put binary into a record: a binary field
// must already be canonical standard *padded* base64 by the time it enters a
// Value, and this is what produces that form.
func Base64(data []byte) Value {
	return Value{kind: KindString, s: base64.StdEncoding.EncodeToString(data)}
}

// Array is a JSON array. Order is kept.
func Array(items ...Value) Value {
	return Value{kind: KindArray, arr: append([]Value(nil), items...)}
}

// Object is a JSON object. The map is copied, so a later write to the caller's
// map cannot change what was canonicalized.
func Object(members map[string]Value) Value {
	copied := make(map[string]Value, len(members))
	for key, value := range members {
		copied[key] = value
	}
	return Value{kind: KindObject, obj: copied}
}

// Kind is which shape this value holds.
func (v Value) Kind() Kind { return v.kind }

// Bool is the boolean, and whether this value was one.
func (v Value) Bool() (bool, bool) { return v.b, v.kind == KindBool }

// Int is the integer, and whether this value was one.
func (v Value) Int() (int64, bool) { return v.i, v.kind == KindInt }

// Str is the string, and whether this value was one.
func (v Value) Str() (string, bool) { return v.s, v.kind == KindString }

// Items is an array's elements, and whether this value was one.
func (v Value) Items() ([]Value, bool) {
	if v.kind != KindArray {
		return nil, false
	}
	return append([]Value(nil), v.arr...), true
}

// Member is one member of an object, and whether it is there.
func (v Value) Member(key string) (Value, bool) {
	if v.kind != KindObject {
		return Value{}, false
	}
	value, ok := v.obj[key]
	return value, ok
}

// Keys is an object's member names in canonical (UTF-16 code-unit) order, and
// whether this value was an object.
func (v Value) Keys() ([]string, bool) {
	if v.kind != KindObject {
		return nil, false
	}
	return sortedKeys(v.obj), true
}

// Equal is value equality, ignoring the order a map was built in.
func (v Value) Equal(other Value) bool {
	if v.kind != other.kind {
		return false
	}
	switch v.kind {
	case KindNull:
		return true
	case KindBool:
		return v.b == other.b
	case KindInt:
		return v.i == other.i
	case KindString:
		return v.s == other.s
	case KindArray:
		if len(v.arr) != len(other.arr) {
			return false
		}
		for index := range v.arr {
			if !v.arr[index].Equal(other.arr[index]) {
				return false
			}
		}
		return true
	default:
		if len(v.obj) != len(other.obj) {
			return false
		}
		for key, value := range v.obj {
			otherValue, ok := other.obj[key]
			if !ok || !value.Equal(otherValue) {
				return false
			}
		}
		return true
	}
}

// ValidateNumberDomain walks a value and refuses any integer outside the
// safe-integer domain. ParseStrict enforces the domain on every inbound byte
// and SigningInput calls this before serializing; a caller producing canonical
// bytes through CanonicalBytes or ChargedBytes directly must call it first.
func ValidateNumberDomain(v Value) error {
	switch v.kind {
	case KindInt:
		if v.i < MinSafeInteger || v.i > MaxSafeInteger {
			return fmt.Errorf("%w: %d", ErrIntegerRange, v.i)
		}
	case KindArray:
		for _, item := range v.arr {
			if err := ValidateNumberDomain(item); err != nil {
				return err
			}
		}
	case KindObject:
		for _, member := range v.obj {
			if err := ValidateNumberDomain(member); err != nil {
				return err
			}
		}
	}
	return nil
}

// CanonicalBytes is the RFC 8785 serialization of a value.
func CanonicalBytes(v Value) []byte {
	var out bytes.Buffer
	appendCanonical(v, &out)
	return out.Bytes()
}

// ChargedBytes is the length of the canonical UTF-8 encoding of a record.
// Binary fields must already have become padded base64 strings — see Base64 —
// before entering the record; this only measures.
func ChargedBytes(record Value) int { return len(CanonicalBytes(record)) }

// SigningInput is the domain's ASCII bytes, a single 0x00 separator, then the
// canonical JSON of the body with its top-level "sig" member removed.
//
// Removal is top-level ONLY: a nested "sig" is somebody's payload data and is
// covered by the signature like any other byte. A non-object value has no
// top-level "sig" and is canonicalized as it is. The domain must be printable
// ASCII — a control byte inside it could collide with the NUL separator and let
// two different (domain, body) pairs share one signing input.
//
// This is the *pairing and record* signing format. The relay envelope uses a
// different one; see Envelope.SigningBytes.
func SigningInput(domain string, body Value) ([]byte, error) {
	if err := ValidateNumberDomain(body); err != nil {
		return nil, err
	}
	for i := 0; i < len(domain); i++ {
		if domain[i] < 0x20 || domain[i] > 0x7e {
			return nil, fmt.Errorf("%w: %q", ErrInvalidDomain, domain)
		}
	}
	stripped := body
	if body.kind == KindObject {
		members := make(map[string]Value, len(body.obj))
		for key, value := range body.obj {
			if key == "sig" {
				continue
			}
			members[key] = value
		}
		stripped = Value{kind: KindObject, obj: members}
	}
	out := make([]byte, 0, len(domain)+1+64)
	out = append(out, domain...)
	out = append(out, 0x00)
	return append(out, CanonicalBytes(stripped)...), nil
}

// Parse reads one JSON value under the protocol's rules: no duplicate member
// names, no byte order mark, no trailing bytes, strict UTF-8, no lone
// surrogate, no fraction or exponent, and no integer outside the safe domain.
//
// It does NOT require the input to be canonical. Relay frames are produced by
// JSON.stringify in member-insertion order, so an envelope arriving from the
// wire is ordinary JSON; ParseStrict is for the bodies whose exact bytes are
// part of the protocol.
func Parse(data []byte) (Value, error) { return parse(data, false) }

// ParseStrict is Parse, and then the input must be the canonical encoding of
// what it parsed to. Pairing bodies, receipts and every deterministic fixture
// are read this way.
func ParseStrict(data []byte) (Value, error) { return parse(data, true) }

func parse(data []byte, requireCanonical bool) (Value, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return Value{}, ErrByteOrderMark
	}
	p := &parser{input: data}
	value, err := p.parseValue()
	if err != nil {
		return Value{}, err
	}
	p.skipWhitespace()
	if p.pos != len(p.input) {
		return Value{}, ErrTrailingBytes
	}
	if requireCanonical && !bytes.Equal(CanonicalBytes(value), data) {
		return Value{}, ErrNotCanonical
	}
	return value, nil
}

// MARK: canonical serialization

func appendCanonical(v Value, out *bytes.Buffer) {
	switch v.kind {
	case KindNull:
		out.WriteString("null")
	case KindBool:
		if v.b {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case KindInt:
		out.WriteString(strconv.FormatInt(v.i, 10))
	case KindString:
		appendEscaped(v.s, out)
	case KindArray:
		out.WriteByte('[')
		for index, item := range v.arr {
			if index > 0 {
				out.WriteByte(',')
			}
			appendCanonical(item, out)
		}
		out.WriteByte(']')
	case KindObject:
		out.WriteByte('{')
		for index, key := range sortedKeys(v.obj) {
			if index > 0 {
				out.WriteByte(',')
			}
			appendEscaped(key, out)
			out.WriteByte(':')
			appendCanonical(v.obj[key], out)
		}
		out.WriteByte('}')
	}
}

// sortedKeys orders member names by UTF-16 code unit, which is what RFC 8785
// asks for and is NOT the same as Go's byte order: a code point above U+FFFF
// is a surrogate pair in UTF-16, so it sorts before U+E000..U+FFFF there and
// after them here.
func sortedKeys(members map[string]Value) []string {
	keys := make([]string, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool { return lessUTF16(keys[a], keys[b]) })
	return keys
}

func lessUTF16(a, b string) bool {
	if !hasAstral(a) && !hasAstral(b) {
		// Below U+10000 the UTF-8 byte order and the UTF-16 code-unit order
		// agree, and almost every protocol key is ASCII.
		return a < b
	}
	left := utf16.Encode([]rune(a))
	right := utf16.Encode([]rune(b))
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) < len(right)
}

func hasAstral(s string) bool {
	for _, r := range s {
		if r > 0xFFFF {
			return true
		}
	}
	return false
}

// appendEscaped writes RFC 8785 §3.2.2.2: escape only what must be escaped.
// Quote and backslash get their short forms, U+0008/09/0A/0C/0D get \b \t \n \f
// \r, every other C0 control gets a lowercase \u00xx. Nothing else is escaped —
// "/" stays bare and non-ASCII is emitted as raw UTF-8, which here means the
// string's own bytes, unexamined.
func appendEscaped(s string, out *bytes.Buffer) {
	const hexDigits = "0123456789abcdef"
	out.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case 0x08:
			out.WriteString(`\b`)
		case 0x09:
			out.WriteString(`\t`)
		case 0x0A:
			out.WriteString(`\n`)
		case 0x0C:
			out.WriteString(`\f`)
		case 0x0D:
			out.WriteString(`\r`)
		default:
			if c < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hexDigits[c>>4])
				out.WriteByte(hexDigits[c&0xF])
			} else {
				out.WriteByte(c)
			}
		}
	}
	out.WriteByte('"')
}

// MARK: recursive-descent parser (RFC 8259 grammar, safe integers only)

type parser struct {
	input []byte
	pos   int
	depth int
}

func (p *parser) parseValue() (Value, error) {
	p.skipWhitespace()
	if p.pos >= len(p.input) {
		return Value{}, fmt.Errorf("%w: unexpected end of input", ErrMalformedJSON)
	}
	switch c := p.input[p.pos]; {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return Value{}, err
		}
		return Str(s), nil
	case c == 't':
		return Bool(true), p.expectLiteral("true")
	case c == 'f':
		return Bool(false), p.expectLiteral("false")
	case c == 'n':
		return Null(), p.expectLiteral("null")
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return Value{}, fmt.Errorf("%w: unexpected byte 0x%02x at offset %d", ErrMalformedJSON, c, p.pos)
	}
}

func (p *parser) parseObject() (Value, error) {
	if err := p.enterContainer(); err != nil {
		return Value{}, err
	}
	defer func() { p.depth-- }()
	p.pos++ // consume "{"
	members := map[string]Value{}
	p.skipWhitespace()
	if p.pos < len(p.input) && p.input[p.pos] == '}' {
		p.pos++
		return Value{kind: KindObject, obj: members}, nil
	}
	for {
		p.skipWhitespace()
		if p.pos >= len(p.input) || p.input[p.pos] != '"' {
			return Value{}, fmt.Errorf("%w: expected an object key at offset %d", ErrMalformedJSON, p.pos)
		}
		// Duplicate detection compares raw, unescaped keys, so "a" and "a"
		// collide as I-JSON requires.
		key, err := p.parseString()
		if err != nil {
			return Value{}, err
		}
		if _, seen := members[key]; seen {
			return Value{}, fmt.Errorf("%w: %q", ErrDuplicateKey, key)
		}
		p.skipWhitespace()
		if p.pos >= len(p.input) || p.input[p.pos] != ':' {
			return Value{}, fmt.Errorf("%w: expected ':' at offset %d", ErrMalformedJSON, p.pos)
		}
		p.pos++
		value, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		members[key] = value
		p.skipWhitespace()
		if p.pos >= len(p.input) {
			return Value{}, fmt.Errorf("%w: unterminated object", ErrMalformedJSON)
		}
		switch p.input[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return Value{kind: KindObject, obj: members}, nil
		default:
			return Value{}, fmt.Errorf("%w: expected ',' or '}' at offset %d", ErrMalformedJSON, p.pos)
		}
	}
}

func (p *parser) parseArray() (Value, error) {
	if err := p.enterContainer(); err != nil {
		return Value{}, err
	}
	defer func() { p.depth-- }()
	p.pos++ // consume "["
	items := []Value{}
	p.skipWhitespace()
	if p.pos < len(p.input) && p.input[p.pos] == ']' {
		p.pos++
		return Value{kind: KindArray, arr: items}, nil
	}
	for {
		item, err := p.parseValue()
		if err != nil {
			return Value{}, err
		}
		items = append(items, item)
		p.skipWhitespace()
		if p.pos >= len(p.input) {
			return Value{}, fmt.Errorf("%w: unterminated array", ErrMalformedJSON)
		}
		switch p.input[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Value{kind: KindArray, arr: items}, nil
		default:
			return Value{}, fmt.Errorf("%w: expected ',' or ']' at offset %d", ErrMalformedJSON, p.pos)
		}
	}
}

func (p *parser) parseString() (string, error) {
	p.pos++ // consume the opening quote
	var out bytes.Buffer
	for {
		if p.pos >= len(p.input) {
			return "", fmt.Errorf("%w: unterminated string", ErrMalformedJSON)
		}
		c := p.input[p.pos]
		switch {
		case c == '"':
			p.pos++
			return out.String(), nil
		case c == '\\':
			p.pos++
			r, err := p.parseEscape()
			if err != nil {
				return "", err
			}
			out.WriteRune(r)
		case c < 0x20:
			return "", fmt.Errorf("%w: unescaped control character at offset %d", ErrMalformedJSON, p.pos)
		case c < 0x80:
			out.WriteByte(c)
			p.pos++
		default:
			r, size, err := p.decodeUTF8()
			if err != nil {
				return "", err
			}
			out.WriteRune(r)
			p.pos += size
		}
	}
}

func (p *parser) parseEscape() (rune, error) {
	if p.pos >= len(p.input) {
		return 0, fmt.Errorf("%w: unterminated escape", ErrMalformedJSON)
	}
	c := p.input[p.pos]
	p.pos++
	switch c {
	case '"':
		return '"', nil
	case '\\':
		return '\\', nil
	case '/':
		return '/', nil
	case 'b':
		return 0x08, nil
	case 'f':
		return 0x0C, nil
	case 'n':
		return 0x0A, nil
	case 'r':
		return 0x0D, nil
	case 't':
		return 0x09, nil
	case 'u':
		unit, err := p.parseHex4()
		if err != nil {
			return 0, err
		}
		if unit >= 0xDC00 && unit <= 0xDFFF {
			return 0, ErrLoneSurrogate
		}
		if unit >= 0xD800 && unit <= 0xDBFF {
			if p.pos+1 >= len(p.input) || p.input[p.pos] != '\\' || p.input[p.pos+1] != 'u' {
				return 0, ErrLoneSurrogate
			}
			p.pos += 2
			low, err := p.parseHex4()
			if err != nil {
				return 0, err
			}
			if low < 0xDC00 || low > 0xDFFF {
				return 0, ErrLoneSurrogate
			}
			return utf16.DecodeRune(rune(unit), rune(low)), nil
		}
		return rune(unit), nil
	default:
		return 0, fmt.Errorf("%w: invalid escape at offset %d", ErrMalformedJSON, p.pos-1)
	}
}

func (p *parser) parseHex4() (uint16, error) {
	if p.pos+4 > len(p.input) {
		return 0, fmt.Errorf(`%w: truncated \u escape`, ErrMalformedJSON)
	}
	var value uint16
	for range 4 {
		c := p.input[p.pos]
		var digit uint16
		switch {
		case c >= '0' && c <= '9':
			digit = uint16(c - '0')
		case c >= 'a' && c <= 'f':
			digit = uint16(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = uint16(c-'A') + 10
		default:
			return 0, fmt.Errorf("%w: invalid hex digit at offset %d", ErrMalformedJSON, p.pos)
		}
		value = value<<4 | digit
		p.pos++
	}
	return value, nil
}

// decodeUTF8 is strict: bad continuation bytes, truncation, overlong forms,
// encoded surrogates and anything above U+10FFFF are refused rather than
// replaced with U+FFFD. Go's utf8.DecodeRune already refuses all of those by
// answering RuneError with size 1, which the caller must not accept as a
// character — a lone 0xEF byte would otherwise become a valid string.
func (p *parser) decodeUTF8() (rune, int, error) {
	r, size := utf8.DecodeRune(p.input[p.pos:])
	if r == utf8.RuneError && size <= 1 {
		return 0, 0, ErrInvalidUTF8
	}
	return r, size, nil
}

func (p *parser) parseNumber() (Value, error) {
	start := p.pos
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.input) || !isDigit(p.input[p.pos]) {
		return Value{}, fmt.Errorf("%w: expected a digit at offset %d", ErrMalformedJSON, p.pos)
	}
	if p.input[p.pos] == '0' {
		p.pos++
	} else {
		for p.pos < len(p.input) && isDigit(p.input[p.pos]) {
			p.pos++
		}
	}
	// A fraction or an exponent: consume the whole JSON number so the error can
	// name the entire literal, then refuse it. See the number note at the top.
	if p.pos < len(p.input) && (p.input[p.pos] == '.' || p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		if p.input[p.pos] == '.' {
			p.pos++
			if p.pos >= len(p.input) || !isDigit(p.input[p.pos]) {
				return Value{}, fmt.Errorf("%w: expected a digit after '.' at offset %d", ErrMalformedJSON, p.pos)
			}
			for p.pos < len(p.input) && isDigit(p.input[p.pos]) {
				p.pos++
			}
		}
		if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
			p.pos++
			if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
				p.pos++
			}
			if p.pos >= len(p.input) || !isDigit(p.input[p.pos]) {
				return Value{}, fmt.Errorf("%w: expected an exponent digit at offset %d", ErrMalformedJSON, p.pos)
			}
			for p.pos < len(p.input) && isDigit(p.input[p.pos]) {
				p.pos++
			}
		}
		return Value{}, fmt.Errorf("%w: %s", ErrFloatingPoint, p.input[start:p.pos])
	}
	literal := string(p.input[start:p.pos])
	integer, err := strconv.ParseInt(literal, 10, 64)
	if err != nil || integer < MinSafeInteger || integer > MaxSafeInteger {
		return Value{}, fmt.Errorf("%w: %s", ErrIntegerRange, literal)
	}
	return Int(integer), nil
}

func (p *parser) expectLiteral(literal string) error {
	if p.pos+len(literal) > len(p.input) || string(p.input[p.pos:p.pos+len(literal)]) != literal {
		return fmt.Errorf("%w: invalid literal at offset %d", ErrMalformedJSON, p.pos)
	}
	p.pos += len(literal)
	return nil
}

func (p *parser) enterContainer() error {
	p.depth++
	if p.depth > MaxNestingDepth {
		return ErrNestingTooDeep
	}
	return nil
}

func (p *parser) skipWhitespace() {
	for p.pos < len(p.input) {
		switch p.input[p.pos] {
		case 0x20, 0x09, 0x0A, 0x0D:
			p.pos++
		default:
			return
		}
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
