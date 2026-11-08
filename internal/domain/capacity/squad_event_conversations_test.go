package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The console splits its filtered skill-event reads, and the relay reader
// refuses an oversized one, at the bound the daemon registers.
func TestTheConsoleCopiesTheSkillEventConversationBound(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/session/skill-event-bounds.ts"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		SquadEventConversations: "MAX_SQUAD_EVENT_CONVERSATIONS",
		SquadEventIDBytes:       "MAX_SQUAD_EVENT_ID_BYTES",
	}
	found := 0
	for _, row := range Register() {
		name, ok := want[row.Name]
		if !ok {
			continue
		}
		found++
		match := regexp.MustCompile(`export const ` + name + ` = ([0-9]+)`).FindSubmatch(raw)
		if match == nil {
			t.Fatalf("%s is missing from the console", name)
		}
		limit, err := strconv.ParseInt(string(match[1]), 10, 64)
		if err != nil || limit != row.Limit {
			t.Fatalf("%s = %s, the register says %d", name, match[1], row.Limit)
		}
	}
	if found != len(want) {
		t.Fatalf("found %d of %d registered rows", found, len(want))
	}
}
