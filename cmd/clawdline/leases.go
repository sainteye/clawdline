package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline leases`: GET /v1/orchestrator/leases, read-only. Every lease
// the daemon keeps — the heavy compile slot and each checkout's landing —
// with who holds it and who waits behind. An answer that cannot be read is
// an error, never an empty table: no rows would read as "nothing is held".
func leasesCommand(args []string) {
	fs := flag.NewFlagSet("leases", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 0, "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)")
	asJSON := fs.Bool("json", false, "print the daemon's answer as it is")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: clawdline leases [--json] [--port n]")
		os.Exit(2)
	}
	b, err := openBroker(*port)
	if err != nil {
		fail(err)
	}
	os.Exit(leasesRun(os.Stdout, os.Stderr, b, *asJSON))
}

func leasesRun(stdout, stderr io.Writer, b *broker, asJSON bool) int {
	const name = "leases"
	a, err := b.request(http.MethodGet, "/v1/orchestrator/leases", nil, nil, "")
	if err != nil {
		fmt.Fprintf(stderr, "clawdline %s: %v\n", name, err)
		return 1
	}
	if !a.ok() {
		return report(stdout, stderr, name, a)
	}
	var list struct {
		Leases *[]contract.LeaseRecord `json:"leases"`
	}
	if json.Unmarshal(a.Body, &list) != nil || list.Leases == nil {
		fmt.Fprintf(stderr, "clawdline %s: the daemon's answer is not a lease list; the leases are unknown: %s\n",
			name, clip(string(a.Body), 200))
		return 1
	}
	if asJSON {
		return report(stdout, stderr, name, a)
	}
	if len(*list.Leases) == 0 {
		fmt.Fprintln(stdout, "No lease is held or waited for.")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "RESOURCE\tKEY\tHOLDER\tLIVENESS\tHELD\tREASON\tWAITING")
	for _, l := range *list.Leases {
		holder, liveness, held, reason := "-", "-", "-", "-"
		if h := l.Holder; h != nil {
			holder, liveness, held, reason = h.Holder, string(h.Liveness), fmt.Sprintf("%ds", h.HeldSeconds), h.Reason
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n", l.Resource, l.Key, holder, liveness, held,
			oneLine(reason), l.QueueDepth)
	}
	_ = tw.Flush()
	for _, l := range *list.Leases {
		for _, q := range l.Queue {
			fmt.Fprintf(stdout, "  waiting for %s %s: #%d %s, %ds: %s\n", l.Resource, l.Key, q.Position, q.Holder,
				q.WaitedSeconds, oneLine(q.Reason))
		}
	}
	return 0
}

// oneLine keeps a free-text cell on its row of the table.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "-"
	}
	return clip(s, 80)
}
