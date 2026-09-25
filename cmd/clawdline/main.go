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
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"

	"github.com/sainteye/clawdline/internal/config"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// version is what this build honestly is.
//
// A binary from `go install github.com/sainteye/clawdline/cmd/clawdline@vX.Y.Z`
// carries that tag in its build info, and that is the number its user will
// quote and the one Clawdline Cloud is told (`cloud.go`'s StartLogin). A
// hard-coded constant would have every released binary claim the same number
// forever: on 2026-09-21 the repository was published and tagged v0.9.0 while
// every copy of it said `0.0.1-p0`.
//
// A build from a checkout has no module version. It says so, with the revision
// it was built from when Go recorded one, rather than borrowing a release
// number it is not.
var version = buildVersion()

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "devel+" + s.Value[:7]
		}
	}
	return "devel"
}

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
	case "dispatch":
		// The broker's own dispatch, guide §4 in one command. The older
		// skeleton's `dispatch` wrote a second table of tasks; this one writes
		// nothing the broker does not read.
		dispatchCommand(os.Args[2:])
	case "land", "settle":
		// The older dispatch skeleton's other two commands are gone with it
		// (docs/design-decisions.md D07): they wrote a second table of tasks
		// the broker's claims never saw. Said by name, so a script that still
		// runs one learns where the work went rather than reading a usage line.
		retiredCommand(os.Args[1])
	case "type":
		// Typing straight into a terminal, with nothing recorded: a tool for
		// proving a terminal backend. `send` is the relay a session uses.
		terminalCommand("send", os.Args[2:])
	case "send":
		sendCommand(os.Args[2:])
	case "notify":
		notifyCommand(os.Args[2:])
	case "session":
		sessionCommand(os.Args[2:])
	case "todo":
		todoCommand(os.Args[2:])
	case "item":
		itemCommand(os.Args[2:])
	case "landings":
		readCommand("landings", "/v1/orchestrator/landings", os.Args[2:])
	case "assistants":
		readCommand("assistants", "/v1/orchestrator/assistants", os.Args[2:])
	case "guide":
		guideCommand(os.Args[2:])
	case "skill":
		skillCommand(os.Args[2:])
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
	case "tunnel":
		tunnelCommand(os.Args[2:])
	case "board":
		boardCommand(os.Args[2:])
	case "project":
		projectCommand(os.Args[2:])
	case "task":
		taskCommand(os.Args[2:])
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
	// The port before anything else is done. Everything below writes: the
	// stable binary, the store, the broker's beat, the cloud line, the tunnel's
	// Reclaim — which stops the cloudflared this state directory's pid file
	// names, whoever started it. A daemon that loses the port to another one
	// over the same directory must not have done any of that first.
	ln, err := httptransport.Listen(cfg)
	if err != nil {
		refuseHeldPort(cfg.Host, cfg.Port, err)
	}
	// The copy of this binary the skill stub runs (skillfile.ProjectBinary).
	// Off the startup path, and never fatal: without it a session cannot read
	// the guide, which is worth a log line, not a daemon that will not start.
	go projectBinary(cfg.Dir)
	srv, err := httptransport.New(cfg)
	if err != nil {
		refuseToServe(1, err.Error())
	}
	// A device file that cannot be read stops the daemon here, rather than
	// letting it listen and refuse everybody.
	if err := srv.AuthReady(); err != nil {
		refuseToServe(1, err.Error())
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
	// The token ledger: transcripts read, a bounded pass a minute, into what
	// each session spent (docs/token-ledger.md).
	srv.StartUsage(context.Background())
	startCloudLine(context.Background(), cfg, srv)
	// The account-free tunnel, if the settings ask for one and something is
	// paired: a cloudflared an earlier run left behind is stopped first.
	srv.StartTunnel()
	stopTunnelOnSignal(srv)
	if err := srv.Serve(ln); err != nil {
		refuseToServe(1, err.Error())
	}
}

// stopTunnelOnSignal takes cloudflared down before the daemon goes, then lets
// the signal do what it would have done. Without this an interrupt or a
// SIGTERM ends the daemon at once and leaves its tunnel up: a public address
// that nothing on screen admits to. A SIGKILL cannot be caught, which is what
// the tunnel's pid file and Reclaim are for.
func stopTunnelOnSignal(srv *httptransport.Server) {
	caught := make(chan os.Signal, 1)
	signal.Notify(caught, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-caught
		srv.StopTunnel()
		signal.Reset(os.Interrupt, syscall.SIGTERM)
		if p, err := os.FindProcess(os.Getpid()); err == nil && p.Signal(sig) == nil {
			// Give the re-raised signal a moment to land; the exit status is
			// then the one the sender expected.
			time.Sleep(time.Second)
		}
		os.Exit(1)
	}()
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
		// The outbound spool is its four rows: two global, one per wire
		// channel, and the receipt table.
		SpoolRows:         int(httptransport.CapacityLimit(capacity.CloudSpool)),
		SpoolBytes:        int(httptransport.CapacityLimit(capacity.CloudSpoolBytes)),
		SpoolChannelBytes: int(httptransport.CapacityLimit(capacity.CloudSpoolChannelBytes)),
		SpoolReceipts:     int(httptransport.CapacityLimit(capacity.CloudSpoolReceipts)),
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
	if identity, webhooks, ok := link.ScheduleWebhooks(); ok {
		srv.StartScheduleWebhooks(ctx, identity, webhooks, version)
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
	// "0" would read like a port. Nobody behind this daemon is the ordinary
	// answer and it is said in words (config.NoUpstream).
	if port, ok := cfg.Upstream(); ok {
		fmt.Printf("upstream  %d\n", port)
	} else {
		fmt.Printf("upstream  none (an unowned route answers 501 not_implemented; %s asks for one)\n",
			config.UpstreamPortEnv)
	}
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
		name := op
		if op == "send" {
			name = "type"
		}
		fmt.Fprintf(os.Stderr, "usage: clawdline %s <session-id> [text]\n", name)
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
			fmt.Fprintln(os.Stderr, "clawdline type needs text")
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
	fmt.Fprintln(os.Stderr, "usage: clawdline <serve|doctor|guide|skill|session|dispatch|todo|item|send|notify|landings|assistants|type|interrupt|close|open|pair|tunnel|cloud|board|project|task|version>")
	fmt.Fprintln(os.Stderr, "  guide [topic] | guide -list   the agent guide this build carries; no daemon needed")
	fmt.Fprintln(os.Stderr, "  skill <install|uninstall>     put this build's skill stub in ~/.claude/skills/clawdline, or put back what was there")
	fmt.Fprintln(os.Stderr, "  session report --summary <sentence>   record this session's finished turn: delivered, awaiting approval")
	fmt.Fprintln(os.Stderr, "  dispatch --title <t> --claims a,b < brief   dispatch an owned child: task.json, inventory and POST in one step")
	fmt.Fprintln(os.Stderr, "  todo <add|list|done>          this session's own to-dos, added only when the person asks")
	fmt.Fprintln(os.Stderr, "  item <add|steps|step-done>    a Board item the person's message asked for, with its --step rows")
	fmt.Fprintln(os.Stderr, "  send --to <terminal> [text…]  relay a message into another session's composer")
	fmt.Fprintln(os.Stderr, "  notify --title <t> --body <b>   push a notification to the person")
	fmt.Fprintln(os.Stderr, "  landings | assistants         every landing still owed; what each assistant's account has left")
	fmt.Fprintln(os.Stderr, "  type <session-id> <text>      type straight into a terminal, recording nothing (for testing a backend)")
	fmt.Fprintln(os.Stderr, "  doctor capacity --drill audit.security   fill a row on purpose, in a throwaway directory, and see it say so")
	fmt.Fprintln(os.Stderr, "  open [--send] [--print]   sign a browser on this machine in, with a device of its own")
	fmt.Fprintln(os.Stderr, "  pair [--watch]            show the code when a device asks to pair")
	fmt.Fprintln(os.Stderr, "  tunnel [--json]           what the cloudflared tunnel is doing; remote_tunnel in the settings turns it on")
	fmt.Fprintln(os.Stderr, "  cloud <status|on|off|login|connect>   the line to app.clawdline.com; off by default")
	fmt.Fprintln(os.Stderr, "  board tracks [--rows] [--json]   the old cards on the three tracks, read-only")
	fmt.Fprintln(os.Stderr, "  project <add|remove|list>    explicitly keep directories in the session-start list")
	fmt.Fprintln(os.Stderr, "  task finish [--port n] <task dir>   a child's result, validated and put in place; no node needed")
	fmt.Fprintln(os.Stderr, "  task ack <task id> <notice id>      acknowledge a child's completion notice, so it stops being typed")
}
