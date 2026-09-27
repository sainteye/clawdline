package devstack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectDistinguishesMissingReadyAndUnreadableWithoutProbing(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "projects", "shop")
	child := filepath.Join(project, "web")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, state := Inspect(child, home); state != InspectionMissing {
		t.Fatalf("missing state = %q", state)
	}
	file := filepath.Join(project, Filename)
	if err := os.WriteFile(file, []byte(`{"name":"Shop","processes":[{"name":"web","port":4173}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, state := Inspect(child, home)
	if state != InspectionReady || spec.Root != project || len(spec.Declared) != 1 || spec.Declared[0].Port != 4173 {
		t.Fatalf("ready inspection = %#v, %q", spec, state)
	}
	if err := os.WriteFile(filepath.Join(child, Filename), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, state := Inspect(child, home); state != InspectionUnreadable {
		t.Fatalf("invalid nearest file state = %q", state)
	}
}
