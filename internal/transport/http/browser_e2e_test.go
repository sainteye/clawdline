package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// TestBrowserBridgeEndToEnd drives a real Chromium through the whole path:
// `clawdline browser` → this server → `clawdline browser-host` (started by
// Chromium through native messaging) → extension/chrome. It runs only when
// both are named:
//
//	CLAWDLINE_BROWSER_E2E_BIN=<built clawdline> CLAWDLINE_BROWSER_E2E_CHROMIUM=<Chromium binary> \
//	  go test ./internal/transport/http -run TestBrowserBridgeEndToEnd -v
//
// Branded Chrome ignores --load-extension, so this needs Chromium. Nothing
// touches the person's daemon or Chrome profile: the profile is a temporary
// directory and the host is registered inside it.
func TestBrowserBridgeEndToEnd(t *testing.T) {
	bin, chromium := os.Getenv("CLAWDLINE_BROWSER_E2E_BIN"), os.Getenv("CLAWDLINE_BROWSER_E2E_CHROMIUM")
	if bin == "" || chromium == "" {
		t.Skip("set CLAWDLINE_BROWSER_E2E_BIN and CLAWDLINE_BROWSER_E2E_CHROMIUM to run")
	}
	conversation := "20000000-0000-4000-8000-000000000001"
	live := session.Session{ID: "pane-browser", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: conversation, State: session.StateWorking, Label: "Browser test"}
	s := paneServer(t, &pane{s: live})
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{live} }

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// Real paths: macOS's TMPDIR is under the /var symlink.
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	s.cfg = config.Config{Dir: dir, Port: port}
	t.Cleanup(func() { gates.Delete(dir) })
	if _, err := s.gate().files.MachineToken(); err != nil {
		t.Fatal(err)
	}
	if _, err := nextconfig.Open(dir).Set(map[string]any{"browser_enabled": true}); err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: s.Handler()}
	go srv.Serve(listener)
	t.Cleanup(func() { srv.Close() })

	// Pages to browse, served beside the daemon.
	pages := http.NewServeMux()
	pages.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><meta charset=utf-8><title>Form</title><body style="font:16px system-ui;margin:40px">
<h1>Sign in to the test site</h1>
<label>Name <input id=name aria-label="Name"></label><br><br>
<label>Password <input id=pw type=password aria-label="Password"></label><br><br>
<button id=go onclick="document.getElementById('out').textContent='Hello, '+document.getElementById('name').value">Continue</button>
<input type=password value="synthetic-unlabelled-password"><input placeholder="Code" value="synthetic-unlabelled-code"><p id=out></p></body>`))
	})
	pageListener, _ := net.Listen("tcp", "127.0.0.1:0")
	pageSrv := &http.Server{Handler: pages}
	go pageSrv.Serve(pageListener)
	t.Cleanup(func() { pageSrv.Close() })
	formURL := "http://" + pageListener.Addr().String() + "/form"

	repo, _ := filepath.Abs("../../..")
	ext, _ := filepath.EvalSymlinks(filepath.Join(repo, "extension", "chrome"))
	sum := sha256.Sum256([]byte(ext))
	id := []byte(hex.EncodeToString(sum[:16]))
	for i, c := range id {
		v, _ := strconv.ParseUint(string(c), 16, 8)
		id[i] = byte('a' + v)
	}
	profile, _ := filepath.EvalSymlinks(t.TempDir())
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "CLAWDLINE_NEXT_DIR=" + dir,
		"CLAWDLINE_NEXT_PORT=" + strconv.Itoa(port), "CLAUDE_CODE_SESSION_ID=" + conversation}
	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errOut.String(), code
	}
	if out, errOut, code := run("browser", "install-host", "--extension-id", string(id), "--chrome-dir", profile); code != 0 {
		t.Fatalf("install-host: %d %s %s", code, out, errOut)
	}
	if _, errOut, code := run("browser", "open", formURL); code == 0 || !strings.Contains(errOut, "browser_extension_missing") {
		t.Fatalf("before Chromium starts, open should be refused browser_extension_missing: %d %s", code, errOut)
	}
	chrome := exec.Command(chromium, "--user-data-dir="+profile, "--load-extension="+ext, "--disable-extensions-except="+ext,
		"--no-first-run", "--no-default-browser-check", "--use-mock-keychain", "--window-size=1280,860", "about:blank")
	// TestMain gives this package an empty HOME; --use-mock-keychain keeps
	// Chromium from stalling on the macOS keychain there. The launcher pins
	// the daemon itself, so the host needs nothing else from this environment.
	chrome.Env = os.Environ()
	logPath := filepath.Join(t.TempDir(), "chromium.log")
	logFile, _ := os.Create(logPath)
	chrome.Stderr = logFile
	chrome.Args = append(chrome.Args[:1], append([]string{"--enable-logging=stderr", "--v=0", "--remote-debugging-port=9357"}, chrome.Args[1:]...)...)
	if err := chrome.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { chrome.Process.Kill(); chrome.Wait() })
	deadline := time.Now().Add(20 * time.Second)
	for !s.browser().Status().Connected {
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(logPath)
			var lines []string
			for _, l := range strings.Split(string(data), "\n") {
				if strings.Contains(strings.ToLower(l), "native") || strings.Contains(l, "extension") || strings.Contains(l, "ERROR") {
					lines = append(lines, l)
				}
			}
			if len(lines) > 30 {
				lines = lines[len(lines)-30:]
			}
			targets := ""
			if res, err := http.Get("http://127.0.0.1:9357/json"); err == nil {
				var list []struct{ Type, URL string }
				json.NewDecoder(res.Body).Decode(&list)
				res.Body.Close()
				for _, l := range list {
					targets += l.Type + " " + l.URL + "\n"
				}
			}
			alive, _ := exec.Command("ps", "-p", strconv.Itoa(chrome.Process.Pid), "-o", "pid,stat,command").CombinedOutput()
			logData, _ := os.ReadFile(logPath)
			if len(logData) > 2000 {
				logData = logData[len(logData)-2000:]
			}
			t.Logf("targets:\n%s\nchromium process:\n%s\nlog tail:\n%s", targets, alive, logData)
			ps, _ := exec.Command("sh", "-c", "ps -ax -o pid,command | grep browser-host | grep -v grep").CombinedOutput()
			manifest, _ := os.ReadFile(filepath.Join(profile, "NativeMessagingHosts", "com.clawdline.browser.json"))
			t.Fatalf("the browser host never polled (id %s)\nprocesses:\n%s\nmanifest:\n%s\nChromium said:\n%s", id, ps, manifest, strings.Join(lines, "\n"))
		}
		time.Sleep(200 * time.Millisecond)
	}

	mustJSON := func(name, out, errOut string, code int, into any) {
		t.Helper()
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", name, code, errOut)
		}
		if into != nil && json.Unmarshal([]byte(out), into) != nil {
			t.Fatalf("%s: not JSON: %s", name, out)
		}
	}
	var opened struct {
		Value struct {
			Tab   int    `json:"tab"`
			Title string `json:"title"`
		} `json:"value"`
	}
	out, errOut, code := run("browser", "open", formURL)
	mustJSON("open", out, errOut, code, &opened)
	if opened.Value.Tab == 0 || opened.Value.Title != "Form" {
		t.Fatalf("open: %s", out)
	}
	var page struct {
		Value struct {
			Text     string `json:"text"`
			Elements []struct {
				Label string `json:"label"`
			} `json:"elements"`
		} `json:"value"`
	}
	out, errOut, code = run("browser", "read")
	mustJSON("read", out, errOut, code, &page)
	if strings.Contains(string(out), "synthetic-unlabelled-password") || strings.Contains(string(out), "synthetic-unlabelled-code") {
		t.Fatal("read exposed form field values")
	}
	if !strings.Contains(page.Value.Text, "Sign in to the test site") || len(page.Value.Elements) < 3 {
		t.Fatalf("read: %s", out)
	}
	out, errOut, code = run("browser", "click", "Name")
	mustJSON("click Name", out, errOut, code, nil)
	out, errOut, code = run("browser", "type", "王小明")
	mustJSON("type", out, errOut, code, nil)
	out, errOut, code = run("browser", "click", "Password")
	mustJSON("click Password", out, errOut, code, nil)
	out, errOut, code = run("browser", "type", "hunter2")
	mustJSON("type password", out, errOut, code, nil)
	out, errOut, code = run("browser", "click", "Continue")
	mustJSON("click Continue", out, errOut, code, nil)
	out, errOut, code = run("browser", "read")
	mustJSON("read after", out, errOut, code, &page)
	if !strings.Contains(page.Value.Text, "Hello, 王小明") {
		t.Fatalf("the form did not receive the typed Chinese name: %s", page.Value.Text)
	}
	shot := filepath.Join(t.TempDir(), "tab.jpg")
	out, errOut, code = run("browser", "screenshot", "--out", shot)
	mustJSON("screenshot", out, errOut, code, nil)
	if data, err := os.ReadFile(shot); err != nil || len(data) < 1000 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("screenshot is not a JPEG: %v %d bytes", err, len(data))
	}
	if keep := os.Getenv("CLAWDLINE_BROWSER_E2E_SHOT"); keep != "" {
		data, _ := os.ReadFile(shot)
		os.WriteFile(keep, data, 0o644)
	}

	// The live panel's own view: the page's CSS size comes with the picture,
	// so a tap on it can become a click at the same point.
	raw, err := s.browser().PersonDo(context.Background(), "screenshot", opened.Value.Tab, nil)
	var view struct {
		Width, Height int
		Data, Title   string
	}
	if err != nil || json.Unmarshal(raw, &view) != nil || view.Width < 300 || view.Height < 200 || view.Data == "" || view.Title != "Form" {
		t.Fatalf("person screenshot: %v %d×%d title %q", err, view.Width, view.Height, view.Title)
	}
	if _, err := s.browser().PersonDo(context.Background(), "click", opened.Value.Tab, json.RawMessage(`{"x":10,"y":10}`)); err == nil {
		t.Fatal("the person clicked without taking the tab over")
	}

	// The person takes the tab over: the agent is refused and learns why.
	s.browser().SetPersonControl(opened.Value.Tab, true)
	if _, err := s.browser().PersonDo(context.Background(), "key", opened.Value.Tab, json.RawMessage(`{"key":"Tab"}`)); err != nil {
		t.Fatalf("the person's key during takeover: %v", err)
	}
	if _, errOut, code := run("browser", "read"); code == 0 || !strings.Contains(errOut, "browser_person_controlling") {
		t.Fatalf("during takeover: %d %s", code, errOut)
	}
	s.browser().SetPersonControl(opened.Value.Tab, false)
	// The emergency stop: an agent may stop, but not resume.
	if out, errOut, code := run("browser", "stop"); code != 0 {
		t.Fatalf("stop: %s %s", out, errOut)
	}
	if _, errOut, code := run("browser", "read"); code == 0 || !strings.Contains(errOut, "browser_stopped") {
		t.Fatalf("after stop: %d %s", code, errOut)
	}
	s.browser().SetStopped(false)

	var audit struct {
		Rows []struct {
			Op       string `json:"op"`
			Text     string `json:"text"`
			TextLen  int    `json:"text_length"`
			Password bool   `json:"password"`
			Site     string `json:"site"`
			Target   string `json:"target"`
		} `json:"rows"`
	}
	out, errOut, code = run("browser", "audit", "--limit", "50")
	mustJSON("audit", out, errOut, code, &audit)
	var typedName, typedPassword, clickedContinue bool
	for _, r := range audit.Rows {
		if r.Op == "type" && r.Text == "王小明" && !r.Password {
			typedName = true
		}
		if r.Op == "type" && r.Password && r.Text == "" && r.TextLen == 7 {
			typedPassword = true
		}
		if r.Op == "click" && r.Target == "Continue" && r.Site == "127.0.0.1" {
			clickedContinue = true
		}
	}
	if !typedName || !typedPassword || !clickedContinue {
		t.Fatalf("audit is missing a row (name %v, password %v, continue %v): %s", typedName, typedPassword, clickedContinue, out)
	}
	t.Logf("audit rows: %d", len(audit.Rows))
}
