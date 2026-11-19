//go:build darwin

package projectassistantacp

import (
	"context"
	"encoding/binary"
	"net/netip"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"golang.org/x/sys/unix"
)

type darwinTCPRoot struct{ identity tcpIdentity }

func (r *darwinTCPRoot) close() {}
func (r *darwinTCPRoot) read(ctx context.Context) (tcpIdentity, error) {
	current, err := tcpProcessIdentity(ctx, r.identity.pid, 0)
	if err != nil || current.pid != r.identity.pid || current.birth != r.identity.birth || current.unique != r.identity.unique {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_root_identity")
	}
	return current, nil
}
func openTCPRoot(ctx context.Context, pid, budget int) (tcpRoot, error) {
	identity, err := tcpProcessIdentity(ctx, pid, budget)
	if err != nil {
		return nil, err
	}
	return &darwinTCPRoot{identity}, nil
}

// PROC_PIDUNIQIDENTIFIERINFO (17) has a 56-byte Darwin ABI layout. Its
// p_uniqueid at offset16 survives exec but never transfers to a reused PID.
// The existing native-process sampler uses this same XNU kernel interface;
// only ownership identifiers are needed here, never argv, cwd or environment.
func darwinTCPUnique(pid int) (uint64, error) {
	var data [56]byte
	n, _, errno := syscall.Syscall6(unix.SYS_PROC_INFO, 2, uintptr(pid), 17, 0, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	runtime.KeepAlive(&data)
	if errno != 0 || n != uintptr(len(data)) {
		return 0, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	value := binary.NativeEndian.Uint64(data[16:24])
	if value == 0 {
		return 0, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	return value, nil
}
func tcpProcessIdentity(ctx context.Context, pid, _ int) (tcpIdentity, error) {
	if err := ctx.Err(); err != nil {
		return tcpIdentity{}, err
	}
	before, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || before == nil || before.Proc.P_pid != int32(pid) || before.Proc.P_stat == 5 {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	unique, err := darwinTCPUnique(pid)
	if err != nil {
		return tcpIdentity{}, err
	}
	after, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || after == nil || after.Proc.P_pid != int32(pid) || after.Proc.P_stat == 5 || before.Proc.P_starttime != after.Proc.P_starttime || before.Eproc.Ppid != after.Eproc.Ppid {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	last, err := darwinTCPUnique(pid)
	if err != nil || last != unique {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	tv := after.Proc.P_starttime
	if tv.Sec <= 0 || tv.Usec < 0 || tv.Usec >= 1000000 {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	if err := ctx.Err(); err != nil {
		return tcpIdentity{}, err
	}
	return tcpIdentity{pid: pid, parent: int(after.Eproc.Ppid), birth: time.Unix(tv.Sec, int64(tv.Usec)*1000).UTC().Format(time.RFC3339Nano), unique: strconv.FormatUint(unique, 10)}, nil
}
func queryTCPListeners(ctx context.Context, pids []int, l Limits) ([]tcpBind, error) {
	ids := make([]string, len(pids))
	for i, pid := range pids {
		ids[i] = strconv.Itoa(pid)
	}
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-n", "-P", "-a", "-p", strings.Join(ids, ","), "-iTCP", "-sTCP:LISTEN", "-F", "pftn")
	cmd.Env = []string{"LC_ALL=C"}
	output, diagnostic := &censusCapture{budget: l.CaptureBytes}, &censusCapture{budget: l.StderrBytes}
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	err := cmd.Run()
	if err != nil || diagnostic.buf.Len() != 0 {
		// Exit1 can mean no matches OR an unreadable process. Empty stdout alone
		// cannot establish that a command owns no listeners.
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
	}
	return parseTCPLsof(output.buf.String())
}
func parseTCPLsof(text string) ([]tcpBind, error) {
	result := []tcpBind{}
	pid := 0
	descriptor, family := "", ""
	bad := func() ([]tcpBind, error) { return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory") }
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if len(line) < 2 {
			return bad()
		}
		switch line[0] {
		case 'p':
			value, err := strconv.Atoi(line[1:])
			if err != nil || value <= 0 {
				return bad()
			}
			pid = value
			descriptor = ""
			family = ""
		case 'f':
			if pid == 0 {
				return bad()
			}
			descriptor = line[1:]
			family = ""
		case 't':
			if descriptor == "" {
				return bad()
			}
			family = line[1:]
		case 'n':
			if pid == 0 || descriptor == "" || (family != "IPv4" && family != "IPv6") {
				return bad()
			}
			name := line[1:]
			if strings.HasPrefix(name, "*:") {
				host := "0.0.0.0"
				if family == "IPv6" {
					host = "[::]"
				}
				name = host + name[1:]
			}
			address, err := netip.ParseAddrPort(name)
			if err != nil || address.Port() == 0 {
				return bad()
			}
			result = append(result, tcpBind{pid: pid, address: address})
		default:
			return bad()
		}
	}
	return result, nil
}
