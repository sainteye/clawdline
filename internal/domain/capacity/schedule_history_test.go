package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestScheduleHistoryPageIsRegistered(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web/console/src/pages/schedules.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`const historyPageRows = ([0-9]+)`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("missing history page bound")
	}
	limit, _ := strconv.Atoi(string(match[1]))
	for _, entry := range Register() {
		if entry.Name == "schedule.history_page_rows" && entry.Limit == int64(limit) {
			return
		}
	}
	t.Fatal("schedule history page bound is not registered")
}
