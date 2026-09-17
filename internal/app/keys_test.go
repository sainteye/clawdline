package app

import (
	"bytes"
	"context"
	"testing"
)

// The route is a byte channel into a tty, so what it will send is a closed
// list: nine digits, Tab, back-tab and the word `submit`. Anything else —
// text, a control byte, a second escape sequence — is refused before a
// session is looked up.
func TestKeyNameIsAClosedList(t *testing.T) {
	admitted := map[string][]byte{
		"1": {'1'}, "9": {'9'}, "tab": {0x09}, "shift+tab": {0x1b, 0x5b, 0x5a}, "submit": nil,
	}
	for key, want := range admitted {
		got, _, ok := KeyName(key)
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("%q: %v %v", key, got, ok)
		}
	}
	for _, key := range []string{"", "0", "10", "a", "Tea", "\x03", "\r", "enter", "escape", "\x1b[A", " 1", "１"} {
		if _, _, ok := KeyName(key); ok {
			t.Fatalf("%q was admitted", key)
		}
	}
	// Refused without reading the machine: an Actions with nothing in it
	// would fail differently if the lookup came first.
	_, err := Actions{}.Key(context.Background(), "%1", "Tea")
	if ref, ok := err.(Refusal); !ok || ref.Code != "bad_request" {
		t.Fatalf("err %v", err)
	}
}
