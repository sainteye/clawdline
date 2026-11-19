//go:build darwin || linux

package projectassistantacp

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestOwnedTCPRootIdentitySurvivesTheCommandsExec(t *testing.T) {
	if inventoryUnavailable() {
		t.Skip("requires the owned process census")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	l := SourceProcessLimits()
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: Configuration{
		Executable: "/bin/sh", Arguments: []string{"-c", `IFS= read -r trigger; exec "$1" -test.run=^TestOwnedCommandPeer$`, "synthetic-exec", executable},
		Environment: []string{"CLAWDLINE_SYNTHETIC_COMMAND=server", "GORACE=atexit_sleep_ms=0"},
	}, Directory: filepath.Clean(t.TempDir()), Limits: l, CombinedOutput: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	before, err := p.tcp.root.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Write(context.Background(), []byte("continue\n")); err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddrPort(commandAddress(t, p))
	after, err := p.tcp.root.read(context.Background())
	if err != nil || after != before {
		t.Fatal("legitimate exec transferred or lost ownership", before, after, err)
	}
	rows, err := p.TCPListeners(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Port != address.Port() {
		t.Fatal(rows, err)
	}
}

func TestOwnedTCPKernelObservationFindsOnlyTheRealCommandListener(t *testing.T) {
	p := startOwnedCommandOutput(t, context.Background(), "server", true)
	address := netip.MustParseAddrPort(commandAddress(t, p))
	foreign, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	rows, err := p.TCPListeners(context.Background())
	if err != nil {
		t.Fatal("actual kernel listener observation", err)
	}
	if len(rows) != 1 || rows[0].Address != address.Addr().String() || rows[0].Port != address.Port() {
		t.Fatal("listener escaped its real command boundary", rows, address, foreign.Addr())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rows, err := p.TCPListeners(ctx); err == nil || rows != nil {
		t.Fatal("cancelled observation published", rows, err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("tcp", address.String(), time.Second); err == nil {
		connection.Close()
		t.Fatal("listener survived its command cleanup")
	}
	if rows, err := p.TCPListeners(context.Background()); err == nil || rows != nil {
		t.Fatal("closed process observation looked live", rows, err)
	}
}
func TestOwnedTCPRootPinRejectsASamePIDReplacement(t *testing.T) {
	p := startOwnedCommandOutput(t, context.Background(), "server", true)
	_ = commandAddress(t, p)
	root, err := p.tcp.root.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if root.unique == "" || root.birth == "" {
		t.Fatal("missing kernel root identity", root)
	}
	fake := &fixtureTCPRoot{value: root}
	o := *p.tcp
	o.root = fake
	query := o.query
	o.query = func(ctx context.Context, pids []int, l Limits) ([]tcpBind, error) {
		rows, err := query(ctx, pids, l)
		fake.value.unique = "reused-same-pid-same-birth"
		return rows, err
	}
	if rows, err := o.inspect(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown || rows != nil {
		t.Fatal(rows, err)
	}
}
