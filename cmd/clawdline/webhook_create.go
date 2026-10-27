package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

func runWebhookCreate(stdout, stderr io.Writer, args []string, open func(int) (*broker, error)) int {
	fs := flag.NewFlagSet("webhook create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("url-file", "", "")
	key := fs.String("key", "", "")
	conversation := fs.String("session", "", "")
	run := fs.String("run", "", "")
	port := fs.Int("port", 0, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *file == "" || *key == "" || *conversation == "" || *run == "" {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_create.usage", "usage: clawdline webhook create --url-file <new private path> --key <key> --session <conversation> --run <run> [--port n]"))
		return webhookExitUsage
	}
	// Reserve the destination before any mutation. O_EXCL refuses existing files
	// and symlinks. No secret goes through stdout, stderr or a durable receipt.
	f, err := os.OpenFile(*file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_create.file", "The URL file could not be created exclusively; nothing was requested."))
		return 1
	}
	saved := false
	defer func() {
		_ = f.Close()
		if !saved {
			_ = os.Remove(*file)
		}
	}()
	b, err := open(*port)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_create.connection", "The daemon could not be opened; nothing was requested."))
		return 1
	}
	a, err := b.request(http.MethodPost, "/v1/orchestrator/schedule-webhooks/create", nil,
		map[string]any{"session_id": *conversation, "via": map[string]any{"run": *run}}, *key)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_create.uncertain", "Creation could not be confirmed. Retry with the same key; do not create a second webhook."))
		return 1
	}
	if !a.ok() {
		code, _ := a.refusal()
		fmt.Fprintf(stderr, "refused status=%d code=%s\n", a.Status, code)
		return 1
	}
	var made schedulewebhook.Created
	if json.Unmarshal(a.Body, &made) != nil || made.Schema != "clawdline.schedule_webhook.management.v1" || made.Hook.ID == "" {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_create.uncertain", "Creation could not be confirmed. Retry with the same key; do not create a second webhook."))
		return 1
	}
	if !made.SecretAvailable || made.PublicURL == "" {
		fmt.Fprintf(stderr, "secret_unavailable hook_id=%s\n", made.Hook.ID)
		return 1
	}
	if _, problem := webhookURL("", func(string) string { return made.PublicURL }); problem != "" {
		fmt.Fprintln(stderr, cliCopy("misc", "webhook_create.uncertain", "Creation could not be confirmed. Retry with the same key; do not create a second webhook."))
		return 1
	}
	if _, err = io.WriteString(f, made.PublicURL+"\n"); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintf(stderr, "secret_save_failed hook_id=%s\n", made.Hook.ID)
		return 1
	}
	saved = true
	fmt.Fprintf(stdout, "hook_id=%s state=%s revision=%d\n", made.Hook.ID, made.Hook.State, made.Hook.Revision)
	return 0
}
