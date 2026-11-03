package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

func outputCommand(args []string) {
	fs := flag.NewFlagSet("output add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	target := fs.String("target", "", "Session terminal id")
	from := fs.String("from", "", "conversation id")
	port := fs.Int("port", 0, "daemon port")
	kind := fs.String("kind", "published", "created, modified or published")
	if len(args) < 1 || args[0] != "add" || fs.Parse(args[1:]) != nil || fs.NArg() != 1 || (*kind != "created" && *kind != "modified" && *kind != "published") {
		fmt.Fprintln(os.Stderr, cliCopy("misc", "output.usage", "usage: clawdline output add [--target terminal] [--from conversation] [--port n] [--kind created|modified|published] <file>"))
		os.Exit(2)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	if *target == "" {
		if *from == "" {
			*from, _, err = conversationFromEnv(os.Getenv)
			if err != nil {
				fail(err)
			}
		}
		who, e := b.request(http.MethodGet, "/v1/orchestrator/whoami", url.Values{"conversation_id": {*from}}, nil, "")
		if e != nil {
			fail(e)
		}
		if !who.ok() {
			os.Exit(report(os.Stdout, os.Stderr, "output add (whoami)", who))
		}
		var identity struct {
			TerminalID string `json:"terminal_id"`
		}
		if json.Unmarshal(who.Body, &identity) != nil || identity.TerminalID == "" {
			fail(fmt.Errorf("%s", cliCopy("misc", "output.identity_missing", "The Session terminal identity is unavailable.")))
		}
		*target = identity.TerminalID
	}
	path, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		fail(err)
	}
	answer, err := b.request(http.MethodPost, "/v1/sessions/"+url.PathEscape(*target)+"/outputs", nil, map[string]string{"path": path, "kind": *kind}, "")
	if err != nil {
		fail(err)
	}
	os.Exit(report(os.Stdout, os.Stderr, "output add", answer))
}
