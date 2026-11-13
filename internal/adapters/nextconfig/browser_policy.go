package nextconfig

import (
	"encoding/json"

	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
)

var ErrBrowserPolicy = browserpolicy.ErrInvalid

type BrowserPolicy = browserpolicy.Policy

func BrowserKinds() []string                                { return browserpolicy.Kinds() }
func DecodeBrowserPolicy(raw []byte) (BrowserPolicy, error) { return browserpolicy.DecodePolicy(raw) }

// BrowserPolicy preserves the prior selected browser as the first automatic
// choice. Native tools remain available; no migration grants usual Chrome.
func (v Values) BrowserPolicy() (BrowserPolicy, error) {
	if raw, ok := v.Raw["browser_policy"]; ok {
		return DecodeBrowserPolicy(raw)
	}
	mode := v.BrowserMode()
	order := BrowserKinds()
	if mode != BrowserModeNative {
		order = []string{mode, BrowserModeNative}
		for _, kind := range BrowserKinds() {
			if kind != mode && kind != BrowserModeNative {
				order = append(order, kind)
			}
		}
	}
	return BrowserPolicy{Order: order, Automatic: map[string]bool{BrowserModeNative: true, BrowserModeIsolated: mode == BrowserModeIsolated, BrowserModeUserChrome: mode == BrowserModeUserChrome}}, nil
}

func browserPolicyChanges(changes map[string]any) (map[string]any, error) {
	value, ok := changes["browser_policy"]
	if !ok {
		return nil, nil
	}
	if _, old := changes["browser_mode"]; old {
		return nil, ErrBrowserPolicy
	}
	if _, old := changes["browser_enabled"]; old {
		return nil, ErrBrowserPolicy
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrBrowserPolicy
	}
	policy, err := DecodeBrowserPolicy(raw)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(changes)+2)
	for key, value := range changes {
		out[key] = value
	}
	out["browser_policy"] = policy
	// Older daemons do not understand per-profile automatic choices. A return
	// to an older build must fail closed rather than grant the wrong profile.
	out["browser_mode"], out["browser_enabled"] = BrowserModeNative, false
	return out, nil
}
