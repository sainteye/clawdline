package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	taskTestID     = "a7100000-0000-4000-8000-000000000001"
	taskTestSecret = "5ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2"
)

func taskTestDir(t *testing.T, result string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), taskTestID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	task := `{"clawdline_protocol": 1, "task_id": "` + taskTestID + `", "kind": "custom"}`
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(task), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.json.tmp"), []byte(result), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func taskTestResult(secret string) string {
	return `{"clawdline_protocol": 1, "task_id": "` + taskTestID + `", "task_secret": "` + secret +
		`", "status": "success", "summary": "done"}`
}

// asked is what a stand-in broker was asked. The handler runs on the
// server's goroutine, so it is held under a lock.
type asked struct {
	mu                   sync.Mutex
	Method, Path, Secret string
}

func (a *asked) set(method, path, secret string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Method, a.Path, a.Secret = method, path, secret
}

func (a *asked) get() (method, path, secret string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Method, a.Path, a.Secret
}

// brokerAt is a stand-in broker answering …/complete as the test says.
func brokerAt(t *testing.T, status int, body string, seen *asked) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.set(r.Method, r.URL.Path, r.Header.Get("X-Clawdline-Task-Secret"))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

// The command publishes the result and asks the broker to collect it, with
// the task's own secret and nothing else.
func TestTaskFinishPublishesAndAsksForCollection(t *testing.T) {
	dir := taskTestDir(t, taskTestResult(taskTestSecret))
	var seen asked
	port := brokerAt(t, http.StatusOK, `{"ok":true}`, &seen)
	var out, errs bytes.Buffer
	if code := finishTask(&out, &errs, dir, port, true, http.DefaultClient); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
	if method, path, secret := seen.get(); method != http.MethodPost ||
		path != "/v1/orchestrator/tasks/"+taskTestID+"/complete" || secret != taskTestSecret {
		t.Fatalf("the broker was asked %s %s", method, path)
	}
	if _, err := os.Stat(filepath.Join(dir, "result.json")); err != nil {
		t.Fatal("result.json is not in place")
	}
	if !strings.Contains(out.String(), "task result preflight: valid") || strings.Contains(out.String(), taskTestSecret) {
		t.Fatalf("said: %s", out.String())
	}
}

// With no broker to ask — no loopback in the sandbox, no daemon on the port —
// the result is still in place and the command still exits 0: the file is the
// report.
func TestTaskFinishWithNoBrokerStillReports(t *testing.T) {
	dir := taskTestDir(t, taskTestResult(taskTestSecret))
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	var out, errs bytes.Buffer
	if code := finishTask(&out, &errs, dir, port, true, http.DefaultClient); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
	if !strings.Contains(out.String(), "Nothing is lost") {
		t.Fatalf("said: %s", out.String())
	}
}

// A result the broker says is not this task's is published and refused: the
// command says so and exits non-zero, with the way out.
func TestTaskFinishSaysWhenTheBrokerRefusesTheResult(t *testing.T) {
	dir := taskTestDir(t, taskTestResult(strings.Repeat("0", 64)))
	var seen asked
	port := brokerAt(t, http.StatusConflict,
		`{"error":{"code":"result_rejected","message":"result.json is there but is not this task's"}}`, &seen)
	var out, errs bytes.Buffer
	if code := finishTask(&out, &errs, dir, port, true, http.DefaultClient); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs.String(), "result_rejected") || !strings.Contains(errs.String(), "delete ") {
		t.Fatalf("said: %s", errs.String())
	}
}

// An invalid result is the old validator's line, and nothing is asked of the
// broker.
func TestTaskFinishRefusesAnInvalidResult(t *testing.T) {
	dir := taskTestDir(t, taskTestResult("<TASK_SECRET>"))
	var seen asked
	port := brokerAt(t, http.StatusOK, `{}`, &seen)
	var out, errs bytes.Buffer
	if code := finishTask(&out, &errs, dir, port, true, http.DefaultClient); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(errs.String(), "task result preflight: invalid — task_secret must be 64 lowercase hexadecimal characters") {
		t.Fatalf("said: %s", errs.String())
	}
	if method, _, _ := seen.get(); method != "" {
		t.Fatal("an invalid result was reported to the broker")
	}
}
