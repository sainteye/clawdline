package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTypeFromStdinIsMarkedSecretAndLosesOneNewline(t *testing.T) {
	var errOut strings.Builder
	args, _, code := browserArgs("type", []string{"--stdin"}, strings.NewReader("hunter2\n"), &errOut)
	if code != 0 || args["text"] != "hunter2" || args["secret"] != true {
		t.Fatalf("args %v code %d %s", args, code, errOut.String())
	}
	if _, _, code := browserArgs("type", []string{"--stdin"}, strings.NewReader(""), &errOut); code != 2 {
		t.Fatal("empty standard input was accepted")
	}
}

func TestViewportCommandOnlySendsTheChosenPreset(t *testing.T) {
	for _, mode := range []string{"mobile", "desktop"} {
		var errOut strings.Builder
		args, _, code := browserArgs("viewport", []string{mode}, strings.NewReader(""), &errOut)
		if code != 0 || args["mode"] != mode {
			t.Fatalf("%s: %v code %d %s", mode, args, code, errOut.String())
		}
	}
	var errOut strings.Builder
	if _, _, code := browserArgs("viewport", []string{"100000x100000"}, strings.NewReader(""), &errOut); code != 2 {
		t.Fatal("an arbitrary viewport was accepted")
	}
}

func TestBrowserHostPollPinsLauncherModeAndProfile(t *testing.T) {
	for _, mode := range []string{"", "user_chrome"} {
		name := mode
		if name == "" {
			name = "legacy_isolated"
		}
		t.Run(name, func(t *testing.T) {
			state := t.TempDir()
			profile := filepath.Join(t.TempDir(), "ordinary Chrome")
			wantMode, wantProfile := mode, profile
			if mode == "" {
				wantMode = "isolated"
				wantProfile = filepath.Join(state, "chrome-profile")
				profile = ""
			}
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			seen := make(chan map[string]string, 2)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/browser/host/poll" || r.Header.Get("X-Clawdline-Orchestrator") != "synthetic-host-token" {
					t.Error("unexpected host request")
					w.WriteHeader(400)
					return
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				calls++
				if calls <= 2 {
					seen <- body
				}
				if calls >= 2 {
					writer.Close()
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"commands":[]}`)
			}))
			defer server.Close()
			t.Setenv("CLAWDLINE_NEXT_DIR", state)
			t.Setenv("CLAWDLINE_NEXT_PORT", strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port))
			t.Setenv("CLAWDLINE_BROWSER_MODE", mode)
			t.Setenv("CLAWDLINE_BROWSER_PROFILE", profile)
			if err := os.WriteFile(filepath.Join(state, "orchestrator-token"), []byte("synthetic-host-token"), 0600); err != nil {
				t.Fatal(err)
			}
			done := make(chan int, 1)
			var errors strings.Builder
			go func() { done <- runBrowserHost(input, io.Discard, &errors) }()
			go func() {
				data := []byte(`{"hello":{"mode":"spoofed","profile":"/spoofed","version":"test","browser":"fixture"}}`)
				binary.Write(writer, binary.LittleEndian, uint32(len(data)))
				writer.Write(data)
			}()
			select {
			case code := <-done:
				if code != 0 {
					t.Fatalf("host exited %d: %s", code, errors.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("host did not finish two polls")
			}
			for i := 0; i < 2; i++ {
				body := <-seen
				if body["mode"] != wantMode || body["profile"] != wantProfile {
					t.Fatalf("poll %d changed launcher binding: %v", i, body)
				}
			}
		})
	}
}

func TestBrowserInstallHostModeDefaultsOnlyForUserChrome(t *testing.T) {
	state, home := t.TempDir(), t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	t.Setenv("CLAWDLINE_NEXT_PORT", "7791")
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	id := strings.Repeat("a", 32)
	var out, errors strings.Builder
	if code := browserInstallHost([]string{"--extension-id", id}, &out, &errors); code != 2 {
		t.Fatalf("legacy isolated install without profile: %d %s", code, errors.String())
	}
	if code := browserInstallHost([]string{"--mode", "native", "--extension-id", id}, &out, &errors); code != 2 {
		t.Fatalf("native mode installed a host: %d", code)
	}
	if entries, err := os.ReadDir(state); err != nil || len(entries) != 0 {
		t.Fatalf("invalid install changed state: %v %v", entries, err)
	}
	out.Reset()
	errors.Reset()
	if code := browserInstallHost([]string{"--mode", "user_chrome", "--extension-id", id}, &out, &errors); code != 0 {
		t.Fatalf("user install: %d %s", code, errors.String())
	}
	var got struct{ Mode, Profile, Manifest, Launcher string }
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatal(err)
	}
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	case "linux":
		want = filepath.Join(home, "config", "google-chrome")
	case "windows":
		want = filepath.Join(home, "local", "Google", "Chrome", "User Data")
	default:
		t.Skip("no Chrome default for this platform")
	}
	if got.Mode != "user_chrome" || got.Profile != want || filepath.Base(got.Launcher) != "browser-host-user-chrome.sh" {
		t.Fatalf("wrong default: %+v", got)
	}
	if _, err := os.Stat(got.Manifest); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserHostRejectsNonHostModesBeforeOpeningTheBroker(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_DIR", t.TempDir())
	t.Setenv("CLAWDLINE_BROWSER_MODE", "native")
	t.Setenv("CLAWDLINE_BROWSER_PROFILE", t.TempDir())
	var errors strings.Builder
	if code := runBrowserHost(strings.NewReader(""), io.Discard, &errors); code != 2 || !strings.Contains(errors.String(), "isolated or user_chrome") {
		t.Fatalf("invalid host: %d %s", code, errors.String())
	}
}

func TestCloseNamesAnOwnedTabWithoutTreatingReleaseAsClose(t *testing.T) {
	var errOut strings.Builder
	args, _, code := browserArgs("close", []string{"--tab", "7"}, strings.NewReader(""), &errOut)
	if code != 0 || args["tab"] != 7 {
		t.Fatalf("close: %v code %d", args, code)
	}
	if _, _, code := browserArgs("close", []string{"all"}, strings.NewReader(""), &errOut); code != 2 {
		t.Fatal("an ambiguous close request was accepted")
	}
}

func TestExplicitBrowserFlagsStaySeparateFromPageArguments(t *testing.T) {
	for _, op := range []string{"open", "read", "type", "close", "viewport"} {
		rest := []string{"--browser", "isolated", "--explicit"}
		switch op {
		case "open":
			rest = append(rest, "https://example.com")
		case "type":
			rest = append(rest, "example text")
		case "viewport":
			rest = append(rest, "mobile")
		}
		var errors strings.Builder
		args, _, code := browserArgs(op, rest, strings.NewReader(""), &errors)
		if code != 0 || args["browser_mode"] != "isolated" || args["explicit"] != true {
			t.Fatalf("%s lost choice: %+v %d %s", op, args, code, errors.String())
		}
	}
	var errors strings.Builder
	if _, _, code := browserArgs("read", []string{"--explicit"}, strings.NewReader(""), &errors); code != 2 {
		t.Fatal("explicit without a browser was accepted")
	}
}
