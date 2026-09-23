//go:build linux

package swiftstore

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLinuxProcessStartMatchesProcessTable(t *testing.T) {
	cmd := exec.Command("/bin/ps", "-p", strconv.Itoa(os.Getpid()), "-o", "lstart=")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	want, err := time.Parse("Mon Jan 2 15:04:05 2006", strings.Join(strings.Fields(string(out)), " "))
	if err != nil {
		t.Fatal(err)
	}
	got := ProcessStart(os.Getpid())
	if !got.Equal(want) {
		t.Fatalf("kernel start = %v, ps = %v", got, want)
	}
	for _, pid := range []int{0, -1} {
		if got := ProcessStart(pid); !got.IsZero() {
			t.Fatalf("invalid pid %d got %v", pid, got)
		}
	}
}

func TestLinuxProcessStartKeepsCommandPunctuationOutOfFields(t *testing.T) {
	stat := "42 (worker ) with space) S " + strings.Repeat("0 ", 18) + "12345 0"
	if got := linuxProcessStart(stat, "cpu 0\nbtime 1000\n", 100); !got.Equal(time.Unix(1123, 0)) {
		t.Fatalf("start from a punctuated command = %v", got)
	}
	for _, tc := range []struct {
		stat, boot string
		ticks      int64
	}{
		{stat, "btime 1000", 0},
		{"42 (short) S 0", "btime 1000", 100},
		{strings.Replace(stat, "12345", "invalid", 1), "btime 1000", 100},
		{stat, "btime invalid", 100},
		{stat, "cpu 0", 100},
	} {
		if got := linuxProcessStart(tc.stat, tc.boot, tc.ticks); !got.IsZero() {
			t.Fatalf("missing evidence produced %v", got)
		}
	}
}
