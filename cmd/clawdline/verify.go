package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline verify`: the things waiting to be verified (docs/verifications.md).
//
//	clawdline verify list [--json]
//	clawdline verify show <id> [--json]
//	clawdline verify add --title … --due … --criterion … [--criterion …] [--why …]
//	                     [--started …] [--compaction-since 14d] [--schedule <id>]
//	clawdline verify note <id> "…"
//	clawdline verify done <id> --accepted|--rejected "reason"
//	clawdline verify delete <id> [--force]
//
// Delete takes exactly one id. There is no bulk delete: every record is
// something somebody asked to be reminded of.

func verifyCommand(args []string) {
	if len(args) == 0 {
		verifyUsage()
	}
	port, rest := verifyPort(args[1:])
	b, err := openBroker(port)
	if err != nil {
		fail(err)
	}
	os.Exit(runVerify(os.Stdout, os.Stderr, b, args[0], rest, os.Getenv, time.Now()))
}

func verifyUsage() {
	for _, line := range []string{
		"usage: clawdline verify list [--json]",
		"       clawdline verify show <id> [--json]",
		"       clawdline verify add --title <text> --due <when> --criterion <text> [--criterion <text> …]",
		"                            [--why <text>] [--started <when>] [--compaction-since 14d] [--schedule <id>]",
		"       clawdline verify note <id> <text>",
		"       clawdline verify done <id> --accepted|--rejected <reason>",
		"       clawdline verify delete <id> [--force]",
		"  <when> is 2026-10-03 09:00 (this machine's zone), 2026-10-03, RFC 3339, or 7d / 36h from now",
		"  every form takes --port n (default CLAWDLINE_NEXT_PORT, else 7727)",
	} {
		fmt.Fprintln(os.Stderr, line)
	}
	os.Exit(2)
}

// verifyPort takes --port out of the arguments wherever it stands, so every
// subcommand's own parsing sees only its own flags.
func verifyPort(args []string) (int, []string) {
	port := 0
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--port" && i+1 < len(args):
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				verifyUsage()
			}
			port = n
			i++
		case strings.HasPrefix(a, "--port="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--port="))
			if err != nil {
				verifyUsage()
			}
			port = n
		default:
			rest = append(rest, a)
		}
	}
	return port, rest
}

// runVerify is the command, answering its exit status: 0 done, 1 refused or
// unreachable, 2 misused.
func runVerify(stdout, stderr io.Writer, b *broker, sub string, args []string, getenv func(string) string, now time.Time) int {
	misuse := func(msg string) int {
		fmt.Fprintln(stderr, "clawdline verify "+sub+": "+msg)
		return 2
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("verify list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		asJSON := fs.Bool("json", false, "")
		if fs.Parse(args) != nil || fs.NArg() != 0 {
			return misuse("takes only --json")
		}
		return verifyList(stdout, stderr, b, *asJSON, now)
	case "show":
		id, rest, ok := verifyID(args)
		if !ok {
			return misuse("names one record: clawdline verify show <id>")
		}
		fs := flag.NewFlagSet("verify show", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		asJSON := fs.Bool("json", false, "")
		if fs.Parse(rest) != nil || fs.NArg() != 0 {
			return misuse("takes only --json after the id")
		}
		return verifyShow(stdout, stderr, b, id, *asJSON, now)
	case "add":
		return verifyAdd(stdout, stderr, b, args, now, misuse)
	case "note":
		id, rest, ok := verifyID(args)
		if !ok || len(rest) != 1 || strings.TrimSpace(rest[0]) == "" {
			return misuse(`names one record and one note: clawdline verify note <id> "…"`)
		}
		body := contract.VerificationNoteCreate{Text: rest[0], Session: conversationOf(getenv)}
		return verifyWrite(stdout, stderr, b, http.MethodPost, "/v1/verifications/"+url.PathEscape(id)+"/notes", nil, body, newKey("verify-note"))
	case "done":
		id, rest, ok := verifyID(args)
		if !ok || len(rest) != 2 {
			return misuse(`names one record, a verdict and the reason: clawdline verify done <id> --accepted|--rejected "reason"`)
		}
		var status contract.VerificationStatus
		switch rest[0] {
		case "--accepted":
			status = contract.VerificationStatusAccepted
		case "--rejected":
			status = contract.VerificationStatusRejected
		default:
			return misuse("the verdict is --accepted or --rejected")
		}
		return verifyWrite(stdout, stderr, b, http.MethodPost, "/v1/verifications/"+url.PathEscape(id)+"/close", nil,
			contract.VerificationClose{Status: status, Reason: rest[1]}, "")
	case "delete":
		id, rest, ok := verifyID(args)
		if !ok {
			return misuse("names exactly one record: clawdline verify delete <id> [--force]")
		}
		var query url.Values
		switch {
		case len(rest) == 0:
		case len(rest) == 1 && rest[0] == "--force":
			query = url.Values{"force": {"1"}}
		default:
			return misuse("takes one id and, for a record still open, --force")
		}
		a, err := b.request(http.MethodDelete, "/v1/verifications/"+url.PathEscape(id), query, nil, "")
		if err != nil {
			fmt.Fprintln(stderr, "clawdline verify:", err)
			return 1
		}
		if !a.ok() {
			return report(stdout, stderr, "verify", a)
		}
		fmt.Fprintln(stdout, "deleted "+id)
		return 0
	}
	return misuse("is not a subcommand: list, show, add, note, done or delete")
}

// verifyID is the one id every subcommand but list and add starts with.
func verifyID(args []string) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || strings.TrimSpace(args[0]) == "" {
		return "", nil, false
	}
	return args[0], args[1:], true
}

// conversationOf is the calling session's conversation, which a note is
// signed with; empty when the command is not run by an assistant.
func conversationOf(getenv func(string) string) string {
	for _, name := range conversationEnv {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// repeated is a flag that may be given more than once.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, "; ") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

func verifyAdd(stdout, stderr io.Writer, b *broker, args []string, now time.Time, misuse func(string) int) int {
	fs := flag.NewFlagSet("verify add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	title := fs.String("title", "", "")
	why := fs.String("why", "", "")
	due := fs.String("due", "", "")
	started := fs.String("started", "", "")
	since := fs.String("compaction-since", "", "")
	schedule := fs.String("schedule", "", "")
	var criteria repeated
	fs.Var(&criteria, "criterion", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return misuse("reads --title, --due, --criterion (repeatable), --why, --started, --compaction-since and --schedule")
	}
	if strings.TrimSpace(*title) == "" || *due == "" || len(criteria) == 0 {
		return misuse("needs --title, --due and at least one --criterion")
	}
	body := contract.VerificationCreate{Title: *title, Why: *why, Criteria: criteria, ScheduleID: *schedule}
	at, err := parseWhen(*due, now)
	if err != nil {
		return misuse("--due: " + err.Error())
	}
	body.DueAt = at.Unix()
	if *started != "" {
		at, err := parseWhen(*started, now)
		if err != nil {
			return misuse("--started: " + err.Error())
		}
		body.StartedAt = at.Unix()
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "compaction-since" {
			body.Source = &contract.VerificationSource{Kind: string(contract.VerificationSourceKindCompactionCompare), Since: *since}
		}
	})
	return verifyWrite(stdout, stderr, b, http.MethodPost, "/v1/verifications", nil, body, newKey("verify-add"))
}

// parseWhen reads a moment as a person writes one: a date and a time in this
// machine's zone, a date alone (its midnight), RFC 3339, or a number of days
// or hours from now.
func parseWhen(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if n := len(raw); n > 1 && (raw[n-1] == 'd' || raw[n-1] == 'h') {
		if count, err := strconv.Atoi(raw[:n-1]); err == nil && count >= 0 {
			unit := 24 * time.Hour
			if raw[n-1] == 'h' {
				unit = time.Hour
			}
			return now.Add(time.Duration(count) * unit), nil
		}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, raw, now.Location()); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not 2026-10-03 09:00, 2026-10-03, RFC 3339, or 7d / 36h", raw)
}

// verifyWrite sends one change and prints the record as it now stands.
func verifyWrite(stdout, stderr io.Writer, b *broker, method, path string, query url.Values, body any, key string) int {
	a, err := b.request(method, path, query, body, key)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline verify:", err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "verify", a)
	}
	var d contract.VerificationDetail
	if json.Unmarshal(a.Body, &d) != nil {
		return report(stdout, stderr, "verify", a)
	}
	writeVerification(stdout, d.Verification, time.Unix(d.At, 0))
	return 0
}

func verifyList(stdout, stderr io.Writer, b *broker, asJSON bool, now time.Time) int {
	a, err := b.request(http.MethodGet, "/v1/verifications", nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline verify:", err)
		return 1
	}
	if asJSON || !a.ok() {
		return report(stdout, stderr, "verify", a)
	}
	var list contract.VerificationList
	if json.Unmarshal(a.Body, &list) != nil {
		fmt.Fprintln(stderr, "clawdline verify: the daemon's answer could not be read; --json prints it as it came")
		return 1
	}
	if len(list.Verifications) == 0 {
		fmt.Fprintln(stdout, "nothing is waiting to be verified")
		return 0
	}
	at := time.Unix(list.At, 0)
	for _, v := range list.Verifications {
		fmt.Fprintf(stdout, "%s  %-8s  %-18s  %s  (%s)\n", v.ID, v.Status, verifyDue(v, at), v.Title, verifyTally(v))
	}
	return 0
}

func verifyShow(stdout, stderr io.Writer, b *broker, id string, asJSON bool, now time.Time) int {
	a, err := b.request(http.MethodGet, "/v1/verifications/"+url.PathEscape(id), nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, "clawdline verify:", err)
		return 1
	}
	if asJSON || !a.ok() {
		return report(stdout, stderr, "verify", a)
	}
	var d contract.VerificationDetail
	if json.Unmarshal(a.Body, &d) != nil {
		fmt.Fprintln(stderr, "clawdline verify: the daemon's answer could not be read; --json prints it as it came")
		return 1
	}
	writeVerification(stdout, d.Verification, time.Unix(d.At, 0))
	if d.Data != nil {
		fmt.Fprintln(stdout, "")
		switch {
		case d.Data.Error != "":
			fmt.Fprintf(stdout, "data (%s): %s\n", d.Data.Kind, d.Data.Error)
		case d.Data.CompactionCompare != nil:
			fmt.Fprintf(stdout, "data (%s):\n", d.Data.Kind)
			writeCompactionComparison(stdout, *d.Data.CompactionCompare)
		}
	}
	return 0
}

// writeVerification is one record as lines: the head, why, the criteria with
// their marks, where its data comes from, the notes, and the verdict.
func writeVerification(w io.Writer, v contract.Verification, at time.Time) {
	fmt.Fprintf(w, "%s  %s — %s, %s\n", v.ID, v.Title, v.Status, verifyDue(v, at))
	fmt.Fprintf(w, "started %s, due %s\n", verifyTime(v.StartedAt), verifyTime(v.DueAt))
	if v.Why != "" {
		fmt.Fprintln(w, "why: "+v.Why)
	}
	for _, c := range v.Criteria {
		mark := " "
		switch c.State {
		case contract.VerificationCriterionStatePassed:
			mark = "✓"
		case contract.VerificationCriterionStateFailed:
			mark = "✗"
		}
		fmt.Fprintf(w, "  [%s] %d. %s\n", mark, c.Index, c.Text)
	}
	if v.Source != nil {
		fmt.Fprintf(w, "data: %s since %s\n", v.Source.Kind, v.Source.Since)
	}
	if v.ScheduleID != "" {
		fmt.Fprintln(w, "schedule: "+v.ScheduleID)
	}
	for _, n := range v.Notes {
		who := "person"
		if n.AuthorKind == contract.VerificationAuthorKindSession {
			who = "session"
			if n.Author != "" {
				who += " " + n.Author
			}
		}
		fmt.Fprintf(w, "note %s (%s): %s\n", verifyTime(n.At), who, n.Text)
	}
	if v.Status != contract.VerificationStatusOpen {
		fmt.Fprintf(w, "%s %s: %s\n", v.Status, verifyTime(v.ClosedAt), v.CloseReason)
	}
}

func verifyTime(unix int64) string {
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04")
}

// verifyDue is how far the due time is from the daemon's clock, or how long
// ago it passed.
func verifyDue(v contract.Verification, at time.Time) string {
	if v.Status != contract.VerificationStatusOpen {
		return "closed"
	}
	d := time.Unix(v.DueAt, 0).Sub(at)
	late := d < 0
	if late {
		d = -d
	}
	days, hours := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour)
	span := fmt.Sprintf("%dd %dh", days, hours)
	if days == 0 {
		span = fmt.Sprintf("%dh %dm", hours, int(d%time.Hour/time.Minute))
	}
	if late {
		return "overdue by " + span
	}
	return "due in " + span
}

// verifyTally is how many criteria have been marked, and how.
func verifyTally(v contract.Verification) string {
	passed, failed := 0, 0
	for _, c := range v.Criteria {
		switch c.State {
		case contract.VerificationCriterionStatePassed:
			passed++
		case contract.VerificationCriterionStateFailed:
			failed++
		}
	}
	return fmt.Sprintf("%d passed, %d failed of %d", passed, failed, len(v.Criteria))
}
