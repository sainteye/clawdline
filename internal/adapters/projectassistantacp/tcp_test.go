package projectassistantacp

import (
	"context"
	"net/netip"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type fixtureTCPRoot struct {
	value tcpIdentity
	err   error
}

func TestTCPObservationRefusesExcessReadersWithoutQueuingAnOSProbe(t *testing.T) {
	entered, release := make(chan struct{}, capacity.MaximumBotServerProbeReaders), make(chan struct{})
	var all sync.WaitGroup
	unblock := sync.OnceFunc(func() { close(release) })
	defer func() { unblock(); all.Wait() }()
	o, _, _ := tcpFixture()
	limit := tcpReadLimit()
	query := o.query
	o.query = func(ctx context.Context, pids []int, l Limits) ([]tcpBind, error) {
		entered <- struct{}{}
		<-release
		return query(ctx, pids, l)
	}
	all.Add(limit)
	for range limit {
		go func() {
			defer all.Done()
			if _, err := o.inspect(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.limits.CleanupTimeout)
	defer cancel()
	for range limit {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("readers did not reach the controlled query")
		}
	}
	if SourceTCPReaders() != limit {
		t.Fatal("active observations not measured")
	}
	if rows, err := o.inspect(context.Background()); pa.Code(err) != pa.CapacityFull || rows != nil {
		t.Error("excess reader became an OS probe", rows, err)
	}
	unblock()
	all.Wait()
	if SourceTCPReaders() != 0 {
		t.Fatal("observation slots leaked")
	}
}

func TestTCPObservationAdmissionUsesOnlyLowerCapacityOverrides(t *testing.T) {
	for _, spec := range []string{"", "bots.server_probe_readers=99", "bots.server_probe_readers=0"} {
		if got := sourceTCPProbeLimit(spec); got != capacity.MaximumBotServerProbeReaders {
			t.Fatal(spec, got)
		}
	}
	if got := sourceTCPProbeLimit("bots.server_probe_readers=2"); got != 2 {
		t.Fatal(got)
	}
}

func (r *fixtureTCPRoot) read(context.Context) (tcpIdentity, error) { return r.value, r.err }
func (*fixtureTCPRoot) close()                                      {}
func tcpFixture() (*tcpObserver, *fixtureTCPRoot, map[int]tcpIdentity) {
	root := &fixtureTCPRoot{value: tcpIdentity{pid: 10, parent: 1, birth: "same-second", unique: "kernel-root-one"}}
	identities := map[int]tcpIdentity{11: {pid: 11, parent: 10, birth: "child-birth", unique: "kernel-child-one"}}
	o := &tcpObserver{root: root, limits: testLimits(), census: func(context.Context) ([]processIdentity, error) {
		return []processIdentity{{pid: 10, ppid: 1, state: "S"}, {pid: 11, ppid: 10, state: "S"}, {pid: 99, ppid: 1, state: "S"}}, nil
	}, sample: func(_ context.Context, pid, _ int) (tcpIdentity, error) { return identities[pid], nil }, query: func(context.Context, []int, Limits) ([]tcpBind, error) {
		return []tcpBind{{pid: 11, address: netip.MustParseAddrPort("127.0.0.1:4321")}}, nil
	}}
	return o, root, identities
}
func TestOwnedTCPObservationRefusesReusedOrChangingProcessIdentity(t *testing.T) {
	for _, change := range []string{"root generation", "child generation", "parent", "foreign bind", "new child", "query failure"} {
		t.Run(change, func(t *testing.T) {
			o, root, identities := tcpFixture()
			query := o.query
			o.query = func(ctx context.Context, pids []int, l Limits) ([]tcpBind, error) {
				result, err := query(ctx, pids, l)
				switch change {
				case "root generation":
					root.value.unique = "kernel-root-reused" // Same PID and same second are insufficient.
				case "child generation":
					v := identities[11]
					v.unique = "kernel-child-reused"
					identities[11] = v
				case "parent":
					v := identities[11]
					v.parent = 99
					identities[11] = v
				case "foreign bind":
					result[0].pid = 99
				case "new child":
					identities[12] = tcpIdentity{pid: 12, parent: 10, birth: "new", unique: "new"}
					old := o.census
					o.census = func(ctx context.Context) ([]processIdentity, error) {
						rows, err := old(ctx)
						return append(rows, processIdentity{pid: 12, ppid: 10, state: "S"}), err
					}
				case "query failure":
					return nil, refusal(pa.ExecutionEffectUnknown, "synthetic_inventory")
				}
				return result, err
			}
			rows, err := o.inspect(context.Background())
			if err == nil || rows != nil {
				t.Fatal("uncertain or foreign listener disclosed", rows, err)
			}
		})
	}
}
func TestOwnedTCPObservationUsesOnlyItsBoundedCurrentTree(t *testing.T) {
	o, _, _ := tcpFixture()
	o.query = func(_ context.Context, pids []int, _ Limits) ([]tcpBind, error) {
		if len(pids) != 2 || pids[0] != 10 || pids[1] != 11 {
			t.Fatal("foreign process queried", pids)
		}
		return []tcpBind{{pid: 11, address: netip.MustParseAddrPort("127.0.0.1:4321")}, {pid: 10, address: netip.MustParseAddrPort("127.0.0.1:4321")}}, nil
	}
	rows, err := o.inspect(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Address != "127.0.0.1" || rows[0].Port != 4321 {
		t.Fatal(rows, err)
	}
	o.limits.OwnedDescendants = 1
	if rows, err := o.inspect(context.Background()); pa.Code(err) != pa.CapacityFull || rows != nil {
		t.Fatal(rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o.limits = testLimits()
	if rows, err := o.inspect(ctx); err == nil || rows != nil {
		t.Fatal(rows, err)
	}
}
func TestMissingTCPObservationDoesNotInventAnEmptyInventory(t *testing.T) {
	p := &Process{}
	if rows, err := p.TCPListeners(context.Background()); pa.Code(err) != pa.Unsupported || rows != nil {
		t.Fatal(rows, err)
	}
	p.tcp = &tcpObserver{err: refusal(pa.ExecutionEffectUnknown, "synthetic_read_denied")}
	if rows, err := p.TCPListeners(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown || rows != nil {
		t.Fatal(rows, err)
	}
	o, _, _ := tcpFixture()
	o.census = func(context.Context) ([]processIdentity, error) { return nil, nil }
	if rows, err := o.inspect(context.Background()); err == nil || rows != nil {
		t.Fatal("an incomplete census became a complete empty reading", rows, err)
	}
}
