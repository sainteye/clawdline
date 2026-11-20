package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The tower draws one background per floor (tower-scene.ts towerFloors) and
// the machine stores placements up to MaxTowerFloors. A floor added or
// removed on one side only fails here.
func TestTowerFloorsMatchTheConsole(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "web", "console", "src", "project-assistants", "tower", "tower-scene.ts"))
	if err != nil {
		t.Fatalf("read tower-scene.ts: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "export const towerFloors = [")
	if start < 0 {
		t.Fatal("towerFloors is not in tower-scene.ts")
	}
	end := strings.Index(src[start:], "] as const")
	if end < 0 {
		t.Fatal("towerFloors is not closed in tower-scene.ts")
	}
	floors := len(regexp.MustCompile(`\{ label: "[^"]+", scene: \w+ \}`).FindAllString(src[start:start+end], -1))
	var row Entry
	for _, e := range Register() {
		if e.Name == "bots.tower_floors" {
			row = e
		}
	}
	if row.Name == "" {
		t.Fatal("bots.tower_floors is not registered")
	}
	if int64(floors) != row.Limit || row.Limit != pa.MaxTowerFloors {
		t.Fatalf("the console draws %d floors, the register says %d, the domain %d", floors, row.Limit, pa.MaxTowerFloors)
	}
}
