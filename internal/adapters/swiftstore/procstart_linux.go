//go:build linux

package swiftstore

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"time"
)

// ProcessStart reads Linux's kernel identity without a subprocess or cgo.
// /proc/pid/stat field 22 is ticks since boot; /proc/stat supplies btime.
// AT_CLKTCK in our auxiliary vector supplies the tick rate, rather than
// assuming a particular kernel configuration. Missing evidence stays zero.
// See proc_pid_stat(5), proc_stat(5), and getauxval(3).
func ProcessStart(pid int) time.Time {
	if pid <= 0 {
		return time.Time{}
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}
	}
	boot, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	aux, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return time.Time{}
	}
	return linuxProcessStart(string(stat), string(boot), linuxClockTicks(aux))
}

func linuxClockTicks(aux []byte) int64 {
	// Auxiliary-vector entries are pairs of native unsigned longs. These
	// numbers are Linux ABI fields, not resource bounds.
	const atClockTicks = 17 // AT_CLKTCK in linux/auxvec.h.
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
		if tag == atClockTicks && value > 0 && value <= uint64(1<<63-1) {
			return int64(value)
		}
		aux = aux[2*word:]
	}
	return 0
}

func linuxProcessStart(stat, boot string, ticks int64) time.Time {
	if ticks <= 0 {
		return time.Time{}
	}
	// comm (field 2) can itself contain spaces and closing parentheses.
	// Only the final ')' starts the fixed numeric fields.
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return time.Time{}
	}
	fields := strings.Fields(stat[end+1:])
	const startField = 22 - 3 // Fields here begin with state, field 3.
	if len(fields) <= startField {
		return time.Time{}
	}
	started, err := strconv.ParseInt(fields[startField], 10, 64)
	if err != nil || started < 0 {
		return time.Time{}
	}
	for _, line := range strings.Split(boot, "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] != "btime" {
			continue
		}
		seconds, err := strconv.ParseInt(parts[1], 10, 64)
		elapsed := started / ticks
		if err != nil || seconds <= 0 || seconds > (1<<63-1)-elapsed {
			return time.Time{}
		}
		return time.Unix(seconds+elapsed, 0)
	}
	return time.Time{}
}
