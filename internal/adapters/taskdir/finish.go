package taskdir

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// The child's half of finishing: `clawdline task finish <task dir>`
// (docs/design-decisions.md D16).
//
// It replaces the validator the briefing used to carry as base64 inside a
// `node -e` line. The rules are that validator's, one for one, with one it
// lacked: the file must decode the way the broker decodes it. The two files it
// leaves behind are the old validator's too — the `.ready` marker binding the
// validated bytes, then `result.json` — so the broker reads a result finished
// this way exactly as it read one finished the old way. What changes is what a
// child needs: the
// binary that dispatched it, which is on this machine by construction, rather
// than a node that is on no Linux or Windows child's PATH by promise.
//
// One thing is deliberately different. The old line ended in `mv`, which
// replaces a result.json already there; this creates it only if it is absent,
// as the broker's own adoption does (AdoptReady). A result the broker may
// already have settled on is not rewritten by a second run.

// InvalidResult is a result.json.tmp the preflight refused, with the reason in
// the validator's words. Nothing was written: the tmp file is left for the
// child to correct.
type InvalidResult struct{ Reason string }

func (e InvalidResult) Error() string { return "task result preflight: invalid — " + e.Reason }

// ErrResultDiffers is a finish that found result.json already there holding
// other bytes. The task has reported once; nothing was replaced.
var ErrResultDiffers = errors.New("result.json already exists and holds a different result")

// Finished is what a finish did.
type Finished struct {
	TaskID string
	// Secret is the result's own task_secret, which the caller needs to ask
	// the broker to collect now. It is never printed.
	Secret string
	// Path is result.json.
	Path string
	// Already is result.json having been in place with exactly these bytes:
	// a second run of the same finish, which changes nothing.
	Already bool
}

// Finish validates dir/result.json.tmp against dir/task.json and publishes it
// as dir/result.json.
//
// The order is the old validator's: validate, write the marker that binds the
// validated bytes, then publish. A process that dies after the marker leaves a
// result the broker adopts on its own (ReadReady, AdoptReady); one that dies
// before it leaves nothing but the tmp file, which is not a completion.
func Finish(dir string) (Finished, error) {
	taskBody, err := readBounded(filepath.Join(dir, "task.json"), resultLimit)
	if err != nil {
		return Finished{}, InvalidResult{Reason: unreadable("task.json", err)}
	}
	resultBody, err := readBounded(filepath.Join(dir, "result.json.tmp"), resultLimit)
	if err != nil {
		return Finished{}, InvalidResult{Reason: unreadable("result.json.tmp", err)}
	}
	task, err := jsonObject(taskBody, "task.json")
	if err != nil {
		return Finished{}, err
	}
	result, err := jsonObject(resultBody, "result.json.tmp")
	if err != nil {
		return Finished{}, err
	}
	if reason := ValidateResult(task, result); reason != "" {
		return Finished{}, InvalidResult{Reason: reason}
	}
	// And it must be what the broker reads, read by the broker's own decoder
	// (ReadResult). The old validator's rules are JavaScript's: `2.0` is an
	// integer there and `"seconds": 2.0` passed, and the broker — which reads
	// seconds as an int — then called the published file unreadable and never
	// settled the task. A check that reads something other than what runs is
	// not a check of it (design-guidelines DG-6).
	var decoded Result
	if err := json.Unmarshal(resultBody, &decoded); err != nil {
		return Finished{}, InvalidResult{Reason: "result.json.tmp is not what the broker reads: " + err.Error()}
	}
	id, _ := task["task_id"].(string)
	secret, _ := result["task_secret"].(string)
	out := Finished{TaskID: id, Secret: secret, Path: filepath.Join(dir, "result.json")}

	if err := writeReady(dir, id, resultBody); err != nil {
		return Finished{}, fmt.Errorf("could not write result.json.ready: %w", err)
	}
	// The validated bytes, not the tmp file: what is published is what was
	// checked, whatever happens to the tmp file after it was read.
	staged := filepath.Join(dir, fmt.Sprintf("result.json.finishing-%d", os.Getpid()))
	if err := writeExclusive(staged, resultBody); err != nil {
		return Finished{}, fmt.Errorf("could not stage result.json: %w", err)
	}
	defer os.Remove(staged)
	if err := os.Link(staged, out.Path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return Finished{}, fmt.Errorf("could not publish result.json: %w", err)
		}
		there, readErr := os.ReadFile(out.Path)
		// The marker binds bytes that will now never be published, and a
		// marker is only ever a promise of a publication.
		_ = os.Remove(filepath.Join(dir, "result.json.ready"))
		if readErr != nil || !bytes.Equal(there, resultBody) {
			return Finished{}, fmt.Errorf("%w; this task has already reported, and result.json.tmp is left "+
				"where it is", ErrResultDiffers)
		}
		out.Already = true
	}
	_ = os.Remove(filepath.Join(dir, "result.json.tmp"))
	_ = os.Remove(filepath.Join(dir, "result.json.ready"))
	return out, nil
}

// writeReady is the marker, written whole under a name of its own and renamed
// into place, as the old validator wrote it.
func writeReady(dir, id string, body []byte) error {
	sum := sha256.Sum256(body)
	marker, err := json.Marshal(ready{Protocol: 1, TaskID: id, Ready: true, SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "result.json.ready")
	tmp := fmt.Sprintf("%s.writing-%d", path, os.Getpid())
	if err := writeExclusive(tmp, append(marker, '\n')); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// writeExclusive creates path, 0600, refusing one that exists.
func writeExclusive(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func unreadable(label string, err error) string {
	if errors.Is(err, ErrTooLarge) {
		return fmt.Sprintf("%s is larger than the broker reads (%d bytes)", label, resultLimit)
	}
	return label + " is not readable JSON"
}

// jsonObject is readJSON: one JSON object, numbers kept as written so that an
// integer is told from a fraction the way the old validator told them.
func jsonObject(body []byte, label string) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	// Anything after the value but whitespace is JSON.parse's refusal too.
	if err := dec.Decode(&value); err != nil || len(bytes.TrimSpace(body[dec.InputOffset():])) != 0 {
		return nil, InvalidResult{Reason: label + " is not readable JSON"}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, InvalidResult{Reason: label + " must contain one JSON object"}
	}
	return obj, nil
}

// ValidateResult is the preflight: the empty string when result may be
// published for task, else the reason it may not, in the old validator's
// words (orchestrator/result-preflight.js until D16 retired it).
func ValidateResult(task, result map[string]any) string {
	if !isOne(task["clawdline_protocol"]) || !isTaskID(task["task_id"]) {
		return "task.json has no valid protocol identity"
	}
	if !isOne(result["clawdline_protocol"]) {
		return "clawdline_protocol must be 1"
	}
	if !isTaskID(result["task_id"]) || result["task_id"] != task["task_id"] {
		return "task_id must be a lowercase UUID matching task.json"
	}
	if s, ok := result["task_secret"].(string); !ok || !taskSecretPattern.MatchString(s) {
		return "task_secret must be 64 lowercase hexadecimal characters"
	}
	if s := result["status"]; s != "success" && s != "failure" {
		return "status must be success or failure"
	}
	if row, present := result["verification"]; present {
		v, ok := row.(map[string]any)
		if !ok || !nonNegativeInteger(v["runs"]) || !nonNegativeInteger(v["seconds"]) ||
			!oneOf(v["last"], "pass", "fail", "skipped") || !nonEmpty(v["scope"], 300) {
			return "verification must contain non-negative integer runs/seconds, a valid last value, and a non-empty scope"
		}
	}
	review, hasReview := result["review"]
	if requiresReview(task) && result["status"] == "success" && !hasReview {
		return "a successful review task requires a closed review receipt"
	}
	if hasReview {
		return validateReview(review)
	}
	return ""
}

var (
	taskIDPattern     = regexp.MustCompile(`^[a-f0-9-]+$`)
	taskSecretPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	slugPattern       = regexp.MustCompile(`^[a-z0-9._-]+$`)
)

// requiresReview is a review node of a graph, or — with no graph — a task
// whose kind has the word "review" in it.
func requiresReview(task map[string]any) bool {
	if graph, ok := task["graph"].(map[string]any); ok {
		if nodes, ok := graph["nodes"].([]any); ok {
			current, hasCurrent := graph["current_node"]
			for _, n := range nodes {
				node, ok := n.(map[string]any)
				if !ok {
					continue
				}
				id, hasID := node["id"]
				// `node.id === graph.current_node`, where two absent values
				// are equal too.
				if hasID == hasCurrent && (!hasID || reflect.DeepEqual(id, current)) {
					return node["kind"] == "review"
				}
			}
		}
	}
	kind, _ := task["kind"].(string)
	for _, word := range strings.FieldsFunc(strings.ToLower(kind), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if word == "review" {
			return true
		}
	}
	return false
}

// validateReview is the closed review receipt: a verdict, and exactly the three
// axes, each passing with no findings or carrying at least one.
func validateReview(value any) string {
	review, ok := value.(map[string]any)
	if !ok || !exactKeys(review, "verdict", "axes") || !oneOf(review["verdict"], "safe_to_land", "changes_required") {
		return "review must contain only a valid verdict and exactly three axes"
	}
	axes, ok := review["axes"].([]any)
	if !ok || len(axes) != 3 {
		return "review must contain only a valid verdict and exactly three axes"
	}
	wanted := map[string]bool{"specification": true, "repository_invariants": true, "runtime_failure_behavior": true}
	seen := map[string]bool{}
	findings := 0
	for _, a := range axes {
		axis, ok := a.(map[string]any)
		name, _ := axis["axis"].(string)
		list, listOK := axis["findings"].([]any)
		if !ok || !exactKeys(axis, "axis", "status", "findings") || !wanted[name] || seen[name] ||
			!oneOf(axis["status"], "pass", "findings") || !listOK || len(list) > 32 {
			return "review axes must be unique, closed, named axes with valid status and findings"
		}
		seen[name] = true
		ids := map[string]bool{}
		for _, f := range list {
			finding, ok := f.(map[string]any)
			id, _ := finding["id"].(string)
			evidence, evidenceOK := finding["evidence"].([]any)
			if !ok || !exactKeys(finding, "id", "severity", "summary", "evidence") || !isSlug(finding["id"]) || ids[id] ||
				!oneOf(finding["severity"], "blocking", "important", "minor") || !nonEmpty(finding["summary"], 500) ||
				!evidenceOK || len(evidence) < 1 || len(evidence) > 8 || !allNonEmpty(evidence, 500) {
				return "each review finding must use the exact id/severity/summary/evidence schema"
			}
			ids[id] = true
			findings++
		}
		if (axis["status"] == "pass") != (len(list) == 0) {
			return "a passing axis has no findings and a findings axis has at least one"
		}
	}
	if len(seen) != len(wanted) {
		return "review must contain each required axis once"
	}
	if (review["verdict"] == "safe_to_land") != (findings == 0) {
		return "review verdict must agree with its findings"
	}
	return ""
}

// isOne is `=== 1`: the number one, however it was written.
func isOne(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	f, err := strconv.ParseFloat(string(n), 64)
	return err == nil && f == 1
}

func isTaskID(v any) bool {
	s, ok := v.(string)
	return ok && len(s) == 36 && taskIDPattern.MatchString(s)
}

// nonNegativeInteger is Number.isInteger(v) && v >= 0.
func nonNegativeInteger(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	f, err := strconv.ParseFloat(string(n), 64)
	return err == nil && !math.IsInf(f, 0) && f == math.Trunc(f) && f >= 0
}

func oneOf(v any, choices ...string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, c := range choices {
		if s == c {
			return true
		}
	}
	return false
}

// jsLength is a string's length as JavaScript counts it, in UTF-16 units: the
// limits below are the old validator's, and they were counted that way.
func jsLength(s string) int { return len(utf16.Encode([]rune(s))) }

func nonEmpty(v any, max int) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != "" && jsLength(s) <= max
}

func allNonEmpty(list []any, max int) bool {
	for _, item := range list {
		if !nonEmpty(item, max) {
			return false
		}
	}
	return true
}

func isSlug(v any) bool {
	s, ok := v.(string)
	return ok && jsLength(s) > 0 && jsLength(s) <= 64 && !strings.HasPrefix(s, "-") &&
		slugPattern.MatchString(strings.ToLower(s))
}

func exactKeys(obj map[string]any, keys ...string) bool {
	if len(obj) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	return true
}
