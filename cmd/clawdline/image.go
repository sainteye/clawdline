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

// image send shows the person pictures from a Session. It stores them as
// shown in the calling Session (POST /v1/artifacts/images with `terminal`), so
// the console draws them under the next reply that Session writes. Nothing has
// to be written into the reply: a marker in prose a model kept only as a
// summary never reached the person, and the 200 the upload answered said
// nothing about that.
func imageCommand(args []string) {
	if len(args) == 0 || args[0] != "send" {
		imageUsage()
	}
	fs := flag.NewFlagSet("image send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	target := fs.String("target", "", "the Session terminal id (default: this Session)")
	from := fs.String("from", "", "the conversation id (default: from the environment)")
	port := fs.Int("port", 0, "the daemon's port")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() == 0 {
		imageUsage()
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(sendImages(os.Stdout, os.Stderr, b, *target, *from, fs.Args(), os.Getenv))
}

func imageUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("misc", "image.usage", "usage: clawdline image send [--target <terminal id>] [--from <conversation id>] [--port n] <picture> [<picture>…]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "image.usage_detail", "  put all flags before the first picture path; one to six PNG, JPEG or GIF files appear under the reply that ran this command, or the next one"))
	os.Exit(2)
}

func sendImages(stdout, stderr io.Writer, b *broker, target, from string, paths []string, getenv func(string) string) int {
	if target == "" {
		if from == "" {
			var err error
			if from, _, err = conversationFromEnv(getenv); err != nil {
				fmt.Fprintf(stderr, cliCopy("misc", "image.no_conversation", "clawdline image send: %s Nothing was sent.\n"), conversationRefusal(err, "--from"))
				return 2
			}
		}
		if from == "" {
			fmt.Fprintln(stderr, cliCopy("misc", "image.name_a_session", "clawdline image send: run it inside a Session, or name one with --target. Nothing was sent."))
			return 2
		}
		who, err := b.request(http.MethodGet, "/v1/orchestrator/whoami", url.Values{"conversation_id": {from}}, nil, "")
		if err != nil {
			fmt.Fprintln(stderr, cliCopy("misc", "image.failed", "clawdline image send:"), err)
			return 1
		}
		if !who.ok() {
			return report(stdout, stderr, "image send (whoami)", who)
		}
		var id struct {
			TerminalID string `json:"terminal_id"`
		}
		if json.Unmarshal(who.Body, &id) != nil || id.TerminalID == "" {
			fmt.Fprintln(stderr, cliCopy("misc", "image.whoami_no_terminal", "clawdline image send: whoami answered without a terminal id. Nothing was sent."))
			return 1
		}
		target = id.TerminalID
	}
	images := make([]map[string]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintln(stderr, cliCopy("misc", "image.failed", "clawdline image send:"), err)
			return 2
		}
		images = append(images, map[string]string{"path": abs})
	}
	a, err := b.request(http.MethodPost, "/v1/artifacts/images", nil, map[string]any{"images": images, "terminal": target}, "")
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "image.failed", "clawdline image send:"), err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, "image send", a)
	}
	var out struct {
		Artifacts []struct {
			ID string `json:"id"`
		} `json:"artifacts"`
	}
	_ = json.Unmarshal(a.Body, &out)
	for _, art := range out.Artifacts {
		fmt.Fprintln(stdout, art.ID)
	}
	fmt.Fprintf(stdout, cliCopy("misc", "image.stored_shown", "Stored %d picture(s) for Session %s. They appear under the first reply this Session records after them, usually the message that ran this command; nothing has to be written into it.\n"), len(out.Artifacts), target)
	fmt.Fprintf(stdout, cliCopy("misc", "image.check", "Stored is not seen: GET /v1/transcript?session=%s lists them in that reply's artifacts once it is written.\n"), url.QueryEscape(target))
	return 0
}
