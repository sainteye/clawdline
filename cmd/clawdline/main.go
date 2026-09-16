// Command clawdline is the whole product: one binary that is both the daemon and
// the command line that talks to it.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"

	"github.com/sainteye/clawdline-go/internal/config"
	httptransport "github.com/sainteye/clawdline-go/internal/transport/http"
)

const version = "0.0.1-p0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve()
	case "doctor":
		doctor()
	case "send":
		terminalCommand("send", os.Args[2:])
	case "interrupt":
		terminalCommand("interrupt", os.Args[2:])
	case "close":
		terminalCommand("close", os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func serve() {
	cfg := config.Load()
	srv, err := httptransport.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
}

func doctor() {
	cfg := config.Load()
	fmt.Printf("version   %s\n", version)
	fmt.Printf("port      %d\n", cfg.Port)
	fmt.Printf("upstream  %d\n", cfg.UpstreamPort)
	fmt.Printf("dir       %s\n", cfg.Dir)

	st, err := store.Open(cfg.Dir)
	if err != nil {
		fmt.Printf("store     unreadable: %v\n", err)
		return
	}
	defer st.Close()
	events, receipts, open, err := st.Counts(context.Background())
	if err != nil {
		fmt.Printf("store     unreadable: %v\n", err)
		return
	}
	fmt.Printf("store     %d events, %d receipts, %d obligations open\n", events, receipts, open)
}

// terminalCommand drives one session from the command line. It is the same
// port the daemon uses, so a thing proved here is proved for both.
func terminalCommand(op string, args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: clawdline %s <session-id> [text]\n", op)
		os.Exit(2)
	}
	target := session.Session{ID: args[0], Backend: session.BackendTmux}
	if !strings.HasPrefix(args[0], "%") {
		target.Backend = session.BackendITerm
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var host ports.TerminalHost
	for _, h := range terminal.Hosts() {
		if (target.Backend == session.BackendTmux) == (h.Name() == "tmux") {
			host = h
			break
		}
	}
	if host == nil {
		fmt.Fprintln(os.Stderr, "clawdline: no backend for", target.Backend)
		os.Exit(1)
	}

	var err error
	switch op {
	case "send":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "clawdline send needs text")
			os.Exit(2)
		}
		err = host.Send(ctx, target, strings.Join(args[1:], " "))
	case "interrupt":
		err = host.Interrupt(ctx, target)
	case "close":
		err = host.Close(ctx, target)
	}
	if err != nil {
		// A refusal is reported as itself. Saying "sent" after a failure is the
		// one answer that leaves a caller worse off than saying nothing.
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: delivered to %s\n", op, target.ID)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline <serve|doctor|send|interrupt|close|version>")
}
