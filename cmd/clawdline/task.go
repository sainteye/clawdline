package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline task finish <task dir>`: a child's result, validated and put in
// place, with nothing but this binary (docs/design-decisions.md D16).
//
// The briefing used to end in a `node -e` line carrying the validator as
// base64. node is on this machine's PATH and on no promise anywhere else, and a
// child that could not run it could not report — so a Linux or Windows child
// without it did its work and then had no way to say so. The binary that
// dispatched the child is on the same machine by construction, and the
// briefing names it by absolute path.
//
// What it does is the old line's: validate result.json.tmp against task.json,
// write the marker binding those bytes, publish them as result.json. Then it
// asks the broker to collect now rather than at its next look, and that part
// is a courtesy: result.json is the completion signal, so a broker that cannot
// be reached loses nothing.
//
// Exit status: 0 when result.json is in place and nothing said it was
// refused; 1 when the result was not published, or was published and the
// broker said it is not this task's; 2 for a usage mistake.

// collectAnswerLimit is how much of the broker's answer to "collect it now"
// is read: one refusal envelope, which is a few hundred bytes.
const collectAnswerLimit = 64 << 10

func taskCommand(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "ack":
			ackCommand(args[1:])
			return
		case "accept":
			acceptCommand(args[1:])
			return
		case "show":
			showCommand(args[1:])
			return
		case "wait":
			waitCommand(args[1:])
			return
		case "land":
			landCommand(args[1:])
			return
		case "gate-evidence":
			gateEvidenceCommand(args[1:])
			return
		case "gate-result":
			gateResultCommand(args[1:])
			return
		}
	}
	if len(args) == 0 || args[0] != "finish" {
		taskUsage()
	}
	fs := flag.NewFlagSet("task finish", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the port of the daemon that dispatched this task (default CLAWDLINE_NEXT_PORT, else 7727)")
	noCollect := fs.Bool("no-collect", false, "put result.json in place and do not ask the broker to collect it now")
	// Flags may come before or after the directory.
	if err := fs.Parse(args[1:]); err != nil {
		taskUsage()
	}
	rest := fs.Args()
	if len(rest) == 0 {
		taskUsage()
	}
	dir := rest[0]
	if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
		taskUsage()
	}
	if *port == 0 {
		p, err := daemonPort()
		if err != nil {
			fmt.Fprintln(os.Stderr, "clawdline:", err)
			os.Exit(2)
		}
		*port = p
	}
	os.Exit(finishTask(os.Stdout, os.Stderr, dir, *port, !*noCollect, http.DefaultClient))
}

func taskUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline task accept [--port n] <task dir>")
	fmt.Fprintln(os.Stderr, "  signs for a child's briefing, with the secret from "+orchestrator.AcceptSecretEnv+" or stdin")
	fmt.Fprintln(os.Stderr, "       clawdline task finish [--port n] [--no-collect] <task dir>")
	fmt.Fprintln(os.Stderr, "  validates <task dir>/result.json.tmp and publishes it as result.json, then asks the broker to collect it")
	fmt.Fprintln(os.Stderr, "       clawdline task show [--port n] [--json] <task id>")
	fmt.Fprintln(os.Stderr, "  one child task, compactly: state, summary, leftovers, verification, landing, checkout;")
	fmt.Fprintln(os.Stderr, "  reading a finished one closes its completion notice")
	fmt.Fprintln(os.Stderr, "       clawdline task wait [--port n] [--timeout 9m] [--any] <task id>...")
	fmt.Fprintln(os.Stderr, "  waits until every task (--any: one) has finished, at most --timeout (default 9m, under a 10m tool call;")
	fmt.Fprintln(os.Stderr, "  at most 2h), then shows each finished one as task show does and closes its notice;")
	fmt.Fprintln(os.Stderr, "  exit 0 all succeeded, 1 one did not, 3 timed out, 4 a task could not be read")
	fmt.Fprintln(os.Stderr, "       clawdline task ack [--port n] <task id> <notice id>")
	fmt.Fprintln(os.Stderr, "  acknowledges a completion notice by hand; rarely needed, since task show and task wait close it")
	fmt.Fprintln(os.Stderr, "       clawdline task land [--port n] <task id> <landed|incorporated|abandoned|nothing_to_land|pending>")
	fmt.Fprintln(os.Stderr, "                           [--target b] [--commit c] [--carrier-task id] [--note t]")
	fmt.Fprintln(os.Stderr, "  records a child's landing by hand, with the orchestrator token; a merge into its target records itself")
	fmt.Fprintln(os.Stderr, "       clawdline task gate-evidence --artifact id --media-type type [--port n] <task dir> <file>")
	fmt.Fprintln(os.Stderr, "  streams one bounded verification artifact with the secret from "+orchestrator.AcceptSecretEnv)
	fmt.Fprintln(os.Stderr, "       clawdline task gate-result [--port n] <task dir> <json file>")
	fmt.Fprintln(os.Stderr, "  submits the closed verification verdict with the secret from "+orchestrator.AcceptSecretEnv)
	os.Exit(2)
}

func gateEvidenceCommand(args []string) {
	fs := flag.NewFlagSet("task gate-evidence", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "")
	artifact := fs.String("artifact", "", "")
	mediaType := fs.String("media-type", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 2 || strings.TrimSpace(*artifact) == "" || strings.TrimSpace(*mediaType) == "" {
		taskUsage()
	}
	secret, err := acceptSecret(os.Getenv, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline task gate-evidence:", err)
		os.Exit(2)
	}
	if *port == 0 {
		if *port, err = daemonPort(); err != nil {
			fail(err)
		}
	}
	os.Exit(submitGateEvidence(os.Stdout, os.Stderr, fs.Arg(0), fs.Arg(1), *artifact, *mediaType, secret, *port, http.DefaultClient))
}

func submitGateEvidence(stdout, stderr io.Writer, dir, path, artifact, mediaType, secret string, port int, client *http.Client) int {
	id, err := taskIDIn(dir)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-evidence:", err)
		return 1
	}
	source, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-evidence:", err)
		return 1
	}
	defer source.Close()
	digest := sha256.New()
	byteCount, err := io.Copy(digest, io.LimitReader(source, contract.WorkGateEvidenceArtifactBytesLimit+1))
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-evidence:", err)
		return 1
	}
	if byteCount > contract.WorkGateEvidenceArtifactBytesLimit {
		fmt.Fprintf(stderr, "clawdline task gate-evidence: artifact is more than %d bytes\n", contract.WorkGateEvidenceArtifactBytesLimit)
		return 1
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-evidence: artifact cannot be streamed:", err)
		return 1
	}
	hexDigest := hex.EncodeToString(digest.Sum(nil))
	keyDigest := sha256.Sum256([]byte(id + "\x00" + artifact + "\x00" + hexDigest))
	key := "gate-evidence-" + hex.EncodeToString(keyDigest[:16])
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/v1/orchestrator/tasks/" + id + "/gate-evidence"
	req, err := http.NewRequest(http.MethodPost, endpoint, source)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-evidence:", err)
		return 1
	}
	req.Header.Set(orchestrator.HeaderTaskSecret, secret)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("X-Clawdline-Artifact-ID", artifact)
	req.Header.Set("X-Clawdline-Content-SHA256", hexDigest)
	req.Header.Set("X-Clawdline-Byte-Count", strconv.FormatInt(byteCount, 10))
	req.Header.Set("Content-Type", mediaType)
	req.ContentLength = byteCount
	if client == nil {
		client = http.DefaultClient
	}
	short := *client
	short.Timeout = brokerTimeout
	res, err := short.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline task gate-evidence: the broker did not acknowledge durable evidence (%v). Nothing local counts as gate evidence; restore loopback and rerun this same command.\n", err)
		return 1
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, collectAnswerLimit))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		fmt.Fprintf(stderr, "clawdline task gate-evidence: refused, %s: %s\n", res.Status, strings.TrimSpace(string(answer)))
		return 1
	}
	var receipt contract.WorkGateEvidenceReceipt
	if json.Unmarshal(answer, &receipt) != nil || receipt.AcceptedAt <= 0 || receipt.TaskID != id || receipt.ArtifactID != artifact ||
		receipt.ByteCount != byteCount || receipt.MediaType != mediaType || receipt.Sha256 != hexDigest {
		fmt.Fprintln(stderr, "clawdline task gate-evidence: the broker returned no matching durable receipt")
		return 1
	}
	fmt.Fprintf(stdout, "gate evidence %s accepted (%d bytes, sha256 %s, replayed=%t)\n", receipt.ArtifactID, receipt.ByteCount, receipt.Sha256, receipt.Replayed)
	return 0
}

func gateResultCommand(args []string) {
	fs := flag.NewFlagSet("task gate-result", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 2 {
		taskUsage()
	}
	secret, err := acceptSecret(os.Getenv, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline task gate-result:", err)
		os.Exit(2)
	}
	if *port == 0 {
		if *port, err = daemonPort(); err != nil {
			fail(err)
		}
	}
	os.Exit(submitGateResult(os.Stdout, os.Stderr, fs.Arg(0), fs.Arg(1), secret, *port, http.DefaultClient))
}

func submitGateResult(stdout, stderr io.Writer, dir, path, secret string, port int, client *http.Client) int {
	id, err := taskIDIn(dir)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-result:", err)
		return 1
	}
	body, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-result:", err)
		return 1
	}
	result, err := contract.DecodeWorkGateResult(body)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-result: invalid closed gate result:", err)
		return 1
	}
	if result.TaskID != id {
		fmt.Fprintln(stderr, "clawdline task gate-result: result task_id does not match the task directory")
		return 1
	}
	digest := sha256.Sum256(body)
	hexDigest := hex.EncodeToString(digest[:])
	key := "gate-result-" + hexDigest[:32]
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/v1/orchestrator/tasks/" + id + "/gate-result"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task gate-result:", err)
		return 1
	}
	req.Header.Set(orchestrator.HeaderTaskSecret, secret)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	short := *client
	short.Timeout = brokerTimeout
	res, err := short.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline task gate-result: the broker did not acknowledge a durable verdict (%v). A result file in the checker task directory cannot finish this gate; restore loopback and rerun this same command.\n", err)
		return 1
	}
	defer res.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, collectAnswerLimit))
	if res.StatusCode != http.StatusAccepted {
		fmt.Fprintf(stderr, "clawdline task gate-result: refused, %s: %s\n", res.Status, strings.TrimSpace(string(answer)))
		return 1
	}
	var receipt contract.WorkGateResultReceipt
	if json.Unmarshal(answer, &receipt) != nil || receipt.AcceptedAt <= 0 || receipt.TaskID != id || receipt.ByteCount != int64(len(body)) ||
		receipt.Sha256 != hexDigest || receipt.Verdict != result.Verdict {
		fmt.Fprintln(stderr, "clawdline task gate-result: the broker returned no matching durable receipt")
		return 1
	}
	fmt.Fprintf(stdout, "gate verdict %s accepted (sha256 %s, replayed=%t)\n", receipt.Verdict, receipt.Sha256, receipt.Replayed)
	return 0
}

// `clawdline task ack <task id> <notice id>`: the root's side of a finished
// child. The daemon keeps typing a completion notice into the root's composer
// until it is acknowledged (guide §5); this is that acknowledgement, with the
// orchestrator token, as one line.
func ackCommand(args []string) {
	fs := flag.NewFlagSet("task ack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil {
		taskUsage()
	}
	rest := fs.Args()
	if len(rest) != 2 {
		taskUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(ackTask(os.Stdout, os.Stderr, b, rest[0], rest[1]))
}

// ackTask is the command, answering its exit status.
func ackTask(stdout, stderr io.Writer, b *broker, id, notice string) int {
	id, notice = strings.TrimSpace(id), strings.TrimSpace(notice)
	if id == "" || notice == "" {
		fmt.Fprintln(stderr, "clawdline task ack: both the task id and the notice id are required")
		return 2
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/tasks/"+url.PathEscape(id)+"/completion/ack", nil,
		map[string]string{"notice_id": notice}, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task ack:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "task ack", a)
	}
	var got contract.BrokerAckResult
	_ = json.Unmarshal(a.Body, &got)
	if got.Changed {
		fmt.Fprintf(stdout, "acknowledged %s notice %s\n", id, got.NoticeID)
	} else {
		fmt.Fprintf(stdout, "acknowledged %s notice %s (already acknowledged)\n", id, got.NoticeID)
	}
	return 0
}

// `clawdline task land <task id> <state>`: a landing a root records by hand.
//
// A merge records itself (landing_detect.go), and a child that declared no
// writes and wrote nothing is recorded nothing_to_land by the broker. What is
// left — a cherry-pick, an `incorporated` delivery, `abandoned`, a shared
// checkout's landing — used to be a raw POST whose credential the guide did
// not name, and a root that sent it with the wrong one was refused
// `forbidden` (2026-10-02). This sends it with the orchestrator token, which
// every landing state accepts, and the completion notice names this line.
type landInvocation struct {
	port int
	id   string
	body map[string]string
}

// landArgs reads the command line. Flags may come before, between or after
// the two words, because the notice puts them after.
func landArgs(args []string) (landInvocation, error) {
	fs := flag.NewFlagSet("task land", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	target := fs.String("target", "", "the branch the delivery landed on")
	commit := fs.String("commit", "", "the commit on the target that carries the delivery")
	carrier := fs.String("carrier-task", "", "for incorporated: the task whose verified landing carried this one")
	note := fs.String("note", "", "what the record should say")
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return landInvocation{}, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		words = append(words, args[0])
		args = args[1:]
	}
	if len(words) != 2 {
		return landInvocation{}, errors.New("a task id and a landing state are required")
	}
	id, state := strings.TrimSpace(words[0]), strings.TrimSpace(words[1])
	if !orchestrator.IsTaskID(id) {
		return landInvocation{}, fmt.Errorf("%q is not a task id", id)
	}
	switch orchestrator.LandingState(state) {
	case orchestrator.LandingPending, orchestrator.LandingLanded, orchestrator.LandingIncorporated,
		orchestrator.LandingAbandoned, orchestrator.LandingNothingToLand:
	default:
		return landInvocation{}, fmt.Errorf("%q is not a landing state: landed, incorporated, abandoned, "+
			"nothing_to_land or pending", state)
	}
	body := map[string]string{"state": state}
	for key, v := range map[string]string{"target": *target, "commit": *commit, "carrier_task": *carrier, "note": *note} {
		if v = strings.TrimSpace(v); v != "" {
			body[key] = v
		}
	}
	return landInvocation{port: *port, id: id, body: body}, nil
}

func landCommand(args []string) {
	inv, err := landArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline task land:", err)
		taskUsage()
	}
	b, err := openBroker(inv.port)
	if err != nil {
		fail(err)
	}
	os.Exit(landTask(os.Stdout, os.Stderr, b, inv))
}

// landTask is the command, answering its exit status. The broker's refusal —
// `unverified_landing` with its reason, `wrote_to_repository`,
// `invalid_transition` — is printed as it came, code first.
func landTask(stdout, stderr io.Writer, b *broker, inv landInvocation) int {
	a, err := b.request(http.MethodPost, "/v1/orchestrator/tasks/"+url.PathEscape(inv.id)+"/landing", nil, inv.body, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task land:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "task land", a)
	}
	var got contract.BrokerProgressResult
	if json.Unmarshal(a.Body, &got) != nil || got.Task.Landing == nil {
		fmt.Fprintln(stderr, "clawdline task land: the daemon answered without the landing it recorded")
		return 1
	}
	l := got.Task.Landing
	line := "recorded " + inv.id + " landing " + string(l.State)
	if l.Target != "" {
		line += " on " + l.Target
	}
	if l.Commit != "" {
		line += " at " + l.Commit
	}
	fmt.Fprintln(stdout, line)
	return 0
}

// finishTask is the command, answering its exit status.
func finishTask(stdout, stderr io.Writer, dir string, port int, collect bool, client *http.Client) int {
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	done, err := taskdir.Finish(dir)
	var invalid taskdir.InvalidResult
	switch {
	case errors.As(err, &invalid):
		// The old validator's line, word for word: a child that learned to
		// read it keeps reading it. Every further violation follows on its
		// own line, so one correction pass can fix them all.
		lines := strings.Split(invalid.Error(), "\n")
		fmt.Fprintln(stderr, lines[0])
		for _, more := range lines[1:] {
			fmt.Fprintln(stderr, "  - "+more)
		}
		fmt.Fprintln(stderr, "Nothing was written. Correct result.json.tmp and run this again.")
		return 1
	case err != nil:
		fmt.Fprintln(stderr, "clawdline task finish:", err)
		return 1
	}
	fmt.Fprintln(stdout, "task result preflight: valid")
	if done.Already {
		fmt.Fprintf(stdout, "result.json was already in place with these exact bytes: %s\n", done.Path)
	} else {
		fmt.Fprintf(stdout, "result.json is in place: %s\n", done.Path)
	}
	if !collect {
		fmt.Fprintln(stdout, "Not asking the broker to collect it now; it reads result.json at its next look.")
		return 0
	}
	return askCollect(stdout, stderr, done, port, client)
}

// askCollect is POST …/tasks/<id>/complete: "collect it now", carrying
// nothing but the task's own secret (D15).
func askCollect(stdout, stderr io.Writer, done taskdir.Finished, port int, client *http.Client) int {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/v1/orchestrator/tasks/" + done.TaskID + "/complete"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task finish:", err)
		return 1
	}
	req.Header.Set(orchestrator.HeaderTaskSecret, done.Secret)
	if client == nil {
		client = http.DefaultClient
	}
	short := *client
	short.Timeout = 10 * time.Second
	res, err := short.Do(req)
	if err != nil {
		// No loopback in this sandbox, or no daemon on that port: the file is
		// the report, and it is in place.
		fmt.Fprintf(stdout, "The broker at 127.0.0.1:%d could not be asked to collect it now (%v). Nothing is lost: "+
			"result.json is the completion signal, and the broker reads it at its next look.\n", port, err)
		return 0
	}
	defer res.Body.Close()
	var refusal contract.AuthRefusal
	body, _ := io.ReadAll(io.LimitReader(res.Body, collectAnswerLimit))
	code, message := "", ""
	if json.Unmarshal(body, &refusal) == nil {
		code, message = refusal.Error.Code, refusal.Error.Message
	}
	switch {
	case res.StatusCode == http.StatusOK:
		fmt.Fprintln(stdout, "The broker collected it: this task is settled on result.json.")
		return 0
	case code == "already_done":
		fmt.Fprintln(stdout, "The broker had already collected it.")
		return 0
	case code == "result_rejected" || code == "result_unreadable" || code == "forbidden":
		// Published, and refused: the broker will never settle on this file,
		// and finish does not replace a result.json. Say how to get out.
		fmt.Fprintf(stderr, "The broker refused result.json (%s): %s\n", code, message)
		fmt.Fprintf(stderr, "It stays where it is. To replace it, delete %s, write result.json.tmp again and run this again.\n",
			done.Path)
		return 1
	}
	said := res.Status
	if code != "" {
		said = code + ": " + strings.TrimSpace(message)
	}
	fmt.Fprintf(stdout, "The broker did not collect it now (%s). result.json is in place, and the broker reads it "+
		"at its next look.\n", said)
	return 0
}

// `clawdline task accept <task dir>`: a child signing for its briefing.
//
// The briefing used to carry the curl line for …/accepted, a paragraph on
// why `--fail-with-body`, and a second recipe for accepted.json when there is
// no loopback — 0.6% of every child's cost, measured on 2026-09-26, to say
// one thing. This is that thing as one line, and it does what `task finish`
// does when the broker cannot be reached: leaves the file the broker collects.
//
// The secret is read from CLAWDLINE_TASK_SECRET, else from stdin, and never
// from argv, where `ps` shows it to anybody on the machine. It is never
// printed.
//
// Exit status: 0 when the broker has the receipt or the receipt file is in
// place; 1 when the broker refused it, or nothing could be written; 2 for a
// usage mistake.
func acceptCommand(args []string) {
	dir, port, err := acceptArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline task accept:", err)
		os.Exit(2)
	}
	var stdin io.Reader
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
		stdin = os.Stdin
	}
	secret, err := acceptSecret(os.Getenv, stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline task accept:", err)
		os.Exit(2)
	}
	if port == 0 {
		if port, err = daemonPort(); err != nil {
			fmt.Fprintln(os.Stderr, "clawdline:", err)
			os.Exit(2)
		}
	}
	os.Exit(acceptTask(os.Stdout, os.Stderr, dir, port, secret, http.DefaultClient))
}

// acceptArgs is the task directory and the port. Anything else on the
// command line is refused, because the one thing a child might put there is
// its secret.
func acceptArgs(args []string) (string, int, error) {
	fs := flag.NewFlagSet("task accept", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "")
	if err := fs.Parse(args); err != nil {
		return "", 0, err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return "", 0, errors.New("the task directory is required")
	}
	dir := rest[0]
	if err := fs.Parse(rest[1:]); err != nil {
		return "", 0, err
	}
	if fs.NArg() != 0 {
		return "", 0, errors.New("it takes one task directory and nothing else; the secret is never taken on the " +
			"command line — set " + orchestrator.AcceptSecretEnv + " or pipe it on stdin")
	}
	return dir, *port, nil
}

// acceptSecret is the task secret, from the environment or else stdin.
func acceptSecret(getenv func(string) string, stdin io.Reader) (string, error) {
	secret := strings.TrimSpace(getenv(orchestrator.AcceptSecretEnv))
	if secret == "" && stdin != nil {
		data, err := io.ReadAll(io.LimitReader(stdin, brokerTokenLimit))
		if err != nil {
			return "", err
		}
		secret = strings.TrimSpace(string(data))
	}
	if secret == "" {
		return "", errors.New("no task secret: set " + orchestrator.AcceptSecretEnv + " or pipe it on stdin")
	}
	if !isTaskSecret(secret) {
		return "", errors.New("the task secret must be 64 lowercase hexadecimal characters — the TASK_SECRET " +
			"value from your first message")
	}
	return secret, nil
}

func isTaskSecret(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// acceptTask is the command, answering its exit status.
func acceptTask(stdout, stderr io.Writer, dir string, port int, secret string, client *http.Client) int {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	id, err := taskIDIn(dir)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task accept:", err)
		return 1
	}
	req, err := http.NewRequest(http.MethodPost,
		"http://127.0.0.1:"+strconv.Itoa(port)+"/v1/orchestrator/tasks/"+id+"/accepted", nil)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task accept:", err)
		return 1
	}
	req.Header.Set(orchestrator.HeaderTaskSecret, secret)
	if client == nil {
		client = http.DefaultClient
	}
	short := *client
	short.Timeout = 10 * time.Second
	res, err := short.Do(req)
	why := ""
	if err != nil {
		why = err.Error()
	} else {
		defer res.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(res.Body, collectAnswerLimit))
		switch {
		case res.StatusCode >= 200 && res.StatusCode < 300:
			fmt.Fprintf(stdout, "Signed: the broker has the receipt for task %s.\n", id)
			return 0
		case res.StatusCode < 500:
			code, message := res.Status, ""
			var refusal contract.AuthRefusal
			if json.Unmarshal(body, &refusal) == nil && refusal.Error.Code != "" {
				code, message = refusal.Error.Code, refusal.Error.Message
			}
			fmt.Fprintf(stderr, "clawdline task accept: refused, %d %s: %s\n", res.StatusCode, code, message)
			return 1
		}
		why = res.Status
	}
	// No loopback in this sandbox, or a broker that could not answer: the
	// file is the receipt, and the broker collects it at its next look.
	path := filepath.Join(dir, "accepted.json")
	body, _ := json.Marshal(struct {
		Secret string `json:"task_secret"`
	}{secret})
	if err := writePrivate(path, append(body, '\n')); err != nil {
		fmt.Fprintf(stderr, "clawdline task accept: the broker at 127.0.0.1:%d could not be reached (%s), "+
			"and the receipt could not be written: %v\n", port, why, err)
		return 1
	}
	fmt.Fprintf(stdout, "The broker at 127.0.0.1:%d could not be reached (%s). Signed all the same: the receipt "+
		"is at %s, and the broker collects it at its next look.\n", port, why, path)
	return 0
}

// taskIDIn is the task id task.json names.
func taskIDIn(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		return "", fmt.Errorf("no readable task.json in %s: %w", dir, err)
	}
	var brief struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(data, &brief); err != nil || brief.TaskID == "" || strings.ContainsAny(brief.TaskID, "/\\ ") {
		return "", fmt.Errorf("%s names no usable task_id", filepath.Join(dir, "task.json"))
	}
	return brief.TaskID, nil
}

// writePrivate writes a file only this user can read, whole or not at all.
func writePrivate(path string, body []byte) error {
	tmp := path + ".writing"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// `clawdline task show <task id>`: the view of one child a root needs to
// integrate it.
//
// A root used to read the child's whole result.json (2.1% of a root's cost,
// measured on 2026-09-26) and its task.json (1.5%) — symbols, artifacts,
// every leftover's reasons — to learn a state, a summary and what is left.
// This prints those, and counts the rest; `--json` is the daemon's whole
// answer for the rare root that needs a symbol by name.
func showCommand(args []string) {
	fs := flag.NewFlagSet("task show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	asJSON := fs.Bool("json", false, "print the daemon's whole answer")
	if err := fs.Parse(args); err != nil {
		taskUsage()
	}
	rest := fs.Args()
	if len(rest) == 0 {
		taskUsage()
	}
	id := rest[0]
	if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
		taskUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(showTask(os.Stdout, os.Stderr, b, id, *asJSON))
}

// showTask is the command, answering its exit status.
func showTask(stdout, stderr io.Writer, b *broker, id string, asJSON bool) int {
	id = strings.TrimSpace(id)
	if id == "" {
		fmt.Fprintln(stderr, "clawdline task show: the task id is required")
		return 2
	}
	a, err := b.request(http.MethodGet, "/v1/orchestrator/tasks/"+url.PathEscape(id), nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task show:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "task show", a)
	}
	var got contract.BrokerTaskEnvelope
	err = json.Unmarshal(a.Body, &got)
	if asJSON {
		code := report(stdout, stderr, "task show", a)
		if err == nil {
			closeNotice(stdout, stderr, b, "task show", got.Task, false)
		}
		return code
	}
	if err != nil {
		fmt.Fprintln(stderr, "clawdline task show: the daemon's answer could not be read:", err)
		return 1
	}
	writeTaskView(stdout, got.Task)
	closeNotice(stdout, stderr, b, "task show", got.Task, true)
	return 0
}

// closeNotice acknowledges a finished task's completion notice once its result
// has been printed, through the same route `task ack` uses.
//
// A root runs `task show` and `task wait` on purpose, to read what the child
// delivered, and that is the reading the notice exists to get: until
// 2026-10-02 the line also asked for a separate `task ack`, which roots put off
// until they had integrated the child, and the daemon kept typing the notice
// at them meanwhile. The daemon's own GET still closes nothing — the console
// polls it (notice.go, NoticeSeen); it is this command, after it has printed,
// that sends the ACK.
//
// A task that has not finished has no notice to close, and one already
// acknowledged is left alone. A refused ACK is one line on stderr and does not
// change the exit status: the read succeeded, and the notice stays open for
// the daemon to type again, which is what it did before. line adds one line to
// the compact view saying the notice was closed.
func closeNotice(stdout, stderr io.Writer, b *broker, name string, t contract.BrokerTask, line bool) bool {
	n := t.CompletionDelivery
	if !orchestrator.State(t.State).Terminal() || n == nil || n.NoticeID == "" ||
		n.State == contract.BrokerNoticeStateAcknowledged {
		return false
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/tasks/"+url.PathEscape(t.ID)+"/completion/ack", nil,
		map[string]string{"notice_id": n.NoticeID}, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: the completion notice %s of %s was not closed: %v\n", name, n.NoticeID, t.ID, err)
		return false
	}
	if !a.ok() {
		code, message := a.refusal()
		if code == "" {
			code, message = strconv.Itoa(a.Status), strings.TrimSpace(string(a.Body))
		}
		fmt.Fprintf(stderr, "clawdline %s: the completion notice %s of %s was not closed: refused, %s: %s\n",
			name, n.NoticeID, t.ID, code, message)
		return false
	}
	if line {
		fmt.Fprintln(stdout, "notice:       closed (read)")
	}
	return true
}

// writeTaskView is the compact view. Each line says what it does not know
// rather than leaving the line out: a missing verification and a verification
// that was skipped are different things to a root deciding what to trust.
func writeTaskView(w io.Writer, t contract.BrokerTask) {
	fmt.Fprintf(w, "%s  %s\n", t.ID, t.Title)
	state := string(t.State)
	if t.Verdict != "" {
		state += " — " + t.Verdict
	}
	fmt.Fprintf(w, "state:        %s\n", state)

	r := t.Result
	switch {
	case r == nil:
		fmt.Fprintln(w, "result:       none — the child wrote no result.json")
	default:
		if r.Verification != nil {
			fmt.Fprintf(w, "verification: %d runs, last %s: %s\n", r.Verification.Runs, r.Verification.Last,
				r.Verification.Scope)
		} else {
			fmt.Fprintln(w, "verification: none reported")
		}
		fmt.Fprintf(w, "symbols:      %d symbols, %d artifacts (--json lists them)\n", len(r.Symbols), len(r.Artifacts))
	}

	switch l := t.Landing; {
	case l == nil:
		fmt.Fprintln(w, "landing:      none owed")
	case l.Settlement != "":
		fmt.Fprintf(w, "landing:      %s (%s)\n", l.State, l.Settlement)
		// The completion notice says a merge records this by itself and
		// leaves the other way to here.
		if l.State == contract.BrokerLandingStatePending && l.Settlement == contract.BrokerLandingSettlementBranchCarriesCommits {
			fmt.Fprintf(w, "              %s\n", orchestrator.CarriedByHand(t.ID))
		}
	default:
		fmt.Fprintf(w, "landing:      %s\n", l.State)
	}
	if wt := t.Worktree; wt != nil {
		fmt.Fprintf(w, "worktree:     %s (branch %s)\n", wt.Path, wt.Branch)
	}

	if r != nil && len(r.Leftovers) > 0 {
		fmt.Fprintf(w, "leftovers:    %d (their reasons: --json)\n", len(r.Leftovers))
		for _, l := range r.Leftovers {
			fmt.Fprintf(w, "  - %s\n", l.Title)
		}
	}

	summary := t.Summary
	if r != nil && r.Summary != "" {
		summary = r.Summary
	}
	if summary != "" {
		fmt.Fprintf(w, "summary:\n%s\n", strings.TrimRight(summary, "\n"))
	}
}
