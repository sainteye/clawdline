//go:build linux

package antigravity

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/ports"
	"golang.org/x/sys/unix"
)

// MaxNativeProcessFileBytes bounds each kernel pseudo-file read. Exceeding it
// refuses identity; it never truncates into a plausible successful observation.
const MaxNativeProcessFileBytes = 1 << 20

func nativeProcFile(ctx context.Context, path string) ([]byte, error) {
	if err := nativeContext(ctx); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nativeProcessReadError(err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxNativeProcessFileBytes+1))
	if err != nil {
		return nil, nativeProcessReadError(err)
	}
	if len(data) > MaxNativeProcessFileBytes {
		return nil, nativeIdentityError("antigravity_native_process_file_too_large", nil)
	}
	if err := nativeContext(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

type linuxNativeStat struct {
	pid, parent int
	tty         uint32
	start       uint64
	// Address fields change across an observed exec even when exe is unchanged.
	code, endCode, stack uint64
}

func parseLinuxNativeStat(data []byte) (linuxNativeStat, error) {
	s := string(data)
	open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	bad := func() (linuxNativeStat, error) {
		return linuxNativeStat{}, nativeIdentityError("antigravity_native_identity_invalid", nil)
	}
	if open < 1 || close <= open {
		return bad()
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	fields := strings.Fields(s[close+1:]) // field 3, state, onwards
	if err != nil || pid <= 0 || len(fields) < 26 || len(fields[0]) != 1 || strings.ContainsAny(fields[0], "ZXx") {
		return bad()
	}
	parent, e1 := strconv.ParseInt(fields[4-3], 10, 32)
	tty, e2 := strconv.ParseInt(fields[7-3], 10, 32)
	start, e3 := strconv.ParseUint(fields[22-3], 10, 64)
	code, e4 := strconv.ParseUint(fields[26-3], 10, 64)
	endCode, e5 := strconv.ParseUint(fields[27-3], 10, 64)
	stack, e6 := strconv.ParseUint(fields[28-3], 10, 64)
	if parent < 0 || e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
		return bad()
	}
	return linuxNativeStat{pid: pid, parent: int(parent), tty: uint32(tty), start: start, code: code, endCode: endCode, stack: stack}, nil
}

func linuxNativeClockTicks(aux []byte) uint64 {
	// Linux ELF auxv ABI: pairs of native unsigned longs, AT_CLKTCK=17.
	word := strconv.IntSize / 8
	read := func(b []byte) uint64 {
		if word == 4 {
			return uint64(binary.NativeEndian.Uint32(b))
		}
		return binary.NativeEndian.Uint64(b)
	}
	for len(aux) >= 2*word {
		tag, value := read(aux), read(aux[word:])
		if tag == 0 {
			break
		}
		if tag == 17 {
			return value
		}
		aux = aux[2*word:]
	}
	return 0
}

func linuxNativeBirth(stat []byte, start, ticks uint64) (string, error) {
	if ticks == 0 || ticks > uint64(time.Second) {
		return "", nativeIdentityError("antigravity_native_clock_unknown", nil)
	}
	for _, line := range strings.Split(string(stat), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "btime" {
			continue
		}
		boot, err := strconv.ParseInt(fields[1], 10, 64)
		elapsed := start / ticks
		if err != nil || boot <= 0 || elapsed > uint64((1<<63-1)-boot) {
			break
		}
		nanos := int64((start % ticks) * uint64(time.Second) / ticks)
		return time.Unix(boot+int64(elapsed), nanos).UTC().Format(time.RFC3339Nano), nil
	}
	return "", nativeIdentityError("antigravity_native_clock_unknown", nil)
}

func linuxNativeTTY(dev uint32) (string, error) {
	if dev == 0 {
		return "", nil
	}
	// /proc/pid/stat tty_nr uses old_encode_dev (major 8..19, minor 0..7
	// and 20..31); UNIX98 ptys use majors 136..143, 256 minors per major.
	major, minor := (dev>>8)&0xfff, (dev&0xff)|((dev>>12)&0xfff00)
	var path string
	switch {
	case major >= 136 && major <= 143:
		path = "/dev/pts/" + strconv.FormatUint(uint64((major-136)*256+minor), 10)
	case major == 4 && minor < 64:
		path = "/dev/tty" + strconv.FormatUint(uint64(minor), 10)
	case major == 4:
		path = "/dev/ttyS" + strconv.FormatUint(uint64(minor-64), 10)
	case major == 5 && minor == 1:
		path = "/dev/console"
	default:
		// Other terminal drivers have no verified naming rule here.
		return "", nativeIdentityError("antigravity_native_tty_unsupported", nil)
	}
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(st.Rdev) != major || unix.Minor(st.Rdev) != minor {
		return "", nativeIdentityError("antigravity_native_tty_unreadable", nil)
	}
	return path, nil
}

func readNativeProcessSample(ctx context.Context, pid int) (nativeProcessSample, error) {
	return readLinuxNativeProcessSample(ctx, "/proc", pid)
}

func readLinuxNativeProcessSample(ctx context.Context, proc string, pid int) (nativeProcessSample, error) {
	dir := filepath.Join(proc, strconv.Itoa(pid))
	statRaw, err := nativeProcFile(ctx, filepath.Join(dir, "stat"))
	if err != nil {
		return nativeProcessSample{}, err
	}
	before, err := parseLinuxNativeStat(statRaw)
	if err != nil || before.pid != pid {
		return nativeProcessSample{}, nativeIdentityError("antigravity_native_identity_invalid", nil)
	}
	bootRaw, err := nativeProcFile(ctx, filepath.Join(proc, "sys/kernel/random/boot_id"))
	if err != nil {
		return nativeProcessSample{}, err
	}
	bootID := strings.TrimSpace(string(bootRaw))
	if id, e := uuid.Parse(bootID); e != nil || id.String() != bootID {
		return nativeProcessSample{}, nativeIdentityError("antigravity_native_boot_unknown", nil)
	}
	bootStat, err := nativeProcFile(ctx, filepath.Join(proc, "stat"))
	if err != nil {
		return nativeProcessSample{}, err
	}
	aux, err := nativeProcFile(ctx, filepath.Join(proc, "self/auxv"))
	if err != nil {
		return nativeProcessSample{}, err
	}
	birth, err := linuxNativeBirth(bootStat, before.start, linuxNativeClockTicks(aux))
	if err != nil {
		return nativeProcessSample{}, err
	}
	executable, err := os.Readlink(filepath.Join(dir, "exe"))
	if err != nil {
		return nativeProcessSample{}, nativeProcessReadError(err)
	}
	image, err := os.Stat(filepath.Join(dir, "exe"))
	if err != nil || !image.Mode().IsRegular() || !filepath.IsAbs(executable) || strings.HasSuffix(executable, " (deleted)") {
		return nativeProcessSample{}, nativeProcessReadError(err)
	}
	tty, err := linuxNativeTTY(before.tty)
	if err != nil {
		return nativeProcessSample{}, err
	}
	workspace, _ := os.Readlink(filepath.Join(dir, "cwd"))
	if !filepath.IsAbs(workspace) || strings.HasSuffix(workspace, " (deleted)") {
		workspace = ""
	}
	afterRaw, err := nativeProcFile(ctx, filepath.Join(dir, "stat"))
	if err != nil {
		return nativeProcessSample{}, err
	}
	after, err := parseLinuxNativeStat(afterRaw)
	if err != nil || before != after {
		return nativeProcessSample{}, nativeIdentityError("antigravity_native_process_changed", nil)
	}
	return nativeProcessSample{NativeProcessObservation: NativeProcessObservation{
		Process:      ports.AntigravityProcess{PID: pid, ParentPID: before.parent, Birth: birth, Executable: executable, TTY: tty},
		WorkspaceCWD: workspace, BootID: bootID,
	}, generation: fmt.Sprintf("%s/%d/%d/%d/%d", bootID, before.start, before.code, before.endCode, before.stack), image: image}, nil
}
