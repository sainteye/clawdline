//go:build windows

package process

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// HolderOf asks the system which process listens on a TCP port: `netstat
// -ano` for the pid and `tasklist` for its image name. Windows is not asked
// when the process started, so Started stays zero and the sentence built on
// it says so.
func HolderOf(ctx context.Context, port int) (Holder, error) {
	out, err := exec.CommandContext(ctx, "netstat", "-ano", "-p", "TCP").Output()
	if err != nil {
		return Holder{}, fmt.Errorf("netstat: %w", err)
	}
	pid, ok := parseNetstatListener(string(out), port)
	if !ok {
		return Holder{}, ErrNoHolder
	}
	h := Holder{PID: pid}
	list, err := exec.CommandContext(ctx, "tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	if err == nil {
		if row, err := csv.NewReader(strings.NewReader(string(list))).Read(); err == nil && len(row) > 0 {
			h.Command = row[0]
		}
	}
	return h, nil
}
