package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The voice copilot's console bounds are constants in TypeScript; each must
// stay the number its registration says.
func TestTheVoiceCopilotConsoleBoundsMatchTheirRegistrations(t *testing.T) {
	for _, c := range []struct{ file, constant, name string }{
		{"../voice-work/engine.ts", "WORK_EXECUTION_MS", "voice.work_execution_milliseconds"},
		{"../voice-work/engine.ts", "WORK_REQUEST_MS", "voice.work_request_milliseconds"},
		{"../voice-work/engine.ts", "WORK_POLL_MS", "voice.work_poll_milliseconds"},
		{"../voice-work/engine.ts", "WORK_HEARTBEAT_MS", "voice.work_heartbeat_milliseconds"},
		{"../voice-work/engine.ts", "WORK_TEXT_RUNES", "voice.work_text_characters"},
		{"copilot-memory.ts", "MEMORY_TURNS", "console.copilot_memory_turns"},
		{"copilot-tools.ts", "LIST_LATEST_READS", "console.copilot_list_transcript_reads"},
		{"copilot-tools.ts", "RECAP_PAGES", "console.copilot_recap_pages"},
		{"copilot-help.ts", "HELP_SECTIONS", "console.copilot_help_sections"},
		{"copilot-help.ts", "HELP_SECTION_CHARS", "console.copilot_help_section_characters"},
		{"copilot-tools.ts", "SCREEN_READ_ATTEMPTS", "console.copilot_screen_reads"},
		{"copilot-tools.ts", "REPLY_CHARS", "console.copilot_reply_characters"},
		{"copilot-tools.ts", "SMART_RENAME_SESSIONS", "console.copilot_smart_rename_sessions"},
		{"copilot-tools.ts", "UPDATE_PAGES", "console.copilot_update_pages"},
		{"copilot-marks.ts", "UPDATE_MARKS", "console.copilot_update_marks"},
		{"copilot-orb.ts", "ORB_MAX_FPS", "console.copilot_orb_frames_per_second"},
		{"copilot-orb.ts", "ORB_MAX_PIXEL_RATIO", "console.copilot_orb_pixel_ratio"},
	} {
		raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web", "console", "src", "realtime", c.file))
		if err != nil {
			t.Fatal(err)
		}
		match := regexp.MustCompile(`export const ` + c.constant + ` = ([0-9]+)`).FindSubmatch(raw)
		if match == nil {
			t.Fatalf("%s is missing from %s", c.constant, c.file)
		}
		limit, _ := strconv.ParseInt(string(match[1]), 10, 64)
		found := false
		for _, e := range Register() {
			if e.Name == c.name {
				found = true
				if e.Limit != limit {
					t.Errorf("%s is %d in %s and %d in its registration", c.constant, limit, c.file, e.Limit)
				}
			}
		}
		if !found {
			t.Errorf("%s has no registration %s", c.constant, c.name)
		}
	}
}
