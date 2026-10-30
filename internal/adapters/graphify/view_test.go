package graphify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const viewFixture = `{"graph":{"schema_version":1},"nodes":[{"id":"a","label":"A","source_file":"source.go","source_location":"L2","community":1,"html":"ignored"},{"id":"b","label":"B","community":2}],"links":[{"source":"a","target":"b","relation":"calls","confidence":"EXTRACTED","script":"ignored"}]}`

func TestViewUsesTheCheckedGraphAndIsReadOnly(t *testing.T) {
	r := fixture(t)
	put(t, r, "graphify-out/graph.json", viewFixture)
	before := snapshot(t, r)
	view, err := View(context.Background(), r)
	if err != nil || view.State != Matched || len(view.Nodes) != 2 || len(view.Links) != 1 || view.Truncated || view.Nodes[0].File != "source.go" || view.Links[0].Confidence != "EXTRACTED" {
		t.Fatalf("%+v %v", view, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, r)) {
		t.Fatal("viewer wrote files")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "ignored") {
		t.Fatal("arbitrary upstream attributes escaped projection")
	}
	put(t, r, "source.go", "package changed")
	stale, err := View(context.Background(), r)
	if err != nil || stale.State != "stale" || len(stale.Nodes) != 2 {
		t.Fatalf("stale snapshot lost its warning: %+v %v", stale, err)
	}
}
func TestViewRejectsInvalidIdentitiesAndNeverBorrowsAnotherRoot(t *testing.T) {
	for _, raw := range []string{
		`{"graph":{"schema_version":1},"nodes":[{"id":"a"},{"id":"a"}],"links":[]}`,
		`{"graph":{"schema_version":1},"nodes":[{"id":"a"}],"links":[{"source":"a","target":"missing"}]}`,
		`{"graph":{"schema_version":99},"nodes":[],"links":[]}`,
	} {
		r := fixture(t)
		put(t, r, "graphify-out/graph.json", raw)
		v, err := View(t.Context(), r)
		if err != nil || len(v.Nodes) != 0 || v.State == Matched {
			t.Fatalf("invalid graph exposed: %+v %v", v, err)
		}
	}
	r := fixture(t)
	put(t, r, "graphify-out/graph.json", viewFixture)
	put(t, r, "graphify-out/.graphify_root", t.TempDir())
	v, _ := View(t.Context(), r)
	if len(v.Nodes) > 0 || v.State == Matched {
		t.Fatalf("borrowed graph: %+v", v)
	}
	missing, _ := View(t.Context(), t.TempDir())
	if missing.State != "missing" {
		t.Fatalf("%+v", missing)
	}
	if err := os.Remove(filepath.Join(r, "graphify-out/.graphify_root")); err != nil {
		t.Fatal(err)
	}
}
func TestViewBoundsKeepCountsAndTruncationHonest(t *testing.T) {
	r := fixture(t)
	nodes := []map[string]any{}
	links := []map[string]any{}
	for i := 0; i < ViewNodeLimit+1; i++ {
		nodes = append(nodes, map[string]any{"id": fmt.Sprint(i), "label": strings.Repeat("x", ViewTextLimit+1)})
		if i > 0 {
			links = append(links, map[string]any{"source": "0", "target": fmt.Sprint(i), "relation": "calls"})
		}
	}
	raw, _ := json.Marshal(map[string]any{"graph": map[string]any{"schema_version": 1}, "nodes": nodes, "links": links})
	put(t, r, "graphify-out/graph.json", string(raw))
	v, err := View(t.Context(), r)
	if err != nil || len(v.Nodes) != ViewNodeLimit || !v.Truncated || v.TotalNodes != ViewNodeLimit+1 || len([]rune(v.Nodes[0].Label)) != ViewTextLimit {
		t.Fatalf("%+v %v", v, err)
	}
	for _, link := range v.Links {
		if link.Confidence != "UNKNOWN" {
			t.Fatal("unproven confidence was promoted")
		}
	}
}
