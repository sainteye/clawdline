package graphify

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	var err error
	r, err = filepath.EvalSymlinks(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(r, "graphify-out"), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, r, "source.go", "package demo\nfunc A() { B() }\nfunc B() {}\n")
	put(t, r, "graphify-out/.graphify_root", r)
	put(t, r, "graphify-out/graph.json", `{"graph":{"schema_version":1},"nodes":[],"links":[]}`)
	sum := md5.Sum([]byte("package demo\nfunc A() { B() }\nfunc B() {}\n"))
	data, err := json.Marshal(map[string]any{"source.go": map[string]any{"ast_hash": hex.EncodeToString(sum[:])}})
	if err != nil {
		t.Fatal(err)
	}
	put(t, r, "graphify-out/manifest.json", string(data))
	return r
}

func put(t *testing.T, r, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, r string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(r, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMatchedCheckIsReadOnly(t *testing.T) {
	r := fixture(t)
	before := snapshot(t, r)
	s, err := Inspect(context.Background(), r)
	if err != nil || s.State != Matched || s.Bytes == 0 || s.Entries == 0 {
		t.Fatalf("%+v %v", s, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, r)) {
		t.Fatal("inspection wrote files")
	}
}

func TestChangedSourcesNeverMatchTheOldGraph(t *testing.T) {
	for _, kind := range []string{"same-mtime-edit", "add", "hidden-add", "delete", "rename"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture(t)
			p := filepath.Join(r, "source.go")
			st, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "same-mtime-edit":
				put(t, r, "source.go", "package demo\nfunc A() {}\n")
				if err := os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "add":
				put(t, r, "new.ts", "export function New() {}")
			case "hidden-add":
				if err := os.Mkdir(filepath.Join(r, ".github"), 0700); err != nil {
					t.Fatal(err)
				}
				put(t, r, ".github/new.yml", "name: changed")
			case "delete":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			case "rename":
				if err := os.Rename(p, filepath.Join(r, "renamed.go")); err != nil {
					t.Fatal(err)
				}
			}
			s, err := Inspect(context.Background(), r)
			if err != nil || s.State == Matched {
				t.Fatalf("%+v %v", s, err)
			}
		})
	}
}

func TestMissingMalformedAndBorrowedIndexesFallBack(t *testing.T) {
	for _, kind := range []string{"missing", "bad-graph", "bad-manifest", "legacy-manifest", "wrong-root", "custom-inputs", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture(t)
			switch kind {
			case "missing":
				if err := os.RemoveAll(filepath.Join(r, "graphify-out")); err != nil {
					t.Fatal(err)
				}
			case "bad-graph":
				put(t, r, "graphify-out/graph.json", "{broken")
			case "bad-manifest":
				put(t, r, "graphify-out/manifest.json", `{"../foreign.go":{"ast_hash":"00000000000000000000000000000000"}}`)
			case "legacy-manifest":
				put(t, r, "graphify-out/manifest.json", `{"source.go":1}`)
			case "wrong-root":
				put(t, r, "graphify-out/.graphify_root", t.TempDir())
			case "custom-inputs":
				put(t, r, ".graphifyrc", "language.tpl=go")
			case "oversized":
				f, err := os.OpenFile(filepath.Join(r, "graphify-out/graph.json"), os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(MaxMetadataBytes + 1)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			s, err := Inspect(context.Background(), r)
			if err != nil || s.State == Matched {
				t.Fatalf("%+v %v", s, err)
			}
		})
	}
}

func TestLaunchGuidanceDoesNotBuildOrClaimFreshness(t *testing.T) {
	r := fixture(t)
	before := snapshot(t, r)
	text, err := LaunchInstructions(context.Background(), r)
	if err != nil || !strings.Contains(text, "before EVERY") || !strings.Contains(text, "clawdline graphify check .") || !strings.Contains(text, "GRAPHIFY_BACKEND=none") || !strings.Contains(text, "Do not install") {
		t.Fatalf("%q %v", text, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, r)) {
		t.Fatal("launch wrote files")
	}
	if err := os.RemoveAll(filepath.Join(r, "graphify-out")); err != nil {
		t.Fatal(err)
	}
	text, err = LaunchInstructions(context.Background(), r)
	if err != nil || !strings.Contains(text, "Use ordinary source search") {
		t.Fatalf("%q %v", text, err)
	}
}

func TestCanceledCheckReturnsWithoutUsingAnIndex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := Inspect(ctx, t.TempDir())
	if err == nil || s.State == Matched {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err = LaunchInstructions(ctx, t.TempDir()); err == nil {
		t.Fatal("canceled launch accepted")
	}
}

func TestRootAliasAndContentHashDoNotDependOnFileTime(t *testing.T) {
	r := fixture(t)
	p := filepath.Join(r, "source.go")
	old := time.Unix(1, 0)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	s, err := Inspect(context.Background(), filepath.Join(r, "."))
	if err != nil || s.State != Matched {
		t.Fatalf("%+v %v", s, err)
	}
}

// An explicit local probe can read an independently produced native index.
// Ordinary tests use hermetic fixtures and never discover user directories.
func TestExplicitNativeGraphifyPilot(t *testing.T) {
	r := os.Getenv("CLAWDLINE_GRAPHIFY_PILOT_ROOT")
	if r == "" {
		t.Skip("no explicit native pilot root")
	}
	s, err := Inspect(context.Background(), r)
	if err != nil || s.State != Matched {
		t.Fatalf("native index: %+v %v", s, err)
	}
	t.Logf("native graph: %s; entries=%d bytes=%d", s.State, s.Entries, s.Bytes)
}
