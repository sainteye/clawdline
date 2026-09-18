package cloud

// The switch, and where the line is pointed.
//
// **Off by default, and off means off.** `docs/remote.md` design principle 3:
// this machine does not reach the user's account until the user says so. So
// every one of these has a default that connects to nothing, and Settings
// answers `Enabled: false` for a file that does not exist, for a file with no
// cloud keys in it, and for a file whose switch is anything other than `true`.
//
// The keys live in this app's own settings file,
// `~/.config/clawdline-next/config.json`, through
// `internal/adapters/nextconfig`, which merges rather than overwrites — a key
// this daemon has never heard of survives a write, including the hotkey the
// macOS shell reads from the same file.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
)

// Settings keys. They are prefixed `cloud_` so that one glance at the file
// says which half of the app a line belongs to.
const (
	// KeyEnabled is the switch. Anything but a JSON `true` is off.
	KeyEnabled = "cloud_enabled"
	// KeyRelayURL is the relay's WebSocket URL. Empty means the default.
	KeyRelayURL = "cloud_relay_url"
	// KeyAPIBase is the control plane's HTTPS base. Empty means the default.
	KeyAPIBase = "cloud_api_base"
	// KeyMachineName is what the person will see in their machine list.
	KeyMachineName = "cloud_machine_name"
	// KeyAppOrigin is where the hosted console lives. It is a separate key
	// from the api and the relay because a pairing link is the one thing this
	// machine hands to a **person**: they open it in a browser, so it has to
	// name the site they will be looking at, not the control plane behind it.
	// Empty means the default.
	KeyAppOrigin = "cloud_app_origin"
	// KeyCommands is whether a Cloud viewer may cause an effect here at all —
	// type into a session, close one, start one. It is a **second** switch on
	// purpose, the same shape as `remote` and `remote_write` on the free path:
	// letting somebody read a session list and letting them run code on this
	// Mac are two different decisions, and one switch cannot carry both.
	KeyCommands = "cloud_commands"
)

// The production endpoints, `CloudBridgeLifecycle.swift:397`.
const (
	DefaultRelayURL = "wss://relay.clawdline.com/v1/connect"
	DefaultAPIBase  = "https://api.clawdline.com"
	// DefaultAppOrigin is the hosted console, `cloud-onboarding.js`'s
	// CLOUD_APP_ORIGIN. A pairing QR carries this origin and the browser
	// reading it checks the origin before it will treat the fragment as a
	// pairing invitation at all.
	DefaultAppOrigin = "https://app.clawdline.com"
)

// MasterKeyID is the key id this machine seals with, `CloudBridgeLifecycle.swift:396`.
const MasterKeyID = "ms-1"

// Settings is one reading of the switch.
type Settings struct {
	Enabled bool
	// Commands is the remote-write switch. Off is off: a viewer may read and
	// may not act.
	Commands    bool
	RelayURL    string
	APIBase     string
	AppOrigin   string
	MachineName string
}

// ReadSettings answers what the settings file says now.
//
// A malformed value is a *refusal*, not a default: a relay URL with a typo in
// its scheme should stop the line and say so, because the alternative is a
// machine that silently connects to the production relay when somebody meant
// to point it at a local one.
func ReadSettings(file *nextconfig.File) (Settings, error) {
	values, err := file.Read()
	if err != nil {
		return Settings{}, err
	}
	settings := Settings{RelayURL: DefaultRelayURL, APIBase: DefaultAPIBase, AppOrigin: DefaultAppOrigin}

	if raw, ok := values.Raw[KeyEnabled]; ok {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return Settings{}, fmt.Errorf("%s must be true or false", KeyEnabled)
		}
		settings.Enabled = enabled
	}
	if raw, ok := values.Raw[KeyCommands]; ok {
		var allowed bool
		if err := json.Unmarshal(raw, &allowed); err != nil {
			return Settings{}, fmt.Errorf("%s must be true or false", KeyCommands)
		}
		settings.Commands = allowed
	}
	if relay, ok := values.String(KeyRelayURL); ok && relay != "" {
		if err := ValidateRelayURL(relay); err != nil {
			return Settings{}, err
		}
		settings.RelayURL = relay
	}
	if base, ok := values.String(KeyAPIBase); ok && base != "" {
		if err := ValidateAPIBase(base); err != nil {
			return Settings{}, err
		}
		settings.APIBase = base
	}
	if origin, ok := values.String(KeyAppOrigin); ok && origin != "" {
		if err := ValidateAppOrigin(origin); err != nil {
			return Settings{}, err
		}
		settings.AppOrigin = origin
	}
	if name, ok := values.String(KeyMachineName); ok {
		settings.MachineName = name
	}
	return settings, nil
}

// SetEnabled writes the switch and leaves every other key as it was.
func SetEnabled(file *nextconfig.File, enabled bool) error {
	_, err := file.Set(map[string]any{KeyEnabled: enabled})
	return err
}

// SetCommands writes the remote-write switch and leaves every other key as it
// was. Turning the line off does not turn this one off, and that is deliberate:
// the person who said "no writes" said it about every time the line comes back
// up, not about this one session.
func SetCommands(file *nextconfig.File, allowed bool) error {
	_, err := file.Set(map[string]any{KeyCommands: allowed})
	return err
}

// maxRelayURLBytes is the Swift app's bound (`CloudTransport.swift:162`). A URL
// longer than this is not a hostname, it is a payload.
const maxRelayURLBytes = 2048

// ValidateRelayURL refuses a relay URL this build will not dial.
//
// The path rule is the one worth reading: an empty path, `/`, or exactly
// `/v1/connect` are accepted, and **anything else is refused rather than
// rewritten** (`CloudTransport.swift:2022-2035`). A device token goes out in
// the Authorization header of this request, and quietly redirecting it to a
// path somebody mistyped is how a bearer credential ends up somewhere nobody
// meant it to be.
func ValidateRelayURL(raw string) error {
	if len(raw) > maxRelayURLBytes {
		return fmt.Errorf("%s is longer than %d bytes", KeyRelayURL, maxRelayURLBytes)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a URL: %w", KeyRelayURL, err)
	}
	switch parsed.Scheme {
	case "wss":
	case "ws":
		// Plain ws is for a relay on this machine and nowhere else. A device
		// token on an unencrypted socket to another host is the credential
		// travelling in clear.
		if !isLoopback(parsed.Hostname()) {
			return fmt.Errorf("%s may only be ws:// for a relay on this machine; %s needs wss://", KeyRelayURL, parsed.Hostname())
		}
	default:
		return fmt.Errorf("%s must be ws:// or wss://, not %q", KeyRelayURL, parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("%s names no host", KeyRelayURL)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not carry a user or password", KeyRelayURL)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("%s must not carry a fragment", KeyRelayURL)
	}
	if parsed.RawQuery != "" {
		// The role is added by the transport. A query here would either be
		// overwritten or would carry a token, and the relay refuses a token in
		// a query string precisely because it lands in every proxy log.
		return fmt.Errorf("%s must not carry a query string", KeyRelayURL)
	}
	switch parsed.Path {
	case "", "/", "/v1/connect":
		return nil
	}
	return fmt.Errorf("%s must end in /v1/connect, not %q", KeyRelayURL, parsed.Path)
}

// ValidateAppOrigin refuses a console origin this build will not put in front
// of a person.
//
// It is the same shape as ValidateAPIBase and it is a separate function because
// what it protects is different: this string ends up in a link somebody clicks
// with a one-time pairing secret in its fragment, so a path, a query or a
// fragment already on it would either be dropped or would carry that secret
// somewhere it was not meant to go.
func ValidateAppOrigin(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a URL: %w", KeyAppOrigin, err)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopback(parsed.Hostname()) {
			return fmt.Errorf("%s may only be http:// for a console on this machine; %s needs https://", KeyAppOrigin, parsed.Hostname())
		}
	default:
		return fmt.Errorf("%s must be http:// or https://, not %q", KeyAppOrigin, parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("%s names no host", KeyAppOrigin)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not carry a user or password", KeyAppOrigin)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an origin, with no query or fragment", KeyAppOrigin)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("%s must be an origin, not %q", KeyAppOrigin, parsed.Path)
	}
	return nil
}

// ValidateAPIBase refuses a control-plane base this build will not post to.
func ValidateAPIBase(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a URL: %w", KeyAPIBase, err)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopback(parsed.Hostname()) {
			return fmt.Errorf("%s may only be http:// for an api on this machine; %s needs https://", KeyAPIBase, parsed.Hostname())
		}
	default:
		return fmt.Errorf("%s must be http:// or https://, not %q", KeyAPIBase, parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("%s names no host", KeyAPIBase)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not carry a user or password", KeyAPIBase)
	}
	return nil
}

func isLoopback(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}
