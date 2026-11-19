package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

func sessionBoardCommand(args []string) {
	if len(args) == 0 {
		args = []string{"help"}
	}
	action := args[0]
	fs := flag.NewFlagSet("session-board "+action, flag.ExitOnError)
	id := fs.String("board", "", "message board ID")
	name := fs.String("name", "", "message board name")
	body := fs.String("text", "", "purpose, summary, or message text")
	target := fs.String("target", "", "new lead's member ID")
	after := fs.Int64("after", 0, "read newer messages after this sequence")
	before := fs.Int64("before", 0, "read older messages before this sequence")
	reply := fs.Int64("reply-to", 0, "message sequence to reply to")
	key := fs.String("key", "", "reuse this key to retry a create or post")
	port := fs.Int("port", 0, "daemon port")
	_ = fs.Parse(args[1:])
	switch action {
	case "list", "read", "inbox", "create", "join", "leave", "post", "summary", "transfer", "archive":
	default:
		fmt.Fprintln(os.Stderr, "usage: clawdline session-board <list|read|inbox|create|join|leave|post|summary|transfer|archive> [flags]")
		fs.PrintDefaults()
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		fs.Usage()
		os.Exit(2)
	}
	conversation, _, err := conversationFromEnv(os.Getenv)
	if err != nil {
		fail(err)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	q := url.Values{"conversation_id": {conversation}}
	route := "/v1/orchestrator/session-boards"
	method := http.MethodPost
	var request any = sb.Command{Action: action, Board: *id, Name: *name, Text: *body, Target: *target, ReplyTo: *reply}
	if action == "list" || action == "read" || action == "inbox" {
		method, request = http.MethodGet, nil
		if action == "inbox" {
			route += "/inbox"
		}
		if action != "list" {
			if *id == "" {
				fail(fmt.Errorf("session-board %s requires --board", action))
			}
			q.Set("board", *id)
			if *after != 0 {
				q.Set("after", strconv.FormatInt(*after, 10))
			}
			if *before != 0 {
				q.Set("before", strconv.FormatInt(*before, 10))
			}
		}
	}
	if action == "create" || action == "post" {
		if *key == "" {
			*key = newKey("session-board")
		}
		fmt.Fprintf(os.Stderr, "Idempotency-Key: %s\n", *key)
	}
	a, err := b.request(method, route, q, request, *key)
	if err != nil {
		fail(err)
	}
	os.Exit(report(os.Stdout, os.Stderr, "session-board", a))
}
