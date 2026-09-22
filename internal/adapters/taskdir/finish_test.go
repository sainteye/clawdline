package taskdir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	finishID     = "a7000000-0000-4000-8000-000000000001"
	finishSecret = "5ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2e75ec2"
)

// finishDir is a task directory with task.json and result.json.tmp written.
func finishDir(t *testing.T, task, result string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), finishID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(task), 0o600); err != nil {
		t.Fatal(err)
	}
	if result != "" {
		if err := os.WriteFile(filepath.Join(dir, "result.json.tmp"), []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func plainTask(kind string) string {
	return `{"clawdline_protocol": 1, "task_id": "` + finishID + `", "kind": "` + kind + `", "title": "t"}`
}

func resultWith(extra string) string {
	return `{"clawdline_protocol": 1, "task_id": "` + finishID + `", "task_secret": "` + finishSecret +
		`", "status": "success", "summary": "done"` + extra + `}`
}

const closedReview = `, "review": {"verdict": "changes_required", "axes": [
 {"axis": "specification", "status": "findings", "findings": [
   {"id": "f-1", "severity": "important", "summary": "s", "evidence": ["a.go:1"]}]},
 {"axis": "repository_invariants", "status": "pass", "findings": []},
 {"axis": "runtime_failure_behavior", "status": "pass", "findings": []}]}`

// A valid result is published whole — result.json holds exactly the bytes that
// were checked — and the tmp file and the marker are gone, as after the old
// `mv` and `rm`.
func TestAFinishPublishesExactlyTheCheckedBytes(t *testing.T) {
	result := resultWith(`, "symbols": ["Finish"], "verification": {"runs": 1, "seconds": 2, "last": "pass", "scope": "go test"}`)
	dir := finishDir(t, plainTask("custom"), result)
	done, err := Finish(dir)
	if err != nil {
		t.Fatal(err)
	}
	if done.TaskID != finishID || done.Secret != finishSecret || done.Already {
		t.Fatalf("%+v", done)
	}
	body, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil || string(body) != result {
		t.Fatalf("result.json = %q, %v", body, err)
	}
	for _, gone := range []string{"result.json.tmp", "result.json.ready"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s is still there", gone)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "finishing") || strings.Contains(e.Name(), "writing") {
			t.Errorf("left behind %s", e.Name())
		}
	}
	// The broker reads it as it reads any result.
	r, _, err := Root{Dir: filepath.Dir(dir)}.ReadResult(finishID)
	if err != nil || r.Summary != "done" || r.Symbols[0] != "Finish" {
		t.Fatalf("the broker reads %+v, %v", r, err)
	}
	// A second run of the same finish finds the same bytes and changes nothing.
	if err := os.WriteFile(filepath.Join(dir, "result.json.tmp"), []byte(result), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := Finish(dir); err != nil || !again.Already {
		t.Fatalf("the same finish twice: %+v, %v", again, err)
	}
}

// Create-if-absent (D16): a result.json already there is never replaced — the
// old line's `mv` would have — and the tmp file is left for the child.
func TestAFinishNeverReplacesAResultAlreadyThere(t *testing.T) {
	dir := finishDir(t, plainTask("custom"), resultWith(`, "summary2": 1`))
	first := resultWith("")
	if err := os.WriteFile(filepath.Join(dir, "result.json"), []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Finish(dir); !errors.Is(err, ErrResultDiffers) {
		t.Fatalf("finish over another result answered %v", err)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "result.json")); string(body) != first {
		t.Fatalf("result.json was replaced: %s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "result.json.tmp")); err != nil {
		t.Fatal("the tmp file was taken away")
	}
	if _, err := os.Stat(filepath.Join(dir, "result.json.ready")); !os.IsNotExist(err) {
		t.Fatal("a marker for bytes that will never be published was left")
	}
}

// The marker is the broker's own: a finish that stopped after writing it is
// adopted by the broker exactly as the old validator's was.
func TestTheFinishMarkerIsTheOneTheBrokerAdopts(t *testing.T) {
	result := resultWith("")
	dir := finishDir(t, plainTask("custom"), result)
	if err := writeReady(dir, finishID, []byte(result)); err != nil {
		t.Fatal(err)
	}
	root := Root{Dir: filepath.Dir(dir)}
	r, body, ok := root.ReadReady(finishID)
	if !ok || r.TaskID != finishID || string(body) != result {
		t.Fatalf("the broker does not take the marker: %+v %v", r, ok)
	}
}

// The old validator's rules, one for one, each with its own sentence. Nothing
// is written for any of them.
func TestAFinishRefusesWhatTheOldValidatorRefused(t *testing.T) {
	cases := []struct {
		name, task, result, reason string
	}{
		{"no tmp file", plainTask("custom"), "", "result.json.tmp is not readable JSON"},
		{"not JSON", plainTask("custom"), "{", "result.json.tmp is not readable JSON"},
		{"trailing bytes", plainTask("custom"), resultWith("") + " x", "result.json.tmp is not readable JSON"},
		{"not an object", plainTask("custom"), "[1]", "result.json.tmp must contain one JSON object"},
		{"a task.json with no identity", `{"clawdline_protocol": 2, "task_id": "` + finishID + `"}`, resultWith(""),
			"task.json has no valid protocol identity"},
		{"protocol", plainTask("custom"), strings.Replace(resultWith(""), `"clawdline_protocol": 1`, `"clawdline_protocol": "1"`, 1),
			"clawdline_protocol must be 1"},
		{"another task", plainTask("custom"), strings.Replace(resultWith(""), finishID, "a7000000-0000-4000-8000-000000000002", 1),
			"task_id must be a lowercase UUID matching task.json"},
		{"secret", plainTask("custom"), strings.Replace(resultWith(""), finishSecret, "<TASK_SECRET>", 1),
			"task_secret must be 64 lowercase hexadecimal characters"},
		{"status", plainTask("custom"), strings.Replace(resultWith(""), `"success"`, `"done"`, 1), "status must be success or failure"},
		{"verification with a fraction", plainTask("custom"), resultWith(`, "verification": {"runs": 1.5, "seconds": 0, "last": "pass", "scope": "x"}`),
			"verification must contain"},
		{"verification with an empty scope", plainTask("custom"), resultWith(`, "verification": {"runs": 1, "seconds": 0, "last": "pass", "scope": "  "}`),
			"verification must contain"},
		{"verification null", plainTask("custom"), resultWith(`, "verification": null`), "verification must contain"},
		// Past the old validator: JavaScript's integers, which it let through
		// and the broker cannot read (it would never settle the task).
		{"an integer written as 2.0", plainTask("custom"), resultWith(`, "verification": {"runs": 1, "seconds": 2.0, "last": "pass", "scope": "x"}`),
			"result.json.tmp is not what the broker reads"},
		{"a protocol written as 1.0", plainTask("custom"), strings.Replace(resultWith(""), `"clawdline_protocol": 1`, `"clawdline_protocol": 1.0`, 1),
			"result.json.tmp is not what the broker reads"},
		{"a symbol that is not a name", plainTask("custom"), resultWith(`, "symbols": ["a", 2]`),
			"result.json.tmp is not what the broker reads"},
		{"a review task with no receipt", plainTask("code-review"), resultWith(""),
			"a successful review task requires a closed review receipt"},
		{"a review graph node with no receipt",
			`{"clawdline_protocol": 1, "task_id": "` + finishID + `", "kind": "custom", "graph": {"current_node": "r", "nodes": [{"id": "b", "kind": "build"}, {"id": "r", "kind": "review"}]}}`,
			resultWith(""), "a successful review task requires a closed review receipt"},
		{"a verdict its findings disagree with", plainTask("review"),
			resultWith(strings.Replace(closedReview, "changes_required", "safe_to_land", 1)), "review verdict must agree with its findings"},
		{"two axes", plainTask("review"),
			resultWith(`, "review": {"verdict": "safe_to_land", "axes": []}`), "review must contain only a valid verdict and exactly three axes"},
		{"a passing axis with a finding", plainTask("review"),
			resultWith(strings.Replace(closedReview, `"status": "findings"`, `"status": "pass"`, 1)),
			"a passing axis has no findings and a findings axis has at least one"},
		{"a finding with no evidence", plainTask("review"),
			resultWith(strings.Replace(closedReview, `["a.go:1"]`, `[]`, 1)), "each review finding must use the exact"},
		{"a finding id that is not a slug", plainTask("review"),
			resultWith(strings.Replace(closedReview, `"f-1"`, `"-f"`, 1)), "each review finding must use the exact"},
	}
	for _, c := range cases {
		dir := finishDir(t, c.task, c.result)
		_, err := Finish(dir)
		var invalid InvalidResult
		if !errors.As(err, &invalid) || !strings.HasPrefix(invalid.Reason, c.reason) {
			t.Errorf("%s: %v, want %q", c.name, err, c.reason)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "task result preflight: invalid — ") {
			t.Errorf("%s: not the old validator's line: %v", c.name, err)
		}
		for _, absent := range []string{"result.json", "result.json.ready"} {
			if _, statErr := os.Stat(filepath.Join(dir, absent)); !os.IsNotExist(statErr) {
				t.Errorf("%s: a refused finish wrote %s", c.name, absent)
			}
		}
	}
	// And the receipt the review cases were cut from is itself accepted.
	if _, err := Finish(finishDir(t, plainTask("review"), resultWith(closedReview))); err != nil {
		t.Fatalf("a closed review receipt: %v", err)
	}
}

// What a child says it did not do passes the preflight when it is readable,
// is absent without comment when there is none, and is refused by name when it
// is there and wrong — the way `verification` is. The table's first row is the
// control: a delivery that names no leftovers is a complete delivery, and
// always was.
func TestLeftoversArePublishedWhenReadableAndRefusedWhenNot(t *testing.T) {
	long := strings.Repeat("x", 201)
	for _, c := range []struct {
		why, extra, reason string
	}{
		{"no leftovers at all", "", ""},
		{"an empty list", `, "leftovers": []`, ""},
		{"one, with everything", `, "leftovers": [{"title": "the flaky test", "why": "not mine",
			"suggested_acceptance": "it passes twenty times"}]`, ""},
		{"one, with a title only", `, "leftovers": [{"title": "the flaky test"}]`, ""},
		{"not a list", `, "leftovers": {"title": "x"}`, "leftovers must be a list"},
		{"nine of them", `, "leftovers": [{"title":"1"},{"title":"2"},{"title":"3"},{"title":"4"},{"title":"5"},
			{"title":"6"},{"title":"7"},{"title":"8"},{"title":"9"}]`, "leftovers must be a list"},
		{"a key nobody named", `, "leftovers": [{"title": "x", "owner": "me"}]`, "title/why/suggested_acceptance"},
		{"no title", `, "leftovers": [{"why": "no reason"}]`, "needs a title"},
		{"an empty title", `, "leftovers": [{"title": "   "}]`, "needs a title"},
		{"a title past its bound", `, "leftovers": [{"title": "` + long + `"}]`, "needs a title"},
		{"a generic non-outcome title", `, "leftovers": [{"title": "修正 session 列表顯示問題"}]`, "完成後"},
		{"a why that is not a string", `, "leftovers": [{"title": "x", "why": 3}]`, "why and suggested_acceptance"},
		{"two of one title", `, "leftovers": [{"title": "x"}, {"title": " x "}]`, "the same title"},
	} {
		dir := finishDir(t, plainTask("custom"), resultWith(c.extra))
		done, err := Finish(dir)
		var bad InvalidResult
		switch {
		case c.reason == "" && err != nil:
			t.Errorf("%s: %v", c.why, err)
		case c.reason == "" && done.TaskID != finishID:
			t.Errorf("%s: %+v", c.why, done)
		case c.reason != "" && !errors.As(err, &bad):
			t.Errorf("%s: %v", c.why, err)
		case c.reason != "" && !strings.Contains(bad.Reason, c.reason):
			t.Errorf("%s: %q does not say %q", c.why, bad.Reason, c.reason)
		}
		if c.reason == "" {
			continue
		}
		// A refusal publishes nothing and leaves the tmp file to be corrected.
		if _, err := os.Stat(filepath.Join(dir, "result.json")); !os.IsNotExist(err) {
			t.Errorf("%s: a refused result was published", c.why)
		}
		if _, err := os.Stat(filepath.Join(dir, "result.json.tmp")); err != nil {
			t.Errorf("%s: the tmp file was taken away: %v", c.why, err)
		}
	}
}

// The broker reads back what the child wrote, field for field. A protocol the
// two halves spell differently is not one.
func TestLeftoversSurviveTheRoundTrip(t *testing.T) {
	dir := finishDir(t, plainTask("custom"), resultWith(`, "leftovers": [{"title": "the flaky test",
		"why": "not mine to fix", "suggested_acceptance": "it passes twenty times"}]`))
	if _, err := Finish(dir); err != nil {
		t.Fatal(err)
	}
	root := Root{Dir: filepath.Dir(dir)}
	got, _, err := root.ReadResult(finishID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Leftovers) != 1 {
		t.Fatalf("leftovers %+v", got.Leftovers)
	}
	lo := got.Leftovers[0]
	if lo.Title != "the flaky test" || lo.Why != "not mine to fix" || lo.Acceptance != "it passes twenty times" {
		t.Fatalf("%+v", lo)
	}
}
