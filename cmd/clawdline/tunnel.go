package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// tunnelCommand prints what the daemon's tunnel is doing, from GET /v1/tunnel
// with this machine's own token. It changes nothing: the tunnel is turned on
// and off by `remote_tunnel` in the settings, as the Remote tab does, and a
// terminal on a machine with no settings window reads it here.
func tunnelCommand(args []string) {
	fs := flag.NewFlagSet("tunnel", flag.ExitOnError)
	asJSON := fs.Bool("json", false, cliCopy("misc", "tunnel.json_help", "print the daemon's answer as it came"))
	_ = fs.Parse(args)
	req, err := daemonRequest(http.MethodGet, "/v1/tunnel", nil)
	if err != nil {
		fail(err)
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		fail(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fail(fmt.Errorf(cliCopy("misc", "tunnel.daemon_refused", "the daemon said no: %s"), refusalText(res)))
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		fail(err)
	}
	if *asJSON {
		_, _ = os.Stdout.Write(data)
		return
	}
	var st contract.TunnelStatus
	if err := json.Unmarshal(data, &st); err != nil {
		fail(fmt.Errorf(cliCopy("misc", "tunnel.answer_unreadable", "the daemon's answer was not a tunnel status: %w"), err))
	}
	printTunnelStatus(os.Stdout, st)
}

// printTunnelStatus localizes the table labels and keeps daemon values as
// received. The --json path above never uses this renderer.
func printTunnelStatus(w io.Writer, st contract.TunnelStatus) {
	fmt.Fprintf(w, cliCopy("misc", "tunnel.state", "state     %s\n"), st.State)
	fmt.Fprintf(w, cliCopy("misc", "tunnel.mode", "mode      %s\n"), st.Mode)
	if st.URL != "" {
		fmt.Fprintf(w, cliCopy("misc", "tunnel.address", "address   %s\n"), st.URL)
	}
	if st.Reason != "" {
		fmt.Fprintf(w, cliCopy("misc", "tunnel.reason", "reason    %s\n"), st.Reason)
	}
	if st.Attempts > 0 {
		fmt.Fprintf(w, cliCopy("misc", "tunnel.attempts", "attempts  %d\n"), st.Attempts)
	}
	if st.Installed {
		fmt.Fprintf(w, cliCopy("misc", "tunnel.binary", "binary    %s\n"), st.Binary)
	} else {
		fmt.Fprintln(w, cliCopy("misc", "tunnel.not_installed", "binary    cloudflared is not installed"))
	}
	fmt.Fprintf(w, cliCopy("misc", "tunnel.config", "config    %s\n"), st.Config)
	if len(st.Command) > 0 {
		fmt.Fprintf(w, cliCopy("misc", "tunnel.command", "command   %s\n"), strings.Join(st.Command, " "))
	}
}
