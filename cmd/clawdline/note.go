package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// note create is the Agent's one-way entry to the person's attention panel.
// The daemon resolves both identities and owns every durable row; the CLI
// supplies the calling conversation and reads the machine credential itself.
func noteCommand(args []string) {
	if len(args) == 0 || args[0] != "create" {
		noteUsage()
	}
	fs := flag.NewFlagSet("note create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	target := fs.String("target", "", "the target Session terminal id")
	from := fs.String("from", "", "the live Root conversation id (default: from the environment)")
	file := fs.String("body-file", "", "JSON note body, or - for stdin")
	key := fs.String("key", "", "the Idempotency-Key; reuse after an uncertain result")
	port := fs.Int("port", 0, "the daemon's port")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *target == "" || *file == "" {
		noteUsage()
	}
	var reader io.Reader
	if *file == "-" {
		reader = os.Stdin
	} else {
		opened, err := os.Open(*file)
		if err != nil {
			fail(err)
		}
		defer opened.Close()
		reader = opened
	}
	body, err := io.ReadAll(io.LimitReader(reader, todoInputLimit+1))
	if err != nil {
		fail(err)
	}
	if len(body) > todoInputLimit {
		fail(fmt.Errorf("the note body is too large"))
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(createNote(os.Stdout, os.Stderr, b, *target, *from, body, *key, os.Getenv))
}

func noteUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline note create --target <terminal id> --body-file <JSON path|-> [--from <conversation id>] [--key k] [--port n]")
	fmt.Fprintln(os.Stderr, "  the body names kind, title, summary, action, reason and optional detail, options, document_url")
	os.Exit(2)
}

func createNote(stdout, stderr io.Writer, b *broker, target, from string, raw []byte, key string, getenv func(string) string) int {
	if from == "" {
		for _, name := range conversationEnv {
			if candidate := strings.TrimSpace(getenv(name)); candidate != "" {
				from = candidate
				break
			}
		}
	}
	if from == "" || target == "" {
		fmt.Fprintln(stderr, "clawdline note create: name a live Root conversation and target Session. Nothing was changed.")
		return 2
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		fmt.Fprintln(stderr, "clawdline note create: --body-file must contain one JSON object. Nothing was changed.")
		return 2
	}
	for _, field := range []string{"source_conversation", "source_label", "target_conversation", "target_session", "target_machine"} {
		if _, exists := body[field]; exists {
			fmt.Fprintf(stderr, "clawdline note create: %s is supplied by this command or the daemon. Nothing was changed.\n", field)
			return 2
		}
	}
	fromJSON, _ := json.Marshal(from)
	targetJSON, _ := json.Marshal(target)
	body["source_conversation"] = fromJSON
	body["target_session"] = targetJSON
	if key == "" {
		key = newKey("note")
	}
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", key)
	a, err := b.request(http.MethodPost, "/v1/work/v2/agent/human-interventions", nil, body, key)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline note create:", err)
		fmt.Fprintf(stderr, "To retry the same note: clawdline note create --key %s …\n", key)
		return 1
	}
	return report(stdout, stderr, "note create", a)
}
