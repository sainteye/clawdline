package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// `clawdline dispatch`: an owned child in one command, the four steps of guide
// §4 that roots used to type as curl.
//
// Measured on 2026-09-25 across root transcripts: one root wrote 786
// hand-built curl commands, each reading the orchestrator token with `cat` and
// assembling JSON (median 739 bytes of output per command, every byte of it
// output tokens), met `stale_inventory` in chains when it dispatched several
// children in a row, and was answered `unauthorized` 13 times for sending the
// wrong token. Here the token is read the way every thin command reads it
// (broker.go), the generation is read immediately before the POST and read
// once more on a stale answer, and what comes back is one line.
//
// **The secret stays in this process.** It is made here with crypto/rand and
// goes to the daemon in the POST body, which is never argv; it is not written
// to task.json, not printed, and masked in any answer that somehow carried it.

// dispatchInstructionsLimit is the daemon's own limit on a brief's
// instructions (internal/app/orchestrator.instructionsLimit, the Swift app's
// 16 KiB). It is checked here too so that an oversized brief is refused before
// a task directory exists, and it bounds the read of stdin.
const dispatchInstructionsLimit = 16 * 1024

// conversationAssistant says which assistant exports each variable in
// conversationEnv.
var conversationAssistant = map[string]string{
	"CLAUDE_CODE_SESSION_ID": "claude",
	"CODEX_THREAD_ID":        "codex",
	"CODEX_SESSION_ID":       "codex",
}

// listFlag is a flag that may be given more than once, each value split on
// commas. set says whether it was given at all: `--claims ""` is an explicit
// empty write set, which is a different answer from forgetting the flag.
type listFlag struct {
	values []string
	set    bool
}

func (l *listFlag) String() string { return strings.Join(l.values, ",") }

func (l *listFlag) Set(v string) error {
	l.set = true
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			l.values = append(l.values, part)
		}
	}
	return nil
}

// dispatchOptions is one dispatch as the flags spelled it.
type dispatchOptions struct {
	Title, ProjectDir, Assistant, Isolation, PermissionMode string
	Kind, Model, WorkID, Label, Conversation, RootAssistant string
	Claims                                                  []string
	ClaimsGiven                                             bool
	Deliverables                                            []string
	Timeout                                                 int
	Instructions                                            string
	JSON                                                    bool
}

// dispatchEnv is what the command takes from its surroundings, so that a test
// can say it.
type dispatchEnv struct {
	getenv func(string) string
	// toplevel is the current directory's git top-level.
	toplevel func() (string, error)
	// fresh answers a new task id and secret.
	fresh func() (id, secret string, err error)
}

func dispatchCommand(args []string) {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o dispatchOptions
	var claims, deliverables listFlag
	fs.StringVar(&o.Title, "title", "", "shown on screen; cut at 200 characters")
	fs.Var(&claims, "claims", "the relative paths the child may write, comma-separated; repeatable")
	fs.StringVar(&o.ProjectDir, "project-dir", "", "the repository (default: this directory's git top-level)")
	fs.StringVar(&o.Assistant, "assistant", "", "claude or codex (default: the one running this command)")
	fs.StringVar(&o.Isolation, "isolation", "none", "none or worktree")
	fs.StringVar(&o.PermissionMode, "permission-mode", "full", "ask, edits or full")
	fs.IntVar(&o.Timeout, "timeout", 30, "minutes, 1–240")
	fs.StringVar(&o.Kind, "kind", "", "optional")
	fs.Var(&deliverables, "deliverable", "a path the child delivers; repeatable")
	fs.StringVar(&o.Model, "model", "", "optional model override")
	fs.StringVar(&o.WorkID, "work-id", "", "the board item this serves (a UUID)")
	fs.StringVar(&o.Label, "label", "", "this root's label on screen")
	instructionsFile := fs.String("instructions-file", "", "the brief (default: stdin)")
	fs.StringVar(&o.Conversation, "conversation", "", "this assistant's conversation id (default: from the environment)")
	fs.StringVar(&o.RootAssistant, "root-assistant", "", "claude or codex, when --conversation names one the environment does not")
	fs.BoolVar(&o.JSON, "json", false, "print the daemon's answer as it came")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		dispatchUsage()
	}
	o.Claims, o.ClaimsGiven, o.Deliverables = claims.values, claims.set, deliverables.values
	// The flags are judged before stdin is waited on or the token is read:
	// a forgotten --title answers at once rather than after a brief.
	if code := checkDispatchFlags(os.Stderr, o); code != 0 {
		os.Exit(code)
	}
	instructions, err := readInstructions(*instructionsFile, os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline dispatch:", err)
		os.Exit(2)
	}
	o.Instructions = instructions
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(dispatchTask(os.Stdout, os.Stderr, b, o, dispatchEnv{
		getenv: os.Getenv, toplevel: gitToplevel, fresh: freshTask,
	}))
}

func dispatchUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline dispatch --title <t> --claims a,b [--claims c] [--project-dir d] "+
		"[--assistant claude|codex] [--isolation none|worktree] [--permission-mode ask|edits|full] [--timeout min] "+
		"[--kind k] [--deliverable p …] [--model m] [--work-id uuid] [--label l] "+
		"[--instructions-file f | instructions on stdin] [--conversation id] [--port n] [--json]")
	fmt.Fprintln(os.Stderr, "  dispatches an owned child: writes task.json, reads the inventory, posts the task")
	os.Exit(2)
}

// readInstructions reads the brief from a file, or from stdin when none is
// named, refusing past the daemon's limit without reading the rest.
func readInstructions(path string, stdin io.Reader) (string, error) {
	from, source := stdin, "stdin"
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		from, source = f, path
	}
	data, err := io.ReadAll(io.LimitReader(from, dispatchInstructionsLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > dispatchInstructionsLimit {
		return "", fmt.Errorf("the instructions in %s are longer than %d bytes, the most a task may carry. "+
			"Nothing was dispatched: shorten them, or put the detail in a file the child reads", source, dispatchInstructionsLimit)
	}
	return string(data), nil
}

// dispatchRefusal says why nothing was dispatched, answering the usage status.
func dispatchRefusal(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, "clawdline dispatch: "+format+" Nothing was dispatched.\n", args...)
	return 2
}

// checkDispatchFlags refuses the flags a dispatch cannot go without.
func checkDispatchFlags(stderr io.Writer, o dispatchOptions) int {
	if strings.TrimSpace(o.Title) == "" {
		return dispatchRefusal(stderr, "--title is required.")
	}
	if !o.ClaimsGiven {
		return dispatchRefusal(stderr, "--claims is required: the relative paths the child may write (--claims '' for none).")
	}
	return 0
}

// dispatchTask is the command, answering its exit status.
func dispatchTask(stdout, stderr io.Writer, b *broker, o dispatchOptions, env dispatchEnv) int {
	usage := func(format string, args ...any) int { return dispatchRefusal(stderr, format, args...) }
	if code := checkDispatchFlags(stderr, o); code != 0 {
		return code
	}
	o.Title = strings.TrimSpace(o.Title)
	if strings.TrimSpace(o.Instructions) == "" {
		return usage("there are no instructions: pass --instructions-file, or send them on stdin.")
	}
	if len(o.Instructions) > dispatchInstructionsLimit {
		return usage("the instructions are longer than %d bytes, the most a task may carry.", dispatchInstructionsLimit)
	}

	// The root: this conversation, and which assistant it is.
	conversation, rootAssistant := strings.TrimSpace(o.Conversation), strings.TrimSpace(o.RootAssistant)
	for _, name := range conversationEnv {
		v := strings.TrimSpace(env.getenv(name))
		if v == "" {
			continue
		}
		if conversation == "" {
			conversation = v
		}
		if rootAssistant == "" {
			rootAssistant = conversationAssistant[name]
		}
		break
	}
	if conversation == "" {
		return usage("cannot tell which conversation is dispatching: none of %s is set. "+
			"Pass --conversation <this assistant's conversation id>.", strings.Join(conversationEnv, ", "))
	}
	if rootAssistant == "" {
		return usage("cannot tell which assistant %s belongs to. Pass --root-assistant claude or codex.", conversation)
	}
	if o.Assistant == "" {
		o.Assistant = rootAssistant
	}

	if o.ProjectDir == "" {
		top, err := env.toplevel()
		if err != nil {
			return usage("--project-dir was not given and this directory's git top-level could not be read: %v.", err)
		}
		o.ProjectDir = top
	}
	if abs, err := filepath.Abs(o.ProjectDir); err == nil {
		o.ProjectDir = abs
	}

	inv, code := readDispatchInventory(stderr, b, o)
	if code != 0 {
		return code
	}
	id, secret, err := env.fresh()
	if err != nil {
		fmt.Fprintln(stderr, "clawdline dispatch: no task id or secret could be made:", err)
		return 1
	}
	dir, err := writeTaskFile(inv.TaskRoot, id, taskFile(id, o, conversation, rootAssistant))
	if err != nil {
		fmt.Fprintln(stderr, "clawdline dispatch:", err, "Nothing was dispatched.")
		return 1
	}
	warnings := inv.overlapLines()

	post := func(generation string) (answer, error) {
		a, err := b.request(http.MethodPost, "/v1/orchestrator/tasks", nil, map[string]string{
			"task_id": id, "secret": secret, "inventory_generation": generation,
		}, "")
		a.Body = maskSecret(a.Body, secret)
		return a, err
	}
	a, err := post(inv.Generation)
	if err == nil && a.Status == http.StatusConflict && refusalCode(a) == "stale_inventory" {
		// Once: something started or ended between the read and the post.
		// A second stale answer means the repository is moving faster than
		// this command can follow, and that is said rather than chased.
		again, code := readDispatchInventory(stderr, b, o)
		if code != 0 {
			_ = os.RemoveAll(dir)
			return code
		}
		warnings = again.overlapLines()
		a, err = post(again.Generation)
	}
	if err != nil {
		// Whether the daemon took it is unknown, so the brief stays.
		fmt.Fprintln(stderr, "clawdline dispatch:", err)
		fmt.Fprintf(stderr, "Whether task %s was dispatched is unknown: GET /v1/orchestrator/tasks/%s answers it. "+
			"Its task.json stays at %s.\n", id, id, dir)
		return 1
	}
	if !a.ok() {
		// A refusal made nothing: the directory this command made is its own,
		// and a task id is never reused.
		_ = os.RemoveAll(dir)
		return report(stdout, stderr, "dispatch", a)
	}
	if o.JSON {
		for _, w := range warnings {
			fmt.Fprintln(stderr, w)
		}
		return report(stdout, stderr, "dispatch", a)
	}
	var got struct {
		Replayed bool `json:"replayed"`
		Task     struct {
			ID       string `json:"id"`
			State    string `json:"state"`
			Worktree *struct {
				Path string `json:"path"`
			} `json:"worktree"`
		} `json:"task"`
		Warnings []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"warnings"`
	}
	if json.Unmarshal(a.Body, &got) != nil || got.Task.ID == "" {
		fmt.Fprintf(stderr, "clawdline dispatch: the daemon said yes with an answer this command cannot read: %s\n",
			strings.TrimSpace(string(a.Body)))
		return 1
	}
	line := "dispatched " + got.Task.ID + " " + got.Task.State
	if got.Task.Worktree != nil && got.Task.Worktree.Path != "" {
		line += " worktree " + got.Task.Worktree.Path
	}
	if got.Replayed {
		line += " (replayed)"
	}
	fmt.Fprintln(stdout, line)
	for _, w := range got.Warnings {
		fmt.Fprintf(stdout, "warning %s: %s\n", w.Code, w.Message)
	}
	for _, w := range warnings {
		fmt.Fprintln(stdout, w)
	}
	return 0
}

// dispatchInventory is the part of the inventory a dispatch reads.
type dispatchInventory struct {
	Generation string `json:"generation"`
	TaskRoot   string `json:"task_root"`
	Live       []struct {
		Task     string   `json:"task"`
		Title    string   `json:"title"`
		State    string   `json:"state"`
		Overlaps []string `json:"overlaps"`
	} `json:"live"`
}

// overlapLines are the live rows whose claims meet this dispatch's.
func (inv dispatchInventory) overlapLines() []string {
	var lines []string
	for _, row := range inv.Live {
		if len(row.Overlaps) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("warning overlap: live task %s (%s) %q also claims %s",
			row.Task, row.State, row.Title, strings.Join(row.Overlaps, ", ")))
	}
	return lines
}

// readDispatchInventory is GET /v1/orchestrator/inventory for this project and
// these claims. A non-zero code is the exit status, already explained.
func readDispatchInventory(stderr io.Writer, b *broker, o dispatchOptions) (dispatchInventory, int) {
	query := url.Values{"project": {o.ProjectDir}}
	if len(o.Claims) > 0 {
		query.Set("claims", strings.Join(o.Claims, ","))
	}
	a, err := b.request(http.MethodGet, "/v1/orchestrator/inventory", query, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline dispatch:", err, "Nothing was dispatched.")
		return dispatchInventory{}, 1
	}
	if !a.ok() {
		return dispatchInventory{}, report(io.Discard, stderr, "dispatch (inventory)", a)
	}
	var inv dispatchInventory
	if json.Unmarshal(a.Body, &inv) != nil || inv.Generation == "" || !filepath.IsAbs(inv.TaskRoot) {
		fmt.Fprintln(stderr, "clawdline dispatch: the inventory answered without a generation or an absolute task_root. "+
			"Nothing was dispatched.")
		return dispatchInventory{}, 1
	}
	return inv, 0
}

// taskFile is task.json as guide §4's table lists it. The secret is not in it.
func taskFile(id string, o dispatchOptions, conversation, rootAssistant string) map[string]any {
	claims := o.Claims
	if claims == nil {
		claims = []string{}
	}
	root := map[string]any{"session_id": conversation, "assistant": rootAssistant}
	if o.Label != "" {
		root["label"] = o.Label
	}
	task := map[string]any{
		"clawdline_protocol": 1,
		"task_id":            id,
		"assistant":          o.Assistant,
		"project_dir":        o.ProjectDir,
		"title":              o.Title,
		"instructions":       o.Instructions,
		"claims":             claims,
		"isolation":          o.Isolation,
		"permission_mode":    o.PermissionMode,
		"timeout_minutes":    o.Timeout,
		"root":               root,
	}
	if o.Kind != "" {
		task["kind"] = o.Kind
	}
	if len(o.Deliverables) > 0 {
		task["deliverables"] = o.Deliverables
	}
	if o.Model != "" {
		task["model"] = o.Model
	}
	if o.WorkID != "" {
		task["work_id"] = o.WorkID
	}
	return task
}

// writeTaskFile writes <root>/<id>/task.json, the directory 0700 and the file
// 0600, and answers the directory. The directory must not exist yet: an id is
// made fresh for each dispatch, so one that is already there is somebody
// else's.
func writeTaskFile(root, id string, task map[string]any) (string, error) {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("the task root %s could not be made: %w.", root, err)
	}
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("the task directory %s could not be made: %w.", dir, err)
	}
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("%s could not be written: %w.", path, err)
	}
	return dir, nil
}

// refusalCode is the code of a refusal answer, or "".
func refusalCode(a answer) string {
	code, _ := a.refusal()
	return code
}

// maskSecret replaces the task secret wherever an answer carries it.
func maskSecret(data []byte, secret string) []byte {
	if secret == "" || len(data) == 0 {
		return data
	}
	return []byte(strings.ReplaceAll(string(data), secret, "<task-secret>"))
}

// freshTask is a new lowercase UUID v4 and a 64-hex secret.
func freshTask() (string, string, error) {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return "", "", err
	}
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	h := hex.EncodeToString(u[:])
	id := h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
	s := make([]byte, 32)
	if _, err := rand.Read(s); err != nil {
		return "", "", err
	}
	return id, hex.EncodeToString(s), nil
}

// gitToplevel is the current directory's repository root.
func gitToplevel() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("this directory is not in a git repository")
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
