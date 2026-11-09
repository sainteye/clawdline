package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestVoiceRecoveryBudgetsMatchTheBrowser(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/legacy/voice-bridge.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		symbol, name string
		limit        int64
	}{
		{"RECORDING_BYTES", "recording_bytes", MaximumVoiceRecordingBytes},
		{"STOP_MS", "stop_milliseconds", MaximumVoiceStopMilliseconds},
		{"PREPARE_MS", "prepare_milliseconds", MaximumVoicePrepareMilliseconds},
		{"READ_MS", "read_milliseconds", MaximumVoiceReadMilliseconds},
	} {
		match := regexp.MustCompile(`export const VOICE_` + row.symbol + ` = ([0-9]+)`).FindSubmatch(raw)
		if match == nil {
			t.Fatalf("missing browser budget %s", row.symbol)
		}
		value, err := strconv.ParseInt(string(match[1]), 10, 64)
		if err != nil || value != row.limit {
			t.Fatalf("browser budget mismatch: %s", row.symbol)
		}
		found := false
		for _, entry := range Register() {
			if entry.Name == "console.voice_"+row.name {
				found = true
				if entry.Limit != value {
					t.Fatalf("registered budget mismatch: %+v", entry)
				}
			}
		}
		if !found {
			t.Fatalf("unregistered dictation budget: %s", row.name)
		}
	}
}
