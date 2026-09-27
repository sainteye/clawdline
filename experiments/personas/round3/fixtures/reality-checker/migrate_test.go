package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateV1(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tasks.json")
	v1 := `[{"id":"t0001","title":"Water plants","done":false,"labels":"home","created":"2025-11-02T08:00:00Z"},
	        {"id":"t0004","title":"Renew passport","done":true,"labels":"","created":"2025-12-01T10:30:00Z"}]`
	if err := os.WriteFile(p, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	migrated, err := Migrate(p)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	tasks := s.List()
	if len(tasks) != 2 || tasks[0].Title != "Water plants" || !tasks[1].Done {
		t.Fatalf("got %+v", tasks)
	}
	if len(tasks[0].Tags) != 1 || tasks[0].Tags[0] != "home" {
		t.Fatalf("tags %v", tasks[0].Tags)
	}
	if s.nextID != 5 {
		t.Fatalf("nextID %d", s.nextID)
	}
	if _, err := os.Stat(p + ".v1.bak"); err != nil {
		t.Fatalf("no backup: %v", err)
	}
	// Running again on the v2 file is a no-op.
	if again, err := Migrate(p); err != nil || again {
		t.Fatalf("second run migrated=%v err=%v", again, err)
	}
}
