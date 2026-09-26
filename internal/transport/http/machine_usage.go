package http

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/machineusage"
	"github.com/sainteye/clawdline/internal/contract"
)

// machineUsageRoute is GET /v1/machine/usage: the dashboard the session counts
// open (web/console/src/machine/). This machine's CPU and memory now, and each
// session's share, summed over the tree of processes the session started.
//
// It is the terminal work of 2026-09-26 made readable: everything in the
// console was slow, and telling a slow daemon from a machine out of memory took
// `vmstat`, `ps` and the sysstat history. The answer then was nine sessions and
// a Go build on two cores and 1.9 GB, which this route shows at a glance.
//
// Like /v1/capacity it is for any paired device and says nothing about where
// this daemon keeps its state; Clawdline Cloud carries it as `machine-usage`.
// It reads the session list's held reading rather than scanning again, because
// the dashboard is opened exactly when the machine can least afford a scan.
func (s *Server) machineUsageRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The machine's usage is read with GET.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	type row struct{ id, label, assistant, tty string }
	rows := map[string]row{}
	roots := []machineusage.Root{{Key: "daemon", PID: os.Getpid()}}
	for _, item := range s.reading(ctx).Sessions {
		// Only assistant sessions are rows, as on the session list.
		if item.Assistant == "" || item.PID <= 0 {
			continue
		}
		key := "session:" + item.ID
		rows[key] = row{id: item.ID, label: item.Label, assistant: string(item.Assistant), tty: item.TTY}
		roots = append(roots, machineusage.Root{Key: key, PID: item.PID})
	}

	s.usageOnce.Do(func() {
		if s.usage == nil {
			s.usage = machineusage.NewSampler()
		}
	})
	u, err := s.usage.Usage(ctx, roots)
	switch {
	case errors.Is(err, machineusage.ErrUnsupported):
		writeRefusal(w, http.StatusNotImplemented, "machine_usage_unsupported", err.Error())
		return
	case err != nil:
		writeRefusal(w, http.StatusServiceUnavailable, "machine_usage_unreadable", err.Error())
		return
	}
	writeJSON(w, machineUsageWire(u, func(key string) (contract.MachineUsageGroup, bool) {
		if key == "daemon" {
			return contract.MachineUsageGroup{Kind: contract.MachineUsageGroupKindDaemon}, true
		}
		r, ok := rows[key]
		return contract.MachineUsageGroup{Kind: contract.MachineUsageGroupKindSession,
			ID: r.id, Label: r.label, Assistant: r.assistant, TTY: r.tty}, ok
	}))
}

// machineUsageWire is a reading as it is sent. named gives each group the
// identity its key stands for.
func machineUsageWire(u machineusage.Usage, named func(string) (contract.MachineUsageGroup, bool)) contract.MachineUsage {
	out := contract.MachineUsage{
		At: u.At.Unix(), IntervalMs: u.Interval.Milliseconds(), Cores: int64(u.Cores),
		CpuPercent: round1(u.CPUPercent), Load: []float64{u.Load[0], u.Load[1], u.Load[2]},
		MemoryTotalBytes: u.MemTotal, MemoryUsedBytes: u.MemUsed, MemoryAvailableBytes: u.MemAvail,
		SwapTotalBytes: u.SwapTotal, SwapUsedBytes: u.SwapUsed,
		Groups: []contract.MachineUsageGroup{}, Others: []contract.MachineUsageOther{},
	}
	if p := u.Pressure; p != nil {
		out.Pressure = &contract.MachinePressure{CpuSome: p.CPUSome, MemorySome: p.MemorySome,
			MemoryFull: p.MemoryFull, IoSome: p.IOSome}
	}
	for _, g := range u.Groups {
		row, ok := named(g.Key)
		if !ok {
			continue
		}
		row.PID, row.Processes = int64(g.PID), int64(g.Processes)
		row.CpuPercent, row.RssBytes, row.SwapBytes = round1(g.CPUPercent), g.RSS, g.Swap
		out.Groups = append(out.Groups, row)
	}
	for _, o := range u.Others {
		out.Others = append(out.Others, contract.MachineUsageOther{Name: o.Comm,
			Processes: int64(o.Processes), CpuPercent: round1(o.CPUPercent), RssBytes: o.RSS})
	}
	return out
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
