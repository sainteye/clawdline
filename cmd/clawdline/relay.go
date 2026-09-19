package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// The thin commands besides the turn report: what each assistant's account
// has left, every landing still owed, a message to another session and a
// notification to the person. Each prints the daemon's JSON as it came.

func readCommand(name, path string, args []string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "usage: clawdline %s [--port n]\n", name)
		os.Exit(2)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	a, err := b.request(http.MethodGet, path, nil, nil, "")
	if err != nil {
		fail(err)
	}
	os.Exit(report(os.Stdout, os.Stderr, name, a))
}

// relayInputLimit bounds what is read from stdin: the daemon's own body cap.
// How many characters a message may have is the daemon's rule (100,000), and
// its refusal says so; it is not spelled a second time here.
const relayInputLimit = 2 << 20

// sendCommand is POST /v1/orchestrator/messages: the text typed into another
// session's composer by the daemon, which checks first that the other side
// is a composer and not a dialog, and keeps a receipt under the
// Idempotency-Key. Sent twice with the same key, it is typed once.
func sendCommand(args []string) {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	to := fs.String("to", "", "the recipient's terminal id (from `clawdline sessions`… see the guide)")
	from := fs.String("from", "", "this session's terminal or conversation id (default: from the environment)")
	key := fs.String("key", "", "the Idempotency-Key; reuse the one printed by a failed attempt to retry it")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil {
		sendUsage()
	}
	text := strings.Join(fs.Args(), " ")
	if text == "" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, relayInputLimit+1))
		if err != nil {
			fail(err)
		}
		if len(data) > relayInputLimit {
			fail(fmt.Errorf("the message on stdin is larger than %d bytes, the most the daemon reads", relayInputLimit))
		}
		text = string(data)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(relayMessage(os.Stdout, os.Stderr, b, *to, *from, text, *key, os.Getenv))
}

func sendUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline send --to <terminal id> [--from id] [--key k] [--port n] [text…]")
	fmt.Fprintln(os.Stderr, "  relays text into another session's composer; with no text arguments it is read from stdin")
	os.Exit(2)
}

// relayMessage is the command, answering its exit status.
func relayMessage(stdout, stderr io.Writer, b *broker, to, from, text, key string, getenv func(string) string) int {
	if strings.TrimSpace(to) == "" {
		fmt.Fprintln(stderr, "clawdline send: --to is required: the recipient's terminal id")
		return 2
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(stderr, "clawdline send: there is no text to send")
		return 2
	}
	if from == "" {
		for _, name := range conversationEnv {
			if v := strings.TrimSpace(getenv(name)); v != "" {
				from = v
				break
			}
		}
	}
	if from == "" {
		fmt.Fprintf(stderr, "clawdline send: cannot tell who is sending: none of %s is set. Pass --from.\n",
			strings.Join(conversationEnv, ", "))
		return 2
	}
	if key == "" {
		key = newKey("send")
	}
	// Said before the request, so that an attempt that dies on the way can
	// be retried as the same message rather than typed a second time.
	fmt.Fprintf(stderr, "Idempotency-Key: %s\n", key)
	a, err := b.request(http.MethodPost, "/v1/orchestrator/messages", nil,
		map[string]string{"from_session": from, "to_session": to, "text": text}, key)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline send:", err)
		fmt.Fprintf(stderr, "To retry the same message: clawdline send --key %s …\n", key)
		return 1
	}
	return report(stdout, stderr, "send", a)
}

// notifyCommand is POST /v1/orchestrator/notify: a push to the person's
// subscribed devices, for something they are waiting on.
func notifyCommand(args []string) {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	title := fs.String("title", "", "1–80 characters")
	body := fs.String("body", "", "1–500 characters")
	session := fs.String("session", "", "the terminal id the notification opens (optional)")
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *title == "" || *body == "" {
		fmt.Fprintln(os.Stderr, "usage: clawdline notify --title <1–80 characters> --body <1–500 characters> [--session terminal] [--port n]")
		os.Exit(2)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	req := map[string]string{"title": *title, "body": *body}
	if *session != "" {
		req["session_id"] = *session
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/notify", nil, req, "")
	if err != nil {
		fail(err)
	}
	os.Exit(report(os.Stdout, os.Stderr, "notify", a))
}
