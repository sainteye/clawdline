//go:build linux

package antigravity

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeLinuxOwnProcess(t *testing.T) {
	checkNativeOwnProcess(t)
	got, err := NewNativeProcessReader(nil).ReadAntigravityProcessWithWorkspace(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if got.BootID == "" {
		t.Fatal("boot identity absent")
	}
}

func TestNativeLinuxSubprocessExit(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	got, err := NewNativeProcessReader(nil).ReadAntigravityProcess(context.Background(), cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentPID != os.Getpid() || !strings.HasSuffix(got.Executable, "/sleep") {
		t.Fatal("subprocess identity differs")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	_, err = NewNativeProcessReader(nil).ReadAntigravityProcess(context.Background(), cmd.Process.Pid)
	requireNativeCode(t, err, "antigravity_native_process_missing")
}

func linuxStatFixture() string {
	fields := make([]string, 26)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[22-3] = "S", "7", "12345"
	fields[26-3], fields[27-3], fields[28-3] = "100", "200", "300"
	return "42 (synthetic ) command) " + strings.Join(fields, " ")
}

func TestNativeLinuxStatAndSubsecondBirth(t *testing.T) {
	stat, err := parseLinuxNativeStat([]byte(linuxStatFixture()))
	if err != nil || stat.pid != 42 || stat.parent != 7 || stat.start != 12345 {
		t.Fatalf("stat = %+v %v", stat, err)
	}
	birth, err := linuxNativeBirth([]byte("cpu 0\nbtime 1000\n"), stat.start, 100)
	if err != nil || birth != time.Unix(1123, 450000000).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("tick precision lost: %q %v", birth, err)
	}
	for _, bad := range []string{"", "42 (incomplete)", strings.Replace(linuxStatFixture(), ") S ", ") Z ", 1)} {
		if _, err := parseLinuxNativeStat([]byte(bad)); err == nil {
			t.Fatal("malformed/dead process accepted")
		}
	}
	for _, ticks := range []uint64{0, 1000000001} {
		if _, err := linuxNativeBirth([]byte("btime 1000\n"), 12345, ticks); err == nil {
			t.Fatal("invalid tick rate accepted")
		}
	}
	word := strconv.IntSize / 8
	aux := make([]byte, 4*word)
	if word == 8 {
		binary.NativeEndian.PutUint64(aux, 17)
		binary.NativeEndian.PutUint64(aux[word:], 100)
	} else {
		binary.NativeEndian.PutUint32(aux, 17)
		binary.NativeEndian.PutUint32(aux[word:], 100)
	}
	if linuxNativeClockTicks(aux) != 100 || linuxNativeClockTicks(nil) != 0 {
		t.Fatal("AT_CLKTCK decode differs")
	}
}

func TestNativeLinuxFileBoundAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kernel-file")
	data := make([]byte, MaxNativeProcessFileBytes)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := nativeProcFile(context.Background(), path); err != nil || len(got) != len(data) {
		t.Fatal("exact bound refused", err)
	}
	if err := os.WriteFile(path, append(data, 0), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := nativeProcFile(context.Background(), path)
	requireNativeCode(t, err, "antigravity_native_process_file_too_large")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = nativeProcFile(ctx, path)
	requireNativeCode(t, err, "antigravity_native_cancelled")
}
