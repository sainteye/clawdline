package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestNextMessagesShareTheRegisteredCardBudget(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/session/persist.ts"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`export const KEPT_CARDS = ([0-9]+)`).FindSubmatch(raw)
	if match == nil {
		t.Fatal("pending card budget missing")
	}
	limit, err := strconv.ParseInt(string(match[1]), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range Register() {
		if row.Name == "console.next_messages" {
			if row.Limit != limit || row.AtLimit != Refuse {
				t.Fatal(row)
			}
			return
		}
	}
	t.Fatal("next-message card budget is not registered")
}
