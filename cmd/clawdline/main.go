// Command clawdline is the whole product: one binary that is both the daemon and
// the command line that talks to it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/domain/task"

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
	case "dispatch":
		dispatchCommand(os.Args[2:])
	case "settle":
		settleCommand(os.Args[2:])
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
	case "dispatch":
		dispatchCommand(os.Args[2:])
	case "settle":
		settleCommand(os.Args[2:])
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

// dispatcher wires the pieces the command line needs.
func dispatcher() (app.Dispatcher, func()) {
	cfg := config.Load()
	st, err := store.Open(cfg.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	return app.Dispatcher{
		Store:    st,
		Tasks:    taskdir.New(cfg.Dir),
		Terminal: terminal.NewTmux(),
	}, func() { st.Close() }
}

func dispatchCommand(args []string) {
	fs := flag.NewFlagSet("dispatch", flag.ExitOnError)
	assistant := fs.String("assistant", "", "claude or codex")
	dir := fs.String("dir", "", "the project directory")
	brief := fs.String("brief", "", "what the task is")
	claims := fs.String("claims", "", "comma-separated paths this task will write")
	command := fs.String("command", "", "the command to run; defaults to the assistant's own name")
	_ = fs.Parse(args)

	// Not passing the flag and passing it empty are different answers, and the
	// domain refuses the first while accepting the second. Building a non-nil
	// empty slice for both would quietly turn "I did not say" into "I say
	// none" — the exact distinction claims exist to carry.
	var declared []string
	fs.Visit(func(f *flag.Flag) {
		if f.Name != "claims" {
			return
		}
		declared = []string{}
		if *claims != "" {
			declared = strings.Split(*claims, ",")
		}
	})
	t := task.Task{
		ID:         newTaskID(),
		Assistant:  task.Assistant(*assistant),
		ProjectDir: *dir,
		Brief:      *brief,
		Claims:     declared,
	}
	run := *command
	if run == "" {
		run = string(t.Assistant)
	}

	d, done := dispatcher()
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	receipt, path, err := d.Dispatch(ctx, t, run)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("task     %s\n", t.ID)
	fmt.Printf("dir      %s\n", path)
	fmt.Printf("receipt  seq=%d replayed=%v\n", receipt.Seq, receipt.Replayed)
}

func settleCommand(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: clawdline settle <task-id>")
		os.Exit(2)
	}
	d, done := dispatcher()
	defer done()
	state, finished, err := d.Settle(context.Background(), args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if !finished {
		// Not answering yet and never answering look the same here, and saying
		// so is more useful than picking one.
		fmt.Println("no result yet")
		return
	}
	fmt.Printf("settled  %s\n", state)
}

func newTaskID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline <serve|doctor|dispatch|settle|send|interrupt|close|version>")
}
