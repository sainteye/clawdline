package cloud

// The production cutover, from the daemon's side: the status route names why
// the line is not up, and the preflight refuses the settings that would send
// the person's approval to the wrong place. None of this dials anything — a
// link whose identity is missing or belongs elsewhere stops before the
// network, which is itself one of the things asserted.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
	"github.com/sainteye/clawdline-go/internal/adapters/cloudkeys"
)

func writeSettings(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeIdentity(t *testing.T, dir, apiBase string) {
	t.Helper()
	store := adaptercloud.NewIdentityStore(filepath.Join(dir, cloudkeys.DirName))
	if err := store.Save(adaptercloud.Identity{AccountID: "usr_local", MachineID: "mac_local", MachineCredential: "local-credential", APIBase: apiBase}); err != nil {
		t.Fatalf("identity: %v", err)
	}
}

func TestTheStatusRouteNamesWhyTheLineIsNotUp(t *testing.T) {
	for _, c := range []struct {
		name     string
		identity string
		code     string
	}{
		{name: "never signed in", code: "no_identity"},
		// The cutover's own trap: an identity from a local test control plane,
		// and settings that now say production.
		{name: "signed in to a local test control plane", identity: "http://127.0.0.1:8180", code: "identity_other_environment"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			// Production endpoints: nothing but the switch in the file.
			writeSettings(t, dir, `{"cloud_enabled":true}`)
			if c.identity != "" {
				writeIdentity(t, dir, c.identity)
			}
			link, err := Open(LinkOptions{Dir: dir})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			status := link.Status()
			if status.LastErrorKind != string(adaptercloud.KindNotSignedIn) {
				t.Errorf("last_error_kind %q, want %s", status.LastErrorKind, adaptercloud.KindNotSignedIn)
			}
			// The settings page shows last_error as it is, so the name leads.
			if want := string(adaptercloud.KindNotSignedIn) + " (" + c.code + ")"; !strings.HasPrefix(status.LastError, want) {
				t.Errorf("last_error %q does not lead with %q", status.LastError, want)
			}
			if link.transport != nil {
				t.Error("a line with no usable identity built a transport, which is the part that dials")
			}
		})
	}
}

func TestThePreflightStopsBeforeThePersonsStepOnlyWhenItShould(t *testing.T) {
	for _, c := range []struct {
		name     string
		settings string
		identity string
		ready    bool
		row      string
		result   string
		next     string
	}{
		{name: "a fresh machine with production defaults", ready: true, row: "endpoints", result: PreflightOK, next: "the person's step"},
		{name: "a local test relay left in the file",
			settings: `{"cloud_api_base":"http://127.0.0.1:8180","cloud_relay_url":"ws://127.0.0.1:8787/v1/connect","cloud_app_origin":"https://127.0.0.1:8443"}`,
			row:      "endpoints", result: PreflightBlock},
		{name: "production api, local relay",
			settings: `{"cloud_relay_url":"ws://127.0.0.1:8787/v1/connect"}`,
			row:      "endpoints", result: PreflightBlock},
		{name: "an identity from a local test control plane", identity: "http://127.0.0.1:8180",
			ready: true, row: "identity", result: PreflightWarn, next: "the person's step"},
		{name: "already signed in to production", identity: adaptercloud.DefaultAPIBase,
			ready: true, row: "identity", result: PreflightOK, next: "clawdline cloud on"},
		{name: "commands already on", settings: `{"cloud_commands":true}`,
			ready: true, row: "commands", result: PreflightWarn},
		{name: "an identity file that cannot be read", identity: "unreadable", row: "identity", result: PreflightBlock},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.settings != "" {
				writeSettings(t, dir, c.settings)
			}
			switch c.identity {
			case "":
			case "unreadable":
				keys := filepath.Join(dir, cloudkeys.DirName)
				if err := os.MkdirAll(keys, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(keys, adaptercloud.IdentityFile), []byte("{not json"), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			default:
				writeIdentity(t, dir, c.identity)
			}
			report := Preflight(LinkOptions{Dir: dir})
			if report.Ready != c.ready {
				t.Errorf("ready %v, want %v: %+v", report.Ready, c.ready, report)
			}
			found := false
			for _, check := range report.Checks {
				if check.Name == c.row {
					found = true
					if check.Result != c.result {
						t.Errorf("%s: %s (%s), want %s", check.Name, check.Result, check.Detail, c.result)
					}
				}
			}
			if !found {
				t.Errorf("no %s row: %+v", c.row, report.Checks)
			}
			if c.next != "" && !strings.Contains(report.Next, c.next) {
				t.Errorf("next %q, want it to name %q", report.Next, c.next)
			}
		})
	}
}

// The preflight refuses the Swift app's directory the way the key store does,
// rather than reporting on it.
func TestThePreflightRefusesTheSwiftAppsDirectory(t *testing.T) {
	foreign := t.TempDir()
	report := Preflight(LinkOptions{Dir: foreign, ForeignDirs: []string{foreign}})
	if report.Ready {
		t.Fatalf("the Swift app's directory passed: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(foreign, cloudkeys.DirName)); !os.IsNotExist(err) {
		t.Errorf("the preflight created a key directory inside the Swift app's: %v", err)
	}
}
