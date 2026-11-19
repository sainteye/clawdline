//go:build linux

package projectassistantacp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"golang.org/x/sys/unix"
)

type linuxTCPRoot struct {
	mu       sync.Mutex
	fd       int
	identity tcpIdentity
	budget   int
}

func (r *linuxTCPRoot) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fd >= 0 {
		_ = unix.Close(r.fd)
		r.fd = -1
	}
}
func (r *linuxTCPRoot) read(ctx context.Context) (tcpIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	alive := func() bool {
		if r.fd < 0 {
			return false
		}
		n, err := unix.Poll([]unix.PollFd{{Fd: int32(r.fd), Events: unix.POLLIN}}, 0)
		return err == nil && n == 0
	}
	if !alive() {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_root_identity")
	}
	current, err := tcpProcessIdentity(ctx, r.identity.pid, r.budget)
	if err != nil || current != r.identity || !alive() {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_root_identity")
	}
	return current, nil
}
func openTCPRoot(ctx context.Context, pid, budget int) (tcpRoot, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, refusal(pa.Unsupported, "listener_pidfd")
	}
	identity, err := tcpProcessIdentity(ctx, pid, budget)
	if err != nil {
		unix.Close(fd)
		return nil, err
	}
	r := &linuxTCPRoot{fd: fd, identity: identity, budget: budget}
	if _, err = r.read(ctx); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}
func tcpProcessIdentity(ctx context.Context, pid, budget int) (tcpIdentity, error) {
	b, err := tcpReadFile(ctx, "/proc/"+strconv.Itoa(pid)+"/stat", &budget)
	if err != nil {
		return tcpIdentity{}, err
	}
	return parseTCPProcIdentity(string(b), pid)
}
func parseTCPProcIdentity(text string, pid int) (tcpIdentity, error) {
	bad := func() (tcpIdentity, error) {
		return tcpIdentity{}, refusal(pa.ExecutionEffectUnknown, "listener_kernel_identity")
	}
	open, close := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if open < 1 || close < open {
		return bad()
	}
	actual, err := strconv.Atoi(strings.TrimSpace(text[:open]))
	if err != nil || actual != pid {
		return bad()
	}
	fields := strings.Fields(text[close+1:]) // state (field3) onward; start ticks are field22.
	if len(fields) <= 22-3 || strings.ContainsAny(fields[0], "ZXx") {
		return bad()
	}
	parent, err := strconv.Atoi(fields[4-3])
	if err != nil || parent < 0 {
		return bad()
	}
	ticks, err := strconv.ParseUint(fields[22-3], 10, 64)
	if err != nil || ticks == 0 {
		return bad()
	}
	return tcpIdentity{pid: pid, parent: parent, birth: strconv.FormatUint(ticks, 10), unique: strconv.FormatUint(ticks, 10)}, nil
}

type tcpNamespace struct {
	pid    int
	inodes map[string]int
	fds    map[string][]tcpSocketFD
}

type tcpSocketFD struct {
	pid  int
	path string
}

func queryTCPListeners(ctx context.Context, pids []int, l Limits) ([]tcpBind, error) {
	budget := l.CaptureBytes
	spaces := map[string]*tcpNamespace{}
	hostNamespace, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_namespace")
	}
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		base := "/proc/" + strconv.Itoa(pid)
		namespace, err := os.Readlink(base + "/ns/net")
		if err != nil {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_namespace")
		}
		if namespace != hostNamespace {
			return nil, refusal(pa.Unsupported, "listener_network_namespace")
		}
		budget -= len(namespace)
		if budget <= 0 {
			return nil, refusal(pa.CapacityFull, "listener_capture")
		}
		group := spaces[namespace]
		if group == nil {
			group = &tcpNamespace{pid: pid, inodes: map[string]int{}, fds: map[string][]tcpSocketFD{}}
			spaces[namespace] = group
		}
		directory, err := os.Open(base + "/fd")
		if err != nil {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_descriptors")
		}
		err = func() error {
			defer directory.Close()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				entries, err := directory.ReadDir(1)
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return refusal(pa.ExecutionEffectUnknown, "listener_descriptors")
				}
				for _, entry := range entries {
					link, err := os.Readlink(filepath.Join(base, "fd", entry.Name()))
					if err != nil {
						return refusal(pa.ExecutionEffectUnknown, "listener_descriptors")
					}
					budget -= len(entry.Name()) + len(link)
					if budget <= 0 {
						return refusal(pa.CapacityFull, "listener_capture")
					}
					if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
						inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
						group.inodes[inode] = pid
						group.fds[inode] = append(group.fds[inode], tcpSocketFD{pid: pid, path: filepath.Join(base, "fd", entry.Name())})
					}
				}
			}
		}()
		if err != nil {
			return nil, err
		}
	}
	result := []tcpBind{}
	for _, group := range spaces {
		for _, name := range []string{"tcp", "tcp6"} {
			data, err := tcpReadFile(ctx, "/proc/"+strconv.Itoa(group.pid)+"/net/"+name, &budget)
			if err != nil {
				return nil, err
			}
			rows, err := parseTCPProcTable(string(data), group.inodes)
			if err != nil {
				return nil, err
			}
			for _, bind := range rows {
				present := false
				for _, fd := range group.fds[bind.socket] {
					if fd.pid != bind.pid {
						continue
					}
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					link, err := os.Readlink(fd.path)
					budget -= len(link)
					if budget <= 0 {
						return nil, refusal(pa.CapacityFull, "listener_capture")
					}
					if err == nil && link == "socket:["+bind.socket+"]" {
						present = true
						break
					}
				}
				namespace, err := os.Readlink("/proc/" + strconv.Itoa(bind.pid) + "/ns/net")
				if !present || err != nil || namespace != hostNamespace {
					return nil, refusal(pa.ExecutionEffectUnknown, "listener_descriptor_changed")
				}
			}
			result = append(result, rows...)
		}
	}
	return result, nil
}
func parseTCPProcTable(text string, inodes map[string]int) ([]tcpBind, error) {
	lines := strings.Split(text, "\n")
	header := strings.Fields(lines[0])
	if len(header) < 4 || header[0] != "sl" || header[1] != "local_address" || header[3] != "st" {
		return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
	}
	result := []tcpBind{}
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
		}
		pid, owned := inodes[fields[9]]
		if !owned || fields[3] != "0A" {
			continue
		}
		host, port, ok := strings.Cut(fields[1], ":")
		if !ok {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
		}
		raw, err := hex.DecodeString(host)
		if err != nil || len(raw) != 4 && len(raw) != 16 {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
		}
		// proc TCP words are native-endian uint32 values, including IPv6's four words.
		for offset := 0; offset < len(raw); offset += 4 {
			value, err := strconv.ParseUint(host[offset*2:offset*2+8], 16, 32)
			if err != nil {
				return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
			}
			binary.NativeEndian.PutUint32(raw[offset:offset+4], uint32(value))
		}
		address, ok := netip.AddrFromSlice(raw)
		number, err := strconv.ParseUint(port, 16, 16)
		if !ok || err != nil || number == 0 {
			return nil, refusal(pa.ExecutionEffectUnknown, "listener_inventory")
		}
		result = append(result, tcpBind{pid: pid, address: netip.AddrPortFrom(address, uint16(number)), socket: fields[9]})
	}
	return result, nil
}
