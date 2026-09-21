package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/process"
)

// exitPortHeld is the status of a daemon that exits because another process
// holds its port. The macOS shell tells it apart from a daemon that failed
// (shell/darwin/Daemon.swift).
const exitPortHeld = 3

// refuseToServe is how `serve` ends before it has served: the reason goes to
// the daemon's log, which is where somebody looks, and to stderr, which is
// where whoever started it looks. Until 2026-09-21 a failed bind went to stderr
// alone, and the macOS shell's stderr is nobody's.
func refuseToServe(code int, reason string) {
	log.Print(reason)
	// A terminal already has the line, because the log mirrors to it, and so
	// does a stderr the log never left (daemonLog could not open the file).
	if log.Writer() != os.Stderr && !stderrIsTerminal() {
		fmt.Fprintln(os.Stderr, "clawdline:", reason)
	}
	os.Exit(code)
}

// refuseHeldPort ends a daemon that could not bind, naming who holds the port.
func refuseHeldPort(host string, port int, bindErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	sentence, held := heldPortSentence(ctx, host, port, bindErr, process.HolderOf, askPort)
	code := 1
	if held {
		code = exitPortHeld
	}
	refuseToServe(code, sentence)
}

// heldPortSentence says who holds the port a daemon could not bind, and
// whether anything does: held is false when the bind failed for some other
// reason, and then the sentence is that reason.
//
// Three things are asked, each of a different witness: the system (which pid,
// what command, since when), the holder's /v1/health (is it a Clawdline daemon
// at all), and the holder's `/` (does it show a console). The last is the one
// that mattered on 2026-09-21: the holder was a Clawdline daemon, alive and
// green, and served no page.
func heldPortSentence(ctx context.Context, host string, port int, bindErr error,
	holderOf func(context.Context, int) (process.Holder, error),
	ask func(ctx context.Context, url string) (int, []byte, error)) (string, bool) {
	where := fmt.Sprintf("port %d on %s", port, host)
	holder, lookupErr := holderOf(ctx, port)
	inUse := errors.Is(bindErr, syscall.EADDRINUSE)
	if lookupErr != nil && !inUse {
		return fmt.Sprintf("could not listen on %s: %v", where, bindErr), false
	}

	var b strings.Builder
	switch {
	case lookupErr == nil:
		fmt.Fprintf(&b, "%s is already held by pid %d", where, holder.PID)
		if holder.Command != "" {
			fmt.Fprintf(&b, " (%s)", holder.Command)
		}
		if !holder.Started.IsZero() {
			fmt.Fprintf(&b, ", running since %s (%s ago)", holder.Started.Format("2006-01-02 15:04:05 -0700"),
				time.Since(holder.Started).Round(time.Second))
		}
	case errors.Is(lookupErr, process.ErrNoHolder):
		// The system lists nobody and the bind still failed: the holder let go
		// in between, or it is out of this user's sight.
		fmt.Fprintf(&b, "%s is already in use (%v), and the system lists no listener there now", where, bindErr)
	default:
		fmt.Fprintf(&b, "%s is already in use (%v), and the system would not say by whom: %v", where, bindErr, lookupErr)
	}
	b.WriteString(". ")
	b.WriteString(describeHolderAnswers(ctx, host, port, ask))
	b.WriteString(" This daemon has started nothing and is exiting.")
	if lookupErr == nil {
		fmt.Fprintf(&b, " Stop pid %d by its PID if it should not be there, or give this daemon another port with CLAWDLINE_NEXT_PORT.", holder.PID)
	} else {
		b.WriteString(" Give this daemon another port with CLAWDLINE_NEXT_PORT.")
	}
	return b.String(), true
}

// describeHolderAnswers asks the holder its health and its `/`, and says what
// it answered.
func describeHolderAnswers(ctx context.Context, host string, port int, ask func(context.Context, string) (int, []byte, error)) string {
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	base := "http://" + net.JoinHostPort(host, strconv.Itoa(port))

	status, body, err := ask(ctx, base+"/v1/health")
	if err != nil {
		return fmt.Sprintf("It does not answer HTTP there (%v).", err)
	}
	var health struct {
		ServedBy string `json:"served_by"`
	}
	if json.Unmarshal(body, &health) != nil || health.ServedBy == "" {
		return fmt.Sprintf("It is not a Clawdline daemon: /v1/health answered %d without saying what it is.", status)
	}
	who := fmt.Sprintf("It answers /v1/health as %s", health.ServedBy)

	status, body, err = ask(ctx, base+"/")
	switch {
	case err != nil:
		return fmt.Sprintf("%s, and / did not answer (%v).", who, err)
	case status == http.StatusOK:
		return who + ", and serves a console at /."
	}
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &refusal)
	if refusal.Error != "" {
		return fmt.Sprintf("%s, and / answers %d %s: it serves no console.", who, status, refusal.Error)
	}
	return fmt.Sprintf("%s, and / answers %d: it serves no console.", who, status)
}

// askPort is one GET, bounded by ctx, reading at most 64 KiB of the answer.
func askPort(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, body, err
}
