// Command clawdline is the whole product: one binary that is both the daemon and
// the command line that talks to it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
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
		if len(os.Args) > 2 && os.Args[2] == "capacity" {
			capacityCommand(os.Args[3:])
			return
		}
		doctor()
	case "dispatch", "land", "settle":
		// The older dispatch skeleton's three commands are gone with it
		// (docs/design-decisions.md D07): they wrote a second table of tasks
		// the broker's claims never saw. Said by name, so a script that still
		// runs one learns where the work went rather than reading a usage line.
		retiredCommand(os.Args[1])
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
	case "board":
		boardCommand(os.Args[2:])
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
	// The log goes where it is bounded before anything else is said.
	daemonLog(cfg)
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
	// The capacity register's beat: every bounded thing measured on the same
	// tick, and health red when evidence has nowhere left to go.
	srv.StartCapacity(context.Background())
	// The board's sweep: a landing closes its item, a delivery waits to be
	// closed, three quiet days send an item back to the Backlog.
	srv.StartWork(context.Background())
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
		// The request queue is the register's `cloud.relay_queue` row.
		QueueDepth: int(httptransport.CapacityLimit(capacity.CloudRelayQueue)),
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
	events, tasks, err := st.Counts(context.Background())
	if err != nil {
		fmt.Printf("store     unreadable: %v\n", err)
		return
	}
	fmt.Printf("store     %d events, %d broker tasks\n", events, tasks)
}

// retiredCommand answers a command this binary no longer has.
func retiredCommand(name string) {
	fmt.Fprintf(os.Stderr, "clawdline %s: retired. Work is dispatched through the broker only — "+
		"POST /v1/orchestrator/tasks, and a schedule's run goes the same way — and a landing is "+
		"recorded with POST /v1/orchestrator/tasks/<id>/landing.\n", name)
	os.Exit(2)
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
	fmt.Fprintln(os.Stderr, "usage: clawdline <serve|doctor|send|interrupt|close|open|pair|cloud|board|version>")
	fmt.Fprintln(os.Stderr, "  doctor capacity --drill audit.security   fill a row on purpose, in a throwaway directory, and see it say so")
	fmt.Fprintln(os.Stderr, "  open [--send] [--print]   sign a browser on this machine in, with a device of its own")
	fmt.Fprintln(os.Stderr, "  pair [--watch]            show the code when a device asks to pair")
	fmt.Fprintln(os.Stderr, "  cloud <status|on|off|login|connect>   the line to app.clawdline.com; off by default")
	fmt.Fprintln(os.Stderr, "  board tracks [--rows] [--json]   the old cards on the three tracks, read-only")
}
