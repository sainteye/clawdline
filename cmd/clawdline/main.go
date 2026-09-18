// Command clawdline is the whole product: one binary that is both the daemon and
// the command line that talks to it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/domain/task"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"

	"github.com/sainteye/clawdline-go/internal/config"
	cloudtransport "github.com/sainteye/clawdline-go/internal/transport/cloud"
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
	case "land":
		landCommand(os.Args[2:])
	case "settle":
		settleCommand(os.Args[2:])
	case "send":
		terminalCommand("send", os.Args[2:])
	case "interrupt":
		terminalCommand("interrupt", os.Args[2:])
	case "close":
		terminalCommand("close", os.Args[2:])
	case "open":
		openCommand(os.Args[2:])
	case "pair":
		pairCommand(os.Args[2:])
	case "cloud":
		cloudCommand(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func serve() {
	cfg := config.Load()
	port, err := daemonPort()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(2)
	}
	cfg.Port = port
	srv, err := httptransport.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	// A device file that cannot be read stops the daemon here, rather than
	// letting it listen and refuse everybody.
	if err := srv.AuthReady(); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	srv.StartScheduler(context.Background())
	// The broker's beat: collect what children wrote, run the two clocks, and
	// keep telling a root its child finished until somebody acknowledges it.
	srv.StartBroker(context.Background())
	startCloudLine(context.Background(), cfg, srv)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
}

// startCloudLine brings up the line to app.clawdline.com, if the settings say
// so.
//
// **Off by default, and a failure here never stops the daemon.** The free
// product does not depend on Cloud (`docs/remote.md` design principle 1), so a
// settings file with a typo in its relay URL, a control plane that is down, or
// a machine that was never enrolled must all leave a working local daemon
// behind. Each of those is recorded where `/v1/cloud/status` can say it, which
// is the difference between a quiet failure and a legible one.
func startCloudLine(ctx context.Context, cfg config.Config, srv *httptransport.Server) {
	link, err := cloudtransport.Open(cloudtransport.LinkOptions{
		Dir:         cfg.Dir,
		ForeignDirs: foreignDirs(),
		// The daemon's own routes, gate and all. A Cloud request is answered by
		// exactly the handler a paired browser on this machine's own network
		// reaches — one set of permission checks, not two.
		Handler: srv.Handler(),
		Authorize: func(r *http.Request) {
			local, machine, err := srv.CloudCredentials()
			if err != nil {
				// No credential is the right answer for a daemon that cannot
				// read its own device store: the gate then refuses, which is
				// what it should do.
				return
			}
			cloudtransport.LocalAuthorizer(local, machine)(r)
		},
		Version: version,
		Log:     func(format string, args ...any) { log.Printf(format, args...) },
	})
	if err != nil {
		// A malformed cloud setting is loud and is not fatal. Falling back to
		// the production relay because somebody mistyped a local one is the
		// failure worth refusing.
		log.Printf("cloud: the line is off: %v", err)
		return
	}
	httptransport.SetCloudLine(cfg.Dir, link)
	if !link.Enabled() {
		return
	}
	go func() {
		if err := link.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("cloud: the line stopped: %v", err)
		}
	}()
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
	case "land":
		landCommand(os.Args[2:])
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

func landCommand(args []string) {
	// The id comes first because that reads naturally, and Go's flag package
	// stops parsing at the first non-flag argument. Taking it off the front is
	// the whole fix; leaving it there silently emptied every flag.
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: clawdline land <task-id> --commit <sha> [--repo dir] [--branch name]")
		os.Exit(2)
	}
	id := args[0]
	fs := flag.NewFlagSet("land", flag.ExitOnError)
	repo := fs.String("repo", ".", "the repository the work was done in")
	commit := fs.String("commit", "", "the commit that carries the work")
	branch := fs.String("branch", "main", "the target branch")
	_ = fs.Parse(args[1:])
	cfg := config.Load()
	st, err := store.Open(cfg.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	defer st.Close()
	l := app.Lander{Store: st, Git: git.New()}
	if err := l.Land(context.Background(), id, *repo, *commit, *branch); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("landed   %s on %s\n", *commit, *branch)
}

func newTaskID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline <serve|doctor|dispatch|settle|land|send|interrupt|close|open|pair|cloud|version>")
	fmt.Fprintln(os.Stderr, "  open [--send] [--print]   sign a browser on this machine in, with a device of its own")
	fmt.Fprintln(os.Stderr, "  pair [--watch]            show the code when a device asks to pair")
	fmt.Fprintln(os.Stderr, "  cloud <status|on|off|login|connect>   the line to app.clawdline.com; off by default")
}
