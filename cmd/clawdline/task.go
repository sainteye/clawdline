package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/contract"
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
	fmt.Fprintln(os.Stderr, "usage: clawdline task finish [--port n] [--no-collect] <task dir>")
	fmt.Fprintln(os.Stderr, "  validates <task dir>/result.json.tmp and publishes it as result.json, then asks the broker to collect it")
	os.Exit(2)
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
		// read it keeps reading it.
		fmt.Fprintln(stderr, invalid.Error())
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
