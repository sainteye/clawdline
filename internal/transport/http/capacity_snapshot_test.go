package http

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

var update = flag.Bool("update", false, "rewrite testdata/capacity_measures.golden.json from capacityMeasures")

// measureShape is what one measure answers on a fresh directory, without its
// live numbers or its words: which fields of the reading it fills. Used,
// OldestAt and the counters are left out: whether they are zero is a live
// number too, and some come from caches this package's other tests fill.
type measureShape struct {
	Name string
	// HostDependent is a measure that reads this machine rather than the
	// state directory; only its presence is pinned.
	HostDependent   bool `json:",omitempty"`
	Known           bool `json:",omitempty"`
	Err             bool `json:",omitempty"`
	Note            bool `json:",omitempty"`
	HasDiskFree     bool `json:",omitempty"`
	WindowSeconds   bool `json:",omitempty"`
	DurableCounters bool `json:",omitempty"`
	Failing         bool `json:",omitempty"`
}

// hostDependentMeasures answer from the machine the test runs on: whether
// tmux is installed, and how long its temporary directory's path is.
var hostDependentMeasures = map[string]bool{
	capacity.TerminalCount:         true,
	"resources.disk_reserve_bytes": true,
}

func renderMeasures(t *testing.T) []byte {
	t.Helper()
	measures := capacityServer(t).capacityMeasures()
	var shapes []measureShape
	for name, m := range measures {
		if hostDependentMeasures[name] {
			shapes = append(shapes, measureShape{Name: name, HostDependent: true})
			continue
		}
		r := m()
		shapes = append(shapes, measureShape{
			Name: name, Known: r.Known, Err: r.Err != "", Note: r.Note != "",
			HasDiskFree:     r.HasDiskFree,
			WindowSeconds:   r.WindowSeconds != 0,
			DurableCounters: r.DurableCounters,
			Failing:         r.Failing,
		})
	}
	sort.Slice(shapes, func(i, j int) bool { return shapes[i].Name < shapes[j].Name })
	out, err := json.MarshalIndent(shapes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

// The set of rows /v1/capacity measures, and the shape of each reading on a
// fresh directory, are pinned so moving a measure next to the package that
// owns it is proved to drop or reshape none. Regenerate with
// `go test ./internal/transport/http -run TestCapacityMeasuresMatchTheirSnapshot -update`
// only when a measure is meant to change.
func TestCapacityMeasuresMatchTheirSnapshot(t *testing.T) {
	got := renderMeasures(t)
	path := filepath.Join("testdata", "capacity_measures.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to create it", err)
	}
	if bytes.Equal(got, want) {
		return
	}
	var gotShapes, wantShapes []measureShape
	if err := json.Unmarshal(got, &gotShapes); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantShapes); err != nil {
		t.Fatalf("%s is not a measure snapshot: %v", path, err)
	}
	g, w := map[string]measureShape{}, map[string]measureShape{}
	for _, x := range gotShapes {
		g[x.Name] = x
	}
	for _, x := range wantShapes {
		w[x.Name] = x
	}
	for _, x := range wantShapes {
		if gv, ok := g[x.Name]; !ok {
			t.Errorf("%s is in the snapshot and nothing measures it", x.Name)
		} else if gv != x {
			t.Errorf("%s reads differently:\n got  %+v\n want %+v", x.Name, gv, x)
		}
	}
	for _, x := range gotShapes {
		if _, ok := w[x.Name]; !ok {
			t.Errorf("%s is measured and not in the snapshot", x.Name)
		}
	}
	if !t.Failed() {
		t.Errorf("%s differs from the measures in order or formatting; regenerate it with -update", path)
	}
}
