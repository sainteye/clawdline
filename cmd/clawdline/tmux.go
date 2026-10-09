package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
)

// tmuxCommand is `clawdline tmux <args…>`: the tmux this daemon runs, on the
// server it runs it on (terminal.ResolveTmux). On a machine with its own tmux
// that is just that tmux; on one without, it is the tmux the release carries,
// pointed at Clawdline's own socket — so `clawdline tmux new -s work` and
// `clawdline tmux attach` reach the sessions the daemon lists, where a bare
// `tmux` would not exist at all.
//
// `clawdline tmux which` says which one, and from where, instead of running it.
func tmuxCommand(args []string) {
	choice := terminal.ResolveTmux(context.Background())
	if len(args) == 1 && args[0] == "which" {
		fmt.Println(choice.Describe())
		if choice.Passed != "" {
			fmt.Printf(cliCopy("misc", "tmux.passed", "  (%s)\n"), choice.Passed)
		}
		if !choice.Found() {
			os.Exit(1)
		}
		return
	}
	if !choice.Found() {
		fmt.Fprintln(os.Stderr, cliCopy("misc", "tmux.none", "clawdline tmux: there is no tmux on this machine, and this build carries none"))
		os.Exit(1)
	}
	cmd := exec.Command(choice.Path, choice.Args(args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "clawdline tmux: %v\n", err)
		os.Exit(1)
	}
}
