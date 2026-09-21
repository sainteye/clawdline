package cloud

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func settingsFile(t *testing.T, body string) *nextconfig.File {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, nextconfig.FileName), []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return nextconfig.Open(dir)
}

// No file, no switch, no line. A first run must not reach anybody's account.
func TestTheLineIsOffUntilSomebodySaysOtherwise(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"no file":        "",
		"empty object":   `{}`,
		"other keys":     `{"hotkey":"cmd+shift+k"}`,
		"switch absent":  `{"cloud_relay_url":"wss://relay.clawdline.com/v1/connect"}`,
		"switch false":   `{"cloud_enabled":false}`,
		"switch not set": `{"cloud_enabled":null}`,
	} {
		settings, err := ReadSettings(settingsFile(t, body))
		if name == "switch not set" {
			// `null` is not `true`; it is also not a boolean, so it is a
			// refusal rather than a silent off. Either answer keeps the line
			// down, and the refusal says why.
			if err == nil && settings.Enabled {
				t.Fatalf("%s: the line came up", name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if settings.Enabled {
			t.Fatalf("%s: the line is on", name)
		}
	}
}

// The defaults point at production, so a machine that is switched on without
// any other configuration goes where it should.
func TestTheDefaultsArePublicClawdline(t *testing.T) {
	t.Parallel()
	settings, err := ReadSettings(settingsFile(t, `{"cloud_enabled":true}`))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !settings.Enabled {
		t.Fatal("the switch did not take")
	}
	if settings.RelayURL != DefaultRelayURL || settings.APIBase != DefaultAPIBase {
		t.Fatalf("endpoints: %s %s", settings.RelayURL, settings.APIBase)
	}
}

// A malformed endpoint stops the line and says so. The alternative is a
// machine that quietly falls back to production when somebody meant to point
// it at a relay of their own.
func TestABadEndpointIsARefusalNotADefault(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"switch is a string":   `{"cloud_enabled":"yes"}`,
		"relay is not a URL":   `{"cloud_relay_url":"::not a url::"}`,
		"relay is http":        `{"cloud_relay_url":"http://relay.clawdline.com/v1/connect"}`,
		"relay has a query":    `{"cloud_relay_url":"wss://relay.clawdline.com/v1/connect?token=abc"}`,
		"relay has a password": `{"cloud_relay_url":"wss://u:p@relay.clawdline.com/v1/connect"}`,
		"relay path is wrong":  `{"cloud_relay_url":"wss://relay.clawdline.com/v2/socket"}`,
		"api is not a URL":     `{"cloud_api_base":"::nope::"}`,
	} {
		if _, err := ReadSettings(settingsFile(t, body)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// Plain ws:// is for a relay on this machine and nowhere else. A device token
// on an unencrypted socket to another host is the credential in clear.
func TestPlainWebSocketsAreOnlyForThisMachine(t *testing.T) {
	t.Parallel()
	if err := ValidateRelayURL("ws://127.0.0.1:8787/v1/connect"); err != nil {
		t.Fatalf("a local relay: %v", err)
	}
	if err := ValidateRelayURL("ws://localhost:8787/v1/connect"); err != nil {
		t.Fatalf("localhost: %v", err)
	}
	if err := ValidateRelayURL("ws://relay.example.com/v1/connect"); err == nil {
		t.Fatal("an unencrypted socket to another host was accepted")
	}
	if err := ValidateAPIBase("http://example.com"); err == nil {
		t.Fatal("an unencrypted control plane on another host was accepted")
	}
	if err := ValidateAPIBase("http://127.0.0.1:8180"); err != nil {
		t.Fatalf("a local control plane: %v", err)
	}
}

// Turning the switch on keeps every other key, including one this daemon has
// never heard of.
func TestTheSwitchIsAMergeNotAnOverwrite(t *testing.T) {
	t.Parallel()
	file := settingsFile(t, `{"hotkey":"cmd+shift+k","something_else":{"kept":true}}`)
	if err := SetEnabled(file, true); err != nil {
		t.Fatalf("set: %v", err)
	}
	values, err := file.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if hotkey, ok := values.String("hotkey"); !ok || hotkey != "cmd+shift+k" {
		t.Fatalf("the hotkey did not survive: %q %v", hotkey, ok)
	}
	if _, ok := values.Raw["something_else"]; !ok {
		t.Fatal("an unknown key did not survive")
	}
	settings, err := ReadSettings(file)
	if err != nil || !settings.Enabled {
		t.Fatalf("after the write: %+v %v", settings, err)
	}
}
