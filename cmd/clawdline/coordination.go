package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

func coordinationCommand(args []string) {
	if len(args) == 0 {
		coordinationUsage()
	}
	if args[0] == "run" {
		coordinationRunCommand(args[1:])
		return
	}
	verb := args[0]
	fs := flag.NewFlagSet("coordination "+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "daemon port")
	reason := fs.String("reason", "", "pause reason")
	wake := fs.String("wake", "", "wake condition")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args[1:]); err != nil {
		coordinationUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(coordinationRun(os.Stdout, os.Stderr, b, verb, fs.Args(), *reason, *wake, *asJSON, os.Getenv))
}

func coordinationUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("misc", "coordination.usage", "usage: clawdline coordination <status|pause|observed|safe|wake|resumed|retry> [options] [id or target session ids]"))
	os.Exit(2)
}

func coordinationRun(stdout, stderr io.Writer, b *broker, verb string, args []string, reason, wake string, asJSON bool, getenv func(string) string) int {
	const base = "/v1/orchestrator/pauses"
	if verb == "status" {
		if len(args) != 0 {
			return 2
		}
		a, err := b.request(http.MethodGet, base, nil, nil, "")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !a.ok() || asJSON {
			return report(stdout, stderr, "coordination status", a)
		}
		var body struct {
			Pauses []orchestrator.PauseView `json:"pauses"`
			At     int64                    `json:"at"`
		}
		if json.Unmarshal(a.Body, &body) != nil {
			fmt.Fprintln(stderr, cliCopy("misc", "coordination.unreadable", "Coordination status was unreadable."))
			return 1
		}
		tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, cliCopy("misc", "coordination.header", "TARGET\tSTATE\tREASON\tWAKE CONDITION\tERROR"))
		for _, p := range body.Pauses {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Target, p.State, oneLine(p.Reason), oneLine(p.WakeCondition), oneLine(p.DeliveryError+p.WakeError))
		}
		_ = tw.Flush()
		return 0
	}
	conversation, _, err := conversationFromEnv(getenv)
	if err != nil || conversation == "" {
		fmt.Fprintln(stderr, cliCopy("misc", "coordination.identity", "This command needs the caller's conversation ID from its Session environment."))
		return 2
	}
	switch verb {
	case "pause":
		if len(args) == 0 || strings.TrimSpace(reason) == "" || strings.TrimSpace(wake) == "" {
			return 2
		}
		for _, target := range args {
			a, err := b.request(http.MethodPost, base, nil, map[string]any{"request_id": newUUID(),
				"requester_session_id": conversation, "target_session_id": target, "reason": reason, "wake_condition": wake}, "")
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if code := report(stdout, stderr, "coordination pause", a); code != 0 {
				return code
			}
		}
		return 0
	case "observed", "safe", "resumed", "wake", "retry":
		if len(args) != 1 {
			return 2
		}
		field := "session_id"
		a, err := b.request(http.MethodPost, base+"/"+args[0]+"/"+verb, nil, map[string]any{field: conversation}, "")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return report(stdout, stderr, "coordination "+verb, a)
	default:
		return 2
	}
}
