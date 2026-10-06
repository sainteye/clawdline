package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline project unify` shows, and on --apply performs, the plan that
// gives one Project's Claude and Codex sessions the same rules file and the
// same skills (docs/project-files.md, Unify). The daemon computes the plan;
// this command only reads it, prints it for a person, and sends back the
// version the person saw.

// Exit codes of --check, and of a plan read: 0 unified, 1 drifting, 3 unknown
// (the plan or the daemon could not be read).
const (
	unifyExitUnified  = 0
	unifyExitDrifting = 1
	unifyExitUnknown  = 3
)

type unifyClient struct {
	base   string
	token  string
	client *http.Client
}

func openUnifyClient() (*unifyClient, error) {
	port, err := daemonPort()
	if err != nil {
		return nil, err
	}
	token, err := localToken(config.Load())
	if err != nil {
		return nil, err
	}
	return &unifyClient{base: "http://127.0.0.1:" + strconv.Itoa(port), token: token,
		client: &http.Client{Timeout: 30 * time.Second}}, nil
}

// call returns the status and body; a transport failure is an error, a
// refusal is a status the caller reads.
func (c *unifyClient) call(method, path string, body []byte, key string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("the daemon did not answer: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("the daemon's answer could not be read: %w", err)
	}
	return res.StatusCode, data, nil
}

func refusalOf(status int, data []byte) error {
	var refusal contract.Refusal
	if json.Unmarshal(data, &refusal) == nil && refusal.Error != "" {
		return fmt.Errorf("%s (%s)", refusal.Detail, refusal.Error)
	}
	return fmt.Errorf("the daemon answered %d", status)
}

// placeFor finds the daemon's Project whose repository is dir's.
func (c *unifyClient) placeFor(dir string) (string, error) {
	want, ok := projects.CanonicalProjectKey(dir)
	if !ok {
		return "", fmt.Errorf("%s is not inside a Git repository", dir)
	}
	status, data, err := c.call(http.MethodGet, "/v1/places", nil, "")
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", refusalOf(status, data)
	}
	var list contract.StartPlaceList
	if err := json.Unmarshal(data, &list); err != nil {
		return "", fmt.Errorf("the daemon's Project list was not readable: %w", err)
	}
	for _, p := range list.Places {
		if got, ok := projects.CanonicalProjectKey(p.Path); ok && got == want {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("this machine does not list %s as a Project; run `clawdline project add %s` first", want, want)
}

func gitTopLevel(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not inside a Git repository", dir)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func projectUnifyCommand(args []string) {
	fs := flag.NewFlagSet("project unify", flag.ExitOnError)
	apply := fs.Bool("apply", false, "apply the plan just printed")
	check := fs.Bool("check", false, "print only the status and drift; exit 0 unified, 1 drifting, 3 unknown")
	asJSON := fs.Bool("json", false, "print the daemon's answer")
	_ = fs.Parse(args)
	if fs.NArg() > 1 || *apply && *check {
		fmt.Fprintln(os.Stderr, "usage: clawdline project unify [--apply | --check] [--json] [directory]")
		os.Exit(2)
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir, err = gitTopLevel(abs)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(unifyExitUnknown)
	}
	c, err := openUnifyClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(unifyExitUnknown)
	}
	os.Exit(runProjectUnify(os.Stdout, os.Stderr, c, dir, *apply, *check, *asJSON))
}

func runProjectUnify(stdout, stderr io.Writer, c *unifyClient, dir string, apply, check, asJSON bool) int {
	place, err := c.placeFor(dir)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline:", err)
		return unifyExitUnknown
	}
	path := "/v1/projects/" + url.PathEscape(place) + "/unify"
	status, data, err := c.call(http.MethodGet, path, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline:", err)
		return unifyExitUnknown
	}
	if status != http.StatusOK {
		fmt.Fprintln(stderr, "clawdline:", refusalOf(status, data))
		return unifyExitUnknown
	}
	var plan contract.ProjectUnifyPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		fmt.Fprintln(stderr, "clawdline: the plan was not readable:", err)
		return unifyExitUnknown
	}
	switch {
	case check:
		if asJSON {
			stdout.Write(indentJSON(data))
		} else {
			printUnifyCheck(stdout, plan)
		}
		return unifyExit(plan.Status)
	case !apply:
		if asJSON {
			stdout.Write(indentJSON(data))
		} else {
			printUnifyPlan(stdout, dir, plan)
		}
		return unifyExit(plan.Status)
	}
	if !asJSON {
		printUnifyPlan(stdout, dir, plan)
	}
	if plan.Status == contract.ProjectUnifyStatusUnknown {
		fmt.Fprintln(stderr, "clawdline: part of the plan could not be read, so nothing was applied.")
		return unifyExitUnknown
	}
	if len(plan.Actions) == 0 {
		if !asJSON {
			fmt.Fprintln(stdout, "\nNothing to apply.")
		} else {
			stdout.Write(indentJSON(data))
		}
		return unifyExit(plan.Status)
	}
	body, _ := json.Marshal(contract.ProjectUnifyApply{Version: plan.Version})
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	status, data, err = c.call(http.MethodPost, path, body, "unify-"+hex.EncodeToString(key))
	if err != nil {
		// The request may have run. Reading the plan again says what disk holds.
		fmt.Fprintln(stderr, "clawdline:", err, "— whether it applied is unknown; run `clawdline project unify` to see.")
		return unifyExitUnknown
	}
	var out contract.ProjectUnifyApplied
	decoded := json.Unmarshal(data, &out) == nil
	switch {
	case status == http.StatusOK && decoded:
	case status == http.StatusOK:
		fmt.Fprintln(stderr, "clawdline: the answer was not readable; run `clawdline project unify` to see what disk holds.")
		return unifyExitUnknown
	case decoded && out.Outcome == contract.ProjectUnifyOutcomeStopped:
		// Stopped part-way: printed below with what ran.
	default:
		fmt.Fprintln(stderr, "clawdline: nothing was applied:", refusalOf(status, data))
		return unifyExitDrifting
	}
	if asJSON {
		stdout.Write(indentJSON(data))
	} else {
		fmt.Fprintf(stdout, "\nApplied %d of %d actions:\n", len(out.Ran), len(plan.Actions))
		for i, a := range out.Ran {
			fmt.Fprintf(stdout, "  %d. %s\n", i+1, a.Description)
		}
		if out.Failed != nil {
			fmt.Fprintf(stdout, "Stopped at: %s\n  %s (%s)\n", out.Failed.Description, out.Detail, out.Error)
		}
		fmt.Fprintln(stdout, "Nothing was committed to git.")
		fmt.Fprintln(stdout)
		printUnifyCheck(stdout, out.Plan)
	}
	if out.Outcome == contract.ProjectUnifyOutcomeStopped {
		return unifyExitDrifting
	}
	return unifyExit(out.Plan.Status)
}

func unifyExit(status contract.ProjectUnifyStatus) int {
	switch status {
	case contract.ProjectUnifyStatusUnified:
		return unifyExitUnified
	case contract.ProjectUnifyStatusDrifting:
		return unifyExitDrifting
	}
	return unifyExitUnknown
}

func indentJSON(data []byte) []byte {
	var b bytes.Buffer
	if json.Indent(&b, data, "", "  ") != nil {
		return append(data, '\n')
	}
	b.WriteByte('\n')
	return b.Bytes()
}

func printUnifyCheck(w io.Writer, p contract.ProjectUnifyPlan) {
	fmt.Fprintf(w, "status: %s\n", p.Status)
	for _, a := range p.Actions {
		fmt.Fprintf(w, "drift: %s\n", a.Description)
	}
	for _, c := range p.Conflicts {
		fmt.Fprintf(w, "conflict: %s: %s\n", c.Path, c.Detail)
	}
}

func seenText(s contract.ProjectUnifySeen) string {
	switch {
	case s.Claude && s.Codex:
		return "Claude and Codex"
	case s.Claude:
		return "Claude only"
	case s.Codex:
		return "Codex only"
	}
	return "neither"
}

func listText(paths []string) string {
	if len(paths) == 0 {
		return "nothing"
	}
	return strings.Join(paths, ", ")
}

func printUnifyPlan(w io.Writer, dir string, p contract.ProjectUnifyPlan) {
	fmt.Fprintf(w, "Project: %s\nStatus: %s\n\n", dir, p.Status)
	fmt.Fprintln(w, "Rules")
	t := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(t, "  Claude reads now:\t%s\n", listText(p.Rules.Now.Claude))
	fmt.Fprintf(t, "  Claude will read:\t%s\n", listText(p.Rules.After.Claude))
	fmt.Fprintf(t, "  Codex reads now:\t%s\n", listText(p.Rules.Now.Codex))
	fmt.Fprintf(t, "  Codex will read:\t%s\n", listText(p.Rules.After.Codex))
	_ = t.Flush()
	if len(p.Rules.ClaudeOnlyLines) > 0 {
		fmt.Fprintln(w, "  Lines in CLAUDE.md that Codex does not see:")
		for _, line := range p.Rules.ClaudeOnlyLines {
			fmt.Fprintf(w, "    | %s\n", line)
		}
	}
	if len(p.Rules.ClaudeOnlyFiles) > 0 {
		fmt.Fprintf(w, "  Read by Claude only, never changed here: %s\n", strings.Join(p.Rules.ClaudeOnlyFiles, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Skills")
	if len(p.Skills) == 0 {
		fmt.Fprintln(w, "  none in .agents/skills or .claude/skills")
	} else {
		t = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(t, "  NAME\tSEEN NOW BY\tWILL BE SEEN BY\tWHERE")
		for _, s := range p.Skills {
			where := string(s.Place)
			if s.Linked {
				where += ", linked"
			}
			if s.Copy {
				where += ", copies"
			}
			fmt.Fprintf(t, "  %s\t%s\t%s\t%s\n", s.Name, seenText(s.Now), seenText(s.After), where)
		}
		_ = t.Flush()
	}
	fmt.Fprintln(w)
	if len(p.Actions) == 0 {
		fmt.Fprintln(w, "Actions: none")
	} else {
		fmt.Fprintln(w, "Actions, in order")
		for i, a := range p.Actions {
			fmt.Fprintf(w, "  %d. %s\n", i+1, a.Description)
		}
	}
	if len(p.Conflicts) > 0 {
		fmt.Fprintln(w, "\nFor you to resolve (unify does not change these)")
		for _, c := range p.Conflicts {
			fmt.Fprintf(w, "  - %s: %s\n", c.Path, c.Detail)
		}
	}
	if len(p.Actions) > 0 {
		fmt.Fprintf(w, "\nNothing has been changed. `clawdline project unify --apply` performs the %d actions above.\n", len(p.Actions))
	}
}
