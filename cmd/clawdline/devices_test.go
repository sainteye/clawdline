package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// realDoor is the daemon's own handler over an empty directory, on a port of
// its own, and this machine's token as the daemon wrote it.
func realDoor(t *testing.T) localDoor {
	t.Helper()
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(t.TempDir(), "swift"))
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Dir: dir, Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}
	srv, err := httptransport.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.AuthReady(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener.Close()
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)
	data, err := os.ReadFile(filepath.Join(dir, devices.LocalTokenFile))
	if err != nil {
		t.Fatal(err)
	}
	return localDoor{base: ts.URL, token: strings.TrimSpace(string(data)), client: &http.Client{Timeout: 5 * time.Second}}
}

// openBrowser is what `clawdline open` asks for.
func openBrowser(t *testing.T, door localDoor) contract.BrowserDevice {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, door.base+"/v1/auth/devices/browser", strings.NewReader(`{"send":true}`))
	req.Header.Set("Authorization", "Bearer "+door.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := door.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var made contract.BrowserDevice
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&made) != nil || made.Token == "" {
		t.Fatalf("clawdline open: %s", res.Status)
	}
	return made
}

// statusWith is the status of a read that needs a paired device, asked with token.
func statusWith(t *testing.T, door localDoor, token, path string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, door.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := door.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// A browser `clawdline open` signed in is in `clawdline devices`, and after
// `clawdline devices revoke` its token is refused.
func TestDevicesRevokeRefusesTheBrowsersToken(t *testing.T) {
	door := realDoor(t)
	browser := openBrowser(t, door)
	if code := statusWith(t, door, browser.Token, "/v1/sessions"); code != http.StatusOK {
		t.Fatalf("the new browser was not let in: %d", code)
	}

	var out, errs bytes.Buffer
	if code := devicesList(&out, &errs, door, nil); code != 0 {
		t.Fatalf("list: exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), browser.ID) || !strings.Contains(out.String(), "Browser on this machine") ||
		!strings.Contains(out.String(), "read+send") || !strings.Contains(out.String(), "this machine's own key") {
		t.Fatalf("list:\n%s", out.String())
	}

	// Asked, and a no changes nothing.
	out.Reset()
	if code := devicesRevoke(&out, &errs, strings.NewReader("n\n"), door, []string{browser.ID}); code != 1 ||
		!strings.Contains(out.String(), "nothing was revoked") {
		t.Fatalf("declined: exit %d: %s", code, out.String())
	}
	if code := statusWith(t, door, browser.Token, "/v1/sessions"); code != http.StatusOK {
		t.Fatalf("a declined revoke took the key: %d", code)
	}

	out.Reset()
	if code := devicesRevoke(&out, &errs, strings.NewReader("y\n"), door, []string{browser.ID}); code != 0 {
		t.Fatalf("revoke: exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "Browser on this machine") || !strings.Contains(out.String(), "revoked") {
		t.Fatalf("revoke said:\n%s", out.String())
	}
	if code := statusWith(t, door, browser.Token, "/v1/sessions"); code != http.StatusUnauthorized {
		t.Fatalf("the revoked browser's token still answers %d", code)
	}
	out.Reset()
	if code := devicesList(&out, &errs, door, nil); code != 0 || strings.Contains(out.String(), browser.ID) {
		t.Fatalf("still listed:\n%s", out.String())
	}
	// This machine's own key is untouched.
	if code := statusWith(t, door, door.token, "/v1/auth/devices"); code != http.StatusOK {
		t.Fatalf("the local token lost the list: %d", code)
	}
}

// A browser device cannot list or revoke: it is refused, so a page signed in
// with one cannot take anybody's key, its own included, through these routes.
func TestDevicesRoutesRefuseABrowser(t *testing.T) {
	door := realDoor(t)
	a, b := openBrowser(t, door), openBrowser(t, door)
	if code := statusWith(t, door, a.Token, "/v1/auth/devices"); code != http.StatusForbidden {
		t.Fatalf("a browser listed devices: %d", code)
	}
	as := localDoor{base: door.base, token: a.Token, client: door.client}
	var out, errs bytes.Buffer
	if code := devicesRevoke(&out, &errs, strings.NewReader("y\n"), as, []string{b.ID}); code != 1 ||
		!strings.Contains(errs.String(), "forbidden") {
		t.Fatalf("a browser revoked another: exit %d: %s", code, errs.String())
	}
	if code := statusWith(t, door, b.Token, "/v1/sessions"); code != http.StatusOK {
		t.Fatalf("the other browser lost its key: %d", code)
	}
}

// An id nobody has is said so, before anything is asked or posted.
func TestDevicesRevokeUnknownID(t *testing.T) {
	door := realDoor(t)
	var out, errs bytes.Buffer
	if code := devicesRevoke(&out, &errs, strings.NewReader("y\n"), door, []string{"nobody"}); code != 1 ||
		!strings.Contains(errs.String(), "no device signed in to this machine has the id nobody") || out.Len() != 0 {
		t.Fatalf("exit %d: out %q errs %q", code, out.String(), errs.String())
	}
}
