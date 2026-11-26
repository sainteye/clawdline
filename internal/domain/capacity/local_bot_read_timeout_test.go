package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The local Bot read deadline is a millisecond literal in TypeScript and a
// seconds row here. The test reads the literal and requires the row, its Go
// source and the literal to name the same wait, so the measured rationale on
// MaximumLocalBotReadSeconds cannot drift from the number the console uses.
func TestLocalBotReadDeadlineMatchesItsRow(t *testing.T) {
	const file = "web/console/src/cloud/project-assistants.ts"
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^const LOCAL_BOT_READ_TIMEOUT_MS = ([0-9_]+)\s*$`).FindSubmatch(raw)
	if match == nil {
		t.Fatalf("%s no longer declares LOCAL_BOT_READ_TIMEOUT_MS", file)
	}
	milliseconds, err := strconv.ParseInt(strings.ReplaceAll(string(match[1]), "_", ""), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if milliseconds != MaximumLocalBotReadSeconds*1000 {
		t.Errorf("LOCAL_BOT_READ_TIMEOUT_MS = %d ms in %s, MaximumLocalBotReadSeconds = %d s", milliseconds, file, MaximumLocalBotReadSeconds)
	}
	for _, row := range Register() {
		if row.Name != "console.local_bot_read_timeout" {
			continue
		}
		if row.Unit != Seconds || row.Limit != MaximumLocalBotReadSeconds || row.AtLimit != Expire {
			t.Errorf("registered as %+v, want %d seconds, expire", row, MaximumLocalBotReadSeconds)
		}
		if len(row.Sources) != 1 || row.Sources[0] != "internal/domain/capacity.MaximumLocalBotReadSeconds" {
			t.Errorf("Sources = %v, want the measured constant", row.Sources)
		}
		return
	}
	t.Error("no row named console.local_bot_read_timeout")
}
