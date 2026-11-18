package projectassistantacp

import (
	"context"
	"io"
	"net/netip"
	"os"
	"sort"
	"sync"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var tcpReadSlots = make(chan struct{}, capacity.MaximumBotServerProbeReaders)
var tcpReadAdmission sync.Mutex
var tcpReadLimit = sync.OnceValue(func() int {
	return sourceTCPProbeLimit(os.Getenv(capacity.OverrideEnv))
})

var _ servers.ListeningProcess = (*Process)(nil)

func sourceTCPProbeLimit(spec string) int {
	resolved, _ := capacity.Resolve(capacity.Register(), spec)
	return int(capacity.Limit(resolved, "bots.server_probe_readers"))
}

// SourceTCPReaders reports actual in-flight observations, not a cached count.
func SourceTCPReaders() int { return len(tcpReadSlots) }

type tcpIdentity struct {
	pid, parent   int
	birth, unique string
}

type tcpRoot interface {
	read(context.Context) (tcpIdentity, error)
	close()
}

type tcpBind struct {
	pid     int
	address netip.AddrPort
	socket  string // Linux descriptor/table join only; never disclosed.
}

type tcpObserver struct {
	root   tcpRoot
	limits Limits
	err    error
	census func(context.Context) ([]processIdentity, error)
	sample func(context.Context, int, int) (tcpIdentity, error)
	query  func(context.Context, []int, Limits) ([]tcpBind, error)
}

func newTCPObserver(ctx context.Context, pid int, limits Limits, census func(context.Context) ([]processIdentity, error)) *tcpObserver {
	root, err := openTCPRoot(ctx, pid, limits.CaptureBytes)
	return &tcpObserver{root: root, err: err, limits: limits, census: census, sample: tcpProcessIdentity, query: queryTCPListeners}
}

func (o *tcpObserver) tree(ctx context.Context) (map[int]tcpIdentity, error) {
	root, err := o.root.read(ctx)
	if err != nil {
		return nil, err
	}
	if root.pid <= 0 || root.birth == "" || root.unique == "" {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_root_identity")
	}
	rows, err := o.census(ctx)
	if err != nil {
		return nil, err
	}
	foundRoot := false
	for _, row := range rows {
		if row.pid == root.pid {
			if row.ppid != root.parent || deadProcess(row) {
				return nil, refusal(pa.ExecutionEffectUnknown, "listener_census")
			}
			foundRoot = true
		}
	}
	if !foundRoot {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_census")
	}
	owned := map[int]tcpIdentity{root.pid: root}
	for changed := true; changed; {
		changed = false
		for _, row := range rows {
			if _, ok := owned[row.pid]; ok || deadProcess(row) {
				continue
			}
			if _, ok := owned[row.ppid]; !ok {
				continue
			}
			if len(owned) >= o.limits.OwnedDescendants {
				return nil, refusal(pa.CapacityFull, "listener_processes")
			}
			current, err := o.sample(ctx, row.pid, o.limits.CaptureBytes)
			if err != nil || current.pid != row.pid || current.parent != row.ppid || current.birth == "" || current.unique == "" {
				return nil, refusal(pa.ExecutionEffectUnknown, "listener_identity")
			}
			owned[row.pid] = current
			changed = true
		}
	}
	if current, err := o.root.read(ctx); err != nil || current != root {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_root_changed")
	}
	return owned, nil
}

func (o *tcpObserver) inspect(ctx context.Context) ([]servers.Listener, error) {
	if o == nil {
		return nil, refusal(pa.Unsupported, "owned_tcp_observation")
	}
	if o.err != nil {
		return nil, o.err
	}
	if o.root == nil {
		return nil, refusal(pa.Unsupported, "owned_tcp_observation")
	}
	ctx, cancel := context.WithTimeout(ctx, o.limits.CleanupTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Keep the backing channel at its registered default ceiling while the
	// same process-start override as diagnostics may lower admission. The
	// mutex spans only this counter change, never a kernel or command read.
	tcpReadAdmission.Lock()
	if len(tcpReadSlots) >= tcpReadLimit() {
		tcpReadAdmission.Unlock()
		return nil, refusal(pa.CapacityFull, "listener_probe_readers")
	}
	tcpReadSlots <- struct{}{}
	tcpReadAdmission.Unlock()
	defer func() { <-tcpReadSlots }()
	before, err := o.tree(ctx)
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(before))
	for pid := range before {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	binds, err := o.query(ctx, pids, o.limits)
	if err != nil {
		return nil, err
	}
	after, err := o.tree(ctx)
	if err != nil || len(before) != len(after) {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_tree_changed")
	}
	for pid, identity := range before {
		if after[pid] != identity {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_tree_changed")
		}
	}
	unique := map[netip.AddrPort]bool{}
	for _, bind := range binds {
		if _, ok := before[bind.pid]; !ok || !bind.address.IsValid() || bind.address.Port() == 0 {
			return nil, refusal(pa.ScopeDenied, "listener_process")
		}
		unique[bind.address] = true
	}
	addresses := make([]netip.AddrPort, 0, len(unique))
	for address := range unique {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].Compare(addresses[j]) < 0 })
	result := make([]servers.Listener, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, servers.Listener{Address: address.Addr().String(), Port: address.Port()})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// TCPListeners observes only a current tree rooted in the unreaped startup
// identity. This read neither extends process lifetime nor authorizes recovery.
func (p *Process) TCPListeners(ctx context.Context) ([]servers.Listener, error) {
	return p.tcp.inspect(ctx)
}

// TCP/proc reads share the supervisor's registered capture budget. No second
// file/table cache or unbounded output capture is introduced.
func tcpReadFile(ctx context.Context, path string, budget *int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if *budget <= 0 {
		return nil, refusal(pa.CapacityFull, "listener_capture")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_read")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(*budget)+1))
	if err != nil || len(b) > *budget {
		return nil, refusal(pa.CapacityFull, "listener_capture")
	}
	*budget -= len(b)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}
