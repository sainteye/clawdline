//go:build darwin

package antigravity

import (
	"encoding/binary"
	"testing"
)

func TestNativeDarwinArgumentCountExcludesEnvironmentTail(t *testing.T) {
	data := make([]byte, 4)
	binary.NativeEndian.PutUint32(data, 3)
	data = append(data, []byte("/synthetic/agy\x00\x00\x00/synthetic/agy\x00--mode\x00plan\x00PRIVATE_SYNTHETIC_KEY=DO_NOT_EXPOSE\x00--mode\x00auto\x00")...)
	got, err := parseDarwinExecutionPlan(data, "/synthetic/agy")
	want, e := parseNativeCommandLine(syntheticPlanBytes("--mode", "plan"), "/synthetic/agy")
	if err != nil || e != nil || got != want {
		t.Fatal("environment tail affected argv plan", err, e)
	}
	for _, count := range []uint32{0, 2, 4, ^uint32(0)} {
		binary.NativeEndian.PutUint32(data, count)
		if _, err := parseDarwinExecutionPlan(data, "/synthetic/agy"); err == nil {
			t.Fatal("malformed argc accepted", count)
		}
	}
	if _, err := parseDarwinExecutionPlan([]byte{0}, "/synthetic/agy"); err == nil {
		t.Fatal("short ABI buffer accepted")
	}
}
