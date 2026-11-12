package nextconfig

import "errors"

var ErrBrowserMode = errors.New("invalid browser mode")

const (
	BrowserModeNative     = "native"
	BrowserModeUserChrome = "user_chrome"
	BrowserModeIsolated   = "isolated"
)

// BrowserMode treats an absent setting as the old consent: enabled meant
// Clawdline's isolated profile. An invalid explicit mode fails closed.
func (v Values) BrowserMode() string {
	if _, exists := v.Raw["browser_mode"]; exists {
		mode, _ := v.String("browser_mode")
		if validBrowserMode(mode) {
			return mode
		}
		return BrowserModeNative
	}
	on, _ := v.Bool("browser_enabled")
	if on {
		return BrowserModeIsolated
	}
	return BrowserModeNative
}

func validBrowserMode(mode string) bool {
	return mode == BrowserModeNative || mode == BrowserModeUserChrome || mode == BrowserModeIsolated
}

// browserChanges writes one canonical choice. The raw legacy boolean is
// enabled only for isolated mode, so a downgrade cannot reinterpret a user-
// Chrome choice as permission for an older daemon's isolated channel. Values.Bool
// projects the canonical runtime state for current callers.
// Legacy writes cannot silently turn an existing user-Chrome choice into an
// isolated one, or leave a revoked choice enabled through the other key.
func browserChanges(current Values, changes map[string]any) (map[string]any, error) {
	modeValue, modeChanged := changes["browser_mode"]
	enabledValue, enabledChanged := changes["browser_enabled"]
	if !modeChanged && !enabledChanged {
		return changes, nil
	}
	mode := current.BrowserMode()
	if modeChanged {
		var ok bool
		mode, ok = modeValue.(string)
		if !ok || !validBrowserMode(mode) {
			return nil, ErrBrowserMode
		}
	}
	if enabledChanged {
		enabled, ok := enabledValue.(bool)
		if !ok {
			return nil, ErrBrowserMode
		}
		if modeChanged && enabled != (mode != BrowserModeNative) {
			return nil, ErrBrowserMode
		}
		if !modeChanged {
			if !enabled {
				mode = BrowserModeNative
			} else if mode == BrowserModeNative {
				mode = BrowserModeIsolated
			}
		}
	}
	out := make(map[string]any, len(changes)+1)
	for key, value := range changes {
		out[key] = value
	}
	out["browser_mode"], out["browser_enabled"] = mode, mode == BrowserModeIsolated
	return out, nil
}
