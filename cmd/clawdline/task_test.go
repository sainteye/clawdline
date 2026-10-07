package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

	"github.com/sainteye/clawdline/internal/contract"
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

func testServerPort(t *testing.T, handler http.Handler) int {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

func TestTaskGateEvidenceStreamsExactBytesAndRequiresDurableReceipt(t *testing.T) {
	dir := acceptTestDir(t)
	body := []byte("focused test output\n")
	file := filepath.Join(t.TempDir(), "focused.log")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	hexDigest := hex.EncodeToString(digest[:])
	port := testServerPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orchestrator/tasks/"+taskTestID+"/gate-evidence" ||
			r.Header.Get("X-Clawdline-Task-Secret") != taskTestSecret ||
			r.Header.Get("X-Clawdline-Artifact-ID") != "focused-log" || r.Header.Get("Content-Type") != "text/plain" ||
			r.Header.Get("X-Clawdline-Content-SHA256") != hexDigest || r.Header.Get("X-Clawdline-Byte-Count") != strconv.Itoa(len(body)) ||
			!bytes.Equal(got, body) || !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "gate-evidence-") {
			t.Errorf("bad gate evidence request: %s %s headers=%v body=%q", r.Method, r.URL.Path, r.Header, got)
		}
		_ = json.NewEncoder(w).Encode(contract.WorkGateEvidenceReceipt{
			AcceptedAt: 42, ArtifactID: "focused-log", ByteCount: int64(len(body)), MediaType: "text/plain", Sha256: hexDigest, TaskID: taskTestID,
		})
	}))
	var out, errs bytes.Buffer
	if code := submitGateEvidence(&out, &errs, dir, file, "focused-log", "text/plain", taskTestSecret, port, http.DefaultClient); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
	if !strings.Contains(out.String(), "gate evidence focused-log accepted") || strings.Contains(out.String()+errs.String(), taskTestSecret) {
		t.Fatalf("said: %s %s", out.String(), errs.String())
	}
}

func TestTaskGateResultSendsExactClosedVerdictAndFailsClosedOffline(t *testing.T) {
	dir := acceptTestDir(t)
	body := []byte(`{"round_id":"a7111111-1111-4111-8111-111111111111","task_id":"` + taskTestID + `","candidate_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","candidate_tree":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","criteria_version":1,"criteria_digest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","verdict":"PASS","claims":[{"criterion":"focused check","state":"passed","evidence":["passed"],"evidence_artifacts":[],"reason":""}],"summary":"verified"}`)
	file := filepath.Join(t.TempDir(), "verdict.json")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	hexDigest := hex.EncodeToString(digest[:])
	port := testServerPort(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Clawdline-Task-Secret") != taskTestSecret || r.Header.Get("Content-Type") != "application/json" ||
			!bytes.Equal(got, body) || !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "gate-result-") {
			t.Errorf("bad gate result request: headers=%v body=%q", r.Header, got)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(contract.WorkGateResultReceipt{
			AcceptedAt: 42, TaskID: taskTestID, ByteCount: int64(len(body)), Sha256: hexDigest, Verdict: contract.WorkGateVerdictPASS,
		})
	}))
	var out, errs bytes.Buffer
	if code := submitGateResult(&out, &errs, dir, file, taskTestSecret, port, http.DefaultClient); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	offlinePort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	out.Reset()
	errs.Reset()
	if code := submitGateResult(&out, &errs, dir, file, taskTestSecret, offlinePort, http.DefaultClient); code != 1 {
		t.Fatalf("offline exit %d: %s %s", code, out.String(), errs.String())
	}
	if !strings.Contains(errs.String(), "Nothing local counts") && !strings.Contains(errs.String(), "cannot finish this gate") {
		t.Fatalf("offline result did not fail closed: %s", errs.String())
	}
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
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	out.Reset()
	errs.Reset()
	secondDir := taskTestDir(t, taskTestResult(taskTestSecret))
	if code := finishTask(&out, &errs, secondDir, port, true, http.DefaultClient); code != 0 ||
		!strings.Contains(out.String(), "Es gingen keine Daten verloren") ||
		!strings.Contains(out.String(), "result.json") {
		t.Fatalf("German offline completion lost its saved result: %d %q %q", code, out.String(), errs.String())
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

// Every violation of one result is printed, the first on the old line and
// each further one on its own, so a child corrects them in one pass.
func TestTaskFinishPrintsEveryViolation(t *testing.T) {
	result := strings.Replace(taskTestResult("<TASK_SECRET>"), `"status": "success"`, `"status": "done"`, 1)
	if result == taskTestResult("<TASK_SECRET>") {
		t.Fatal("the test result has no status to break")
	}
	dir := taskTestDir(t, result)
	var out, errs bytes.Buffer
	if code := finishTask(&out, &errs, dir, 0, false, http.DefaultClient); code != 1 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(errs.String(), "\n")
	if lines[0] != "task result preflight: invalid — task_secret must be 64 lowercase hexadecimal characters" {
		t.Fatalf("first line: %q", lines[0])
	}
	if len(lines) < 2 || lines[1] != "  - status must be success or failure" {
		t.Fatalf("said: %s", errs.String())
	}
}

// A task directory as the broker leaves it, with nothing written by the child.
func acceptTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), taskTestID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	task := `{"clawdline_protocol": 1, "task_id": "` + taskTestID + `", "kind": "custom"}`
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(task), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Signing online is one POST to …/accepted with the task's own secret, and
// the secret is never said back.
func TestTaskAcceptSignsWithTheBroker(t *testing.T) {
	dir := acceptTestDir(t)
	var seen asked
	port := brokerAt(t, http.StatusOK, `{"ok":true,"task":{"id":"`+taskTestID+`"}}`, &seen)
	var out, errs bytes.Buffer
	if code := acceptTask(&out, &errs, dir, port, taskTestSecret, http.DefaultClient); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
	if method, path, secret := seen.get(); method != http.MethodPost ||
		path != "/v1/orchestrator/tasks/"+taskTestID+"/accepted" || secret != taskTestSecret {
		t.Fatalf("the broker was asked %s %s", method, path)
	}
	if _, err := os.Stat(filepath.Join(dir, "accepted.json")); !os.IsNotExist(err) {
		t.Fatal("a receipt the broker took was also left as a file")
	}
	if strings.Contains(out.String()+errs.String(), taskTestSecret) {
		t.Fatalf("the secret was printed: %s %s", out.String(), errs.String())
	}
}

// With no broker to reach, the receipt is the file the broker collects, and
// the command still exits 0: the child has signed.
func TestTaskAcceptWithNoBrokerLeavesTheReceipt(t *testing.T) {
	dir := acceptTestDir(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	var out, errs bytes.Buffer
	if code := acceptTask(&out, &errs, dir, port, taskTestSecret, http.DefaultClient); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errs.String())
	}
	body, err := os.ReadFile(filepath.Join(dir, "accepted.json"))
	if err != nil {
		t.Fatal("no accepted.json was left")
	}
	if string(body) != `{"task_secret":"`+taskTestSecret+`"}`+"\n" {
		t.Fatalf("accepted.json is %s", body)
	}
	if info, _ := os.Stat(filepath.Join(dir, "accepted.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("accepted.json is %v, readable beyond this user", info.Mode().Perm())
	}
	if strings.Contains(out.String()+errs.String(), taskTestSecret) {
		t.Fatalf("the secret was printed: %s %s", out.String(), errs.String())
	}
}

// A broker that refuses the secret is a refusal, not a reason to leave a file.
func TestTaskAcceptSaysARefusal(t *testing.T) {
	dir := acceptTestDir(t)
	var seen asked
	port := brokerAt(t, http.StatusForbidden, `{"error":{"code":"forbidden","message":"not this task's secret"}}`, &seen)
	var out, errs bytes.Buffer
	if code := acceptTask(&out, &errs, dir, port, strings.Repeat("0", 64), http.DefaultClient); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errs.String(), "forbidden") {
		t.Fatalf("said: %s", errs.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "accepted.json")); !os.IsNotExist(err) {
		t.Fatal("a refused secret was left as a receipt")
	}
}

// The secret comes from the environment or stdin. On the command line it
// would be in argv for anybody's `ps`, so an extra argument is refused
// before anything is sent, and the refusal does not repeat it.
func TestTaskAcceptTakesTheSecretOnlyFromEnvOrStdin(t *testing.T) {
	if _, _, err := acceptArgs([]string{"/t/dir", taskTestSecret}); err == nil {
		t.Fatal("a secret on the command line was accepted")
	} else if strings.Contains(err.Error(), taskTestSecret) {
		t.Fatalf("the refusal repeats the secret: %v", err)
	}
	if dir, port, err := acceptArgs([]string{"--port", "7791", "/t/dir"}); err != nil || dir != "/t/dir" || port != 7791 {
		t.Fatalf("acceptArgs = %q %d %v", dir, port, err)
	}

	env := envOf(map[string]string{"CLAWDLINE_TASK_SECRET": taskTestSecret})
	if got, err := acceptSecret(env, strings.NewReader("")); err != nil || got != taskTestSecret {
		t.Fatalf("from the environment: %q %v", got, err)
	}
	if got, err := acceptSecret(envOf(nil), strings.NewReader(taskTestSecret+"\n")); err != nil || got != taskTestSecret {
		t.Fatalf("from stdin: %q %v", got, err)
	}
	if _, err := acceptSecret(envOf(nil), strings.NewReader("<TASK_SECRET>")); err == nil ||
		strings.Contains(err.Error(), "<TASK_SECRET>") {
		t.Fatalf("a placeholder was taken, or repeated: %v", err)
	}
}

// `task show` is the root's compact view of a child: what a completion
// notice sends it to instead of the whole result.json.
func TestTaskShowIsTheCompactView(t *testing.T) {
	task := `{"ok":true,"task":{"id":"` + taskTestID + `","title":"shorter protocol","state":"success",` +
		`"verdict":"result.json collected","summary":"broker summary",` +
		`"result":{"status":"success","summary":"Did the whole thing.\nOn two lines.",` +
		`"symbols":["a","b","c"],"artifacts":["artifacts/x"],` +
		`"verification":{"runs":2,"seconds":40,"last":"pass","scope":"go test ./..."},` +
		`"leftovers":[{"title":"Roots read one line per child","why":"out of scope","suggested_acceptance":"x"}]},` +
		`"landing":{"state":"pending","settlement":"branch_carries_commits"},` +
		`"worktree":{"base":"b","branch":"clawdline/task/x","path":"/w/x","repository":"/r",` +
		`"branch_exists":null,"commits":null,"dirty":null,"merged":null}}}`
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, task })
	var out, errs bytes.Buffer
	if code := showTask(&out, &errs, b, taskTestID, false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	got := out.String()
	for _, want := range []string{"success", "result.json collected", "Did the whole thing.\nOn two lines.",
		"Roots read one line per child", "3 symbols", "2 runs, last pass: go test ./...",
		"pending (branch_carries_commits)", "/w/x",
		"a cherry-pick is recorded with clawdline task land " + taskTestID + " landed --target <branch> --commit <commit>"} {
		if !strings.Contains(got, want) {
			t.Errorf("task show does not say %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"out of scope", `"a"`, "artifacts/x"} {
		if strings.Contains(got, gone) {
			t.Errorf("task show carries %q, which is --json's:\n%s", gone, got)
		}
	}
	if req := s.requests(); len(req) != 1 || req[0].Method != http.MethodGet ||
		req[0].EscapedPath != "/v1/orchestrator/tasks/"+taskTestID {
		t.Fatalf("asked %+v", req)
	}

	out.Reset()
	if code := showTask(&out, &errs, b, taskTestID, true); code != 0 || !strings.Contains(out.String(), `"out of scope"`) {
		t.Fatalf("--json: exit %d:\n%s", code, out.String())
	}
}

func TestTaskShowMakesAnUncommittedPermissionFailureResumable(t *testing.T) {
	task := `{"ok":true,"task":{"id":"` + taskTestID + `","title":"build blocked","state":"failure",` +
		`"result":{"status":"failure","summary":"Implemented parser; build remains unverified; changes are uncommitted.",` +
		`"artifacts":["internal/parser.go","internal/parser_test.go"],` +
		`"verification":{"runs":1,"seconds":3,"last":"fail",` +
		`"scope":"tools/check.sh: mkdir shared dependency cache: permission denied"}},` +
		`"landing":{"state":"pending","settlement":"branch_empty"},` +
		`"worktree":{"base":"b","branch":"clawdline/task/x","path":"/w/x","repository":"/r",` +
		`"branch_exists":true,"commits":0,"dirty":true,"merged":false}}}`
	_, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, task })
	var out, errs bytes.Buffer
	if code := showTask(&out, &errs, b, taskTestID, false); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	for _, want := range []string{"state:        failure", "last fail: tools/check.sh: mkdir shared dependency cache: permission denied",
		"rerun:", "changed file:  internal/parser.go", "changed file:  internal/parser_test.go",
		"branch:       0 delivery commit(s)", "uncommitted:  true", "branch_empty", "/w/x"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in resumable handoff:\n%s", want, out.String())
		}
	}
}
