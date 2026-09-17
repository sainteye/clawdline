package cloud

import (
	"errors"
	"strings"
	"testing"
)

// TestPublishedBodiesAreCanonical reads the pairing bodies the vector file
// pins and requires each one to be its own canonical encoding. These are the
// bytes a signature and a fingerprint are taken over, so "parses" is not the
// claim being made here — "these exact bytes" is.
func TestPublishedBodiesAreCanonical(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	bodies := map[string]string{
		"pairing offer":              vectors.PairingHandover.Offer,
		"pairing handover":           vectors.PairingHandover.Handover,
		"grant AAD":                  vectors.PairingHandover.AAD,
		"execution response payload": vectors.ControlResponse.ResponsePayload.Body,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := ParseStrict([]byte(body))
			if err != nil {
				t.Fatalf("ParseStrict: %v", err)
			}
			if got := string(CanonicalBytes(value)); got != body {
				t.Errorf("re-serialized differently\n got %s\nwant %s", got, body)
			}
			if ChargedBytes(value) != len(body) {
				t.Errorf("charged bytes %d, want %d", ChargedBytes(value), len(body))
			}
		})
	}

	payload := vectors.ControlResponse.ResponsePayload
	value, err := ParseStrict([]byte(payload.Body))
	if err != nil {
		t.Fatalf("ParseStrict: %v", err)
	}
	keys, _ := value.Keys()
	if len(keys) != payload.FieldCount {
		t.Errorf("the response payload has %d members, the vector pins %d",
			len(keys), payload.FieldCount)
	}
	if sha256Hex([]byte(payload.Body)) != payload.SHA256 {
		t.Errorf("the vector file's own digest does not match its body")
	}
}

// TestCanonicalSerialization walks the RFC 8785 rules that actually bite.
func TestCanonicalSerialization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value Value
		want  string
	}{
		{"an empty object", Object(nil), `{}`},
		{"an empty array", Array(), `[]`},
		{"members sort, they do not keep insertion order",
			Object(map[string]Value{"b": Int(2), "a": Int(1), "C": Int(3)}),
			`{"C":3,"a":1,"b":2}`},
		{"array order is kept", Array(Int(3), Int(1), Int(2)), `[3,1,2]`},
		{"a slash stays bare", Str("a/b"), `"a/b"`},
		{"non-ASCII stays raw UTF-8", Str("會話"), `"會話"`},
		{"the five short escapes", Str("\b\t\n\f\r"), `"\b\t\n\f\r"`},
		{"quote and backslash", Str(`"\`), `"\"\\"`},
		{"every other C0 control is lowercase \\u00xx", Str("\x00\x1f"), `"\u0000\u001f"`},
		{"negative integers", Int(-1), `-1`},
		{"the safe-integer edges",
			Array(Int(MaxSafeInteger), Int(MinSafeInteger)),
			`[9007199254740991,-9007199254740991]`},
		{"null and the booleans", Array(Null(), Bool(true), Bool(false)), `[null,true,false]`},
		{"binary enters as padded base64", Base64([]byte{0, 255, 16}), `"AP8Q"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := string(CanonicalBytes(c.value)); got != c.want {
				t.Errorf("\n got %s\nwant %s", got, c.want)
			}
		})
	}
}

// TestMemberOrderIsUTF16NotBytes is the one ordering rule Go gets wrong by
// default. A code point above U+FFFF is a surrogate pair in UTF-16, so it sorts
// before U+E000..U+FFFF there and after them in Go's byte order.
func TestMemberOrderIsUTF16NotBytes(t *testing.T) {
	t.Parallel()
	astral := "\U0001F600" // U+1F600, a surrogate pair in UTF-16
	basic := "\uFF00"      // U+FF00, one UTF-16 unit above the surrogate range
	value := Object(map[string]Value{astral: Int(1), basic: Int(2)})
	want := `{"` + astral + `":1,"` + basic + `":2}`
	if got := string(CanonicalBytes(value)); got != want {
		t.Errorf("astral keys must sort first\n got %s\nwant %s", got, want)
	}
	if !(basic < astral) {
		t.Fatal("this test is pointless unless Go's byte order disagrees")
	}
}

// TestParseRefusals is the fail-closed list. Every one of these has a way of
// silently producing different bytes on the two ends of the protocol.
func TestParseRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  error
	}{
		{"a fraction", `{"a":1.0}`, ErrFloatingPoint},
		{"an exponent, even an integral one", `{"a":1e2}`, ErrFloatingPoint},
		{"past MAX_SAFE_INTEGER", `9007199254740992`, ErrIntegerRange},
		{"past MIN_SAFE_INTEGER", `-9007199254740992`, ErrIntegerRange},
		{"a duplicate member", `{"a":1,"a":2}`, ErrDuplicateKey},
		{"a duplicate member spelled twice over", `{"a":1,"\u0061":2}`, ErrDuplicateKey},
		{"a byte order mark", "\xef\xbb\xbf{}", ErrByteOrderMark},
		{"trailing bytes", `{} {}`, ErrTrailingBytes},
		{"a lone high surrogate", `"\ud800"`, ErrLoneSurrogate},
		{"a lone low surrogate", `"\udc00"`, ErrLoneSurrogate},
		{"invalid UTF-8", "\"\xff\"", ErrInvalidUTF8},
		{"an unterminated object", `{"a":1`, ErrMalformedJSON},
		{"a leading zero", `01`, ErrTrailingBytes},
		{"nothing at all", ``, ErrMalformedJSON},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(c.input)); !errors.Is(err, c.want) {
				t.Errorf("%q: want %v, got %v", c.input, c.want, err)
			}
		})
	}

	deep := strings.Repeat("[", MaxNestingDepth+1) + strings.Repeat("]", MaxNestingDepth+1)
	if _, err := Parse([]byte(deep)); !errors.Is(err, ErrNestingTooDeep) {
		t.Errorf("deep nesting: want ErrNestingTooDeep, got %v", err)
	}
}

// TestParseStrictRefusesANonCanonicalSpelling — Parse reads wire JSON, which
// need not be canonical; ParseStrict reads the bodies whose exact bytes are the
// protocol.
func TestParseStrictRefusesANonCanonicalSpelling(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"b":1,"a":2}`,  // out of order
		`{"a": 1}`,       // insignificant whitespace
		`{"a":"\u0041"}`, // an escape that need not be one
		`{"a":"\/"}`,     // an escaped slash
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(input)); err != nil {
				t.Fatalf("Parse must still accept it: %v", err)
			}
			if _, err := ParseStrict([]byte(input)); !errors.Is(err, ErrNotCanonical) {
				t.Errorf("ParseStrict: want ErrNotCanonical, got %v", err)
			}
		})
	}
}

// TestSigningInput pins the domain-separated record signing format: ASCII
// domain, one NUL, canonical body with only a TOP-LEVEL sig removed.
func TestSigningInput(t *testing.T) {
	t.Parallel()
	body := Object(map[string]Value{
		"a":   Int(1),
		"sig": Str("whatever"),
		"nested": Object(map[string]Value{
			"sig": Str("kept"),
		}),
	})
	got, err := SigningInput("clawdline-record-v1", body)
	if err != nil {
		t.Fatalf("SigningInput: %v", err)
	}
	want := "clawdline-record-v1\x00" + `{"a":1,"nested":{"sig":"kept"}}`
	if string(got) != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}

	if _, err := SigningInput("with a \x00 in it", body); !errors.Is(err, ErrInvalidDomain) {
		t.Errorf("a NUL in the domain could collide with the separator: %v", err)
	}
	if _, err := SigningInput("會話", body); !errors.Is(err, ErrInvalidDomain) {
		t.Errorf("a non-ASCII domain: want ErrInvalidDomain, got %v", err)
	}
	unsafe := Object(map[string]Value{"a": Int(MaxSafeInteger + 1)})
	if _, err := SigningInput("d", unsafe); !errors.Is(err, ErrIntegerRange) {
		t.Errorf("an unsafe integer must never reach a signature: %v", err)
	}
}
