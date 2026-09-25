package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// `clawdline todo`: a Session's own quick to-dos, the list the person sees
// under that Session. `add` is for when the person asked the Session to track
// its work there — never on the Session's own initiative — and each row is
// shown to the person as added by the Session. `list` is the Agent's read of
// the same list, and `done` completes one of its rows. Sending and deleting a
// row stay the person's; this command has neither.
//
// The conversation is found the way `session report` finds it.

func todoCommand(args []string) {
	if len(args) == 0 {
		todoUsage()
	}
	op := args[0]
	fs := flag.NewFlagSet("todo "+op, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	conversation := fs.String("conversation", "", "this assistant's conversation id (default: from the environment)")
	key := fs.String("key", "", "the Idempotency-Key; reuse the one printed by a failed attempt to retry it")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args[1:]); err != nil {
		todoUsage()
	}
	var texts []string
	switch op {
	case "add":
		texts = fs.Args()
		if len(texts) == 0 {
			lines, err := todoLines(os.Stdin)
			if err != nil {
				fail(err)
			}
			texts = lines
		}
	case "list":
		if fs.NArg() != 0 {
			todoUsage()
		}
	case "done":
		if fs.NArg() != 1 {
			todoUsage()
		}
		texts = fs.Args()
	default:
		todoUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(sessionTodo(os.Stdout, os.Stderr, b, op, texts, *conversation, *key, os.Getenv))
}

func todoUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline todo add [--conversation id] [--key k] [--port n] <text> [text…]")
	fmt.Fprintln(os.Stderr, "       clawdline todo list [--conversation id] [--port n]")
	fmt.Fprintln(os.Stderr, "       clawdline todo done [--conversation id] [--key k] [--port n] <to-do id>")
	fmt.Fprintln(os.Stderr, "  add writes this Session's own to-dos, one per argument or one per non-empty stdin line,")
	fmt.Fprintln(os.Stderr, "  and only when the person asked for them; all are added or none are")
	os.Exit(2)
}

// todoInputLimit bounds what `todo add` reads from stdin: the daemon's own
// work-system body cap. How many rows one call may carry is the daemon's rule,
// and its refusal says so.
const todoInputLimit = 96 << 10

// todoLines is one row per non-empty line.
func todoLines(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, todoInputLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > todoInputLimit {
		return nil, fmt.Errorf("the to-dos on stdin are larger than %d bytes, the most the daemon reads", todoInputLimit)
	}
	var out []string
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64<<10), todoInputLimit+1)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

// sessionTodo is the command, answering its exit status.
func sessionTodo(stdout, stderr io.Writer, b *broker, op string, args []string, conversation, key string,
	getenv func(string) string) int {
	name := "todo " + op
	if conversation == "" {
		for _, env := range conversationEnv {
			if v := strings.TrimSpace(getenv(env)); v != "" {
				conversation = v
				break
			}
		}
	}
	if conversation == "" {
		fmt.Fprintf(stderr, "clawdline %s: cannot tell which conversation this is: none of %s is set. "+
			"Pass --conversation <this assistant's conversation id>. Nothing was changed.\n",
			name, strings.Join(conversationEnv, ", "))
		return 2
	}
	base := "/v1/work/v2/agent/session-todos/" + url.PathEscape(conversation)
	var method, path string
	var body any
	switch op {
	case "list":
		a, err := b.request(http.MethodGet, base, nil, nil, "")
		if err != nil {
			fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
			return 1
		}
		return report(stdout, stderr, name, a)
	case "add":
		rows := make([]map[string]string, 0, len(args))
		for _, text := range args {
			if strings.TrimSpace(text) != "" {
				rows = append(rows, map[string]string{"text": text})
			}
		}
		if len(rows) == 0 {
			fmt.Fprintf(stderr, "clawdline %s: there is no to-do to add\n", name)
			return 2
		}
		method, path, body = http.MethodPost, base, map[string]any{"todos": rows}
	case "done":
		id := strings.TrimSpace(args[0])
		if id == "" {
			fmt.Fprintf(stderr, "clawdline %s: name the to-do id to complete\n", name)
			return 2
		}
		method, path, body = http.MethodPost, base+"/"+url.PathEscape(id)+"/complete", map[string]any{}
	default:
		fmt.Fprintf(stderr, "clawdline todo: no such action %q\n", op)
		return 2
	}
	if key == "" {
		key = newKey("todo")
	}
	// Said before the request, so that an attempt that dies on the way can
	// be retried as the same write rather than made a second time.
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", key)
	a, err := b.request(method, path, nil, body, key)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		fmt.Fprintf(stderr, "To retry the same write: clawdline %s --key %s …\n", name, key)
		return 1
	}
	return report(stdout, stderr, name, a)
}
