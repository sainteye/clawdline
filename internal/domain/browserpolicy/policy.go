package browserpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	Native     = "native"
	Isolated   = "isolated"
	UserChrome = "user_chrome"
)

var ErrInvalid = errors.New("invalid browser priority or automatic selection")

func ValidMode(mode string) bool { return mode == Native || mode == Isolated || mode == UserChrome }

// Policy controls automatic selection. A person's explicit Session
// request bypasses Automatic, never profile setup or an emergency stop.
type Policy struct {
	Order     []string        `json:"order"`
	Automatic map[string]bool `json:"automatic"`
}

func Kinds() []string {
	return []string{Native, Isolated, UserChrome}
}

func (p Policy) Valid() bool {
	kinds := Kinds()
	if len(p.Order) != len(kinds) || len(p.Automatic) != len(kinds) {
		return false
	}
	seen := map[string]bool{}
	for _, mode := range p.Order {
		if !ValidMode(mode) || seen[mode] {
			return false
		}
		if _, ok := p.Automatic[mode]; !ok {
			return false
		}
		seen[mode] = true
	}
	return true
}

func DecodePolicy(raw []byte) (Policy, error) {
	var p Policy
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !p.Valid() {
		return Policy{}, ErrInvalid
	}
	// JSON null is not an explicit switch value. Decode to bool would otherwise
	// erase the distinction between an intentional false and missing consent.
	var shape struct {
		Automatic map[string]json.RawMessage `json:"automatic"`
	}
	if json.Unmarshal(raw, &shape) != nil {
		return Policy{}, ErrInvalid
	}
	for _, value := range shape.Automatic {
		if string(value) != "true" && string(value) != "false" {
			return Policy{}, ErrInvalid
		}
	}
	return p, nil
}
