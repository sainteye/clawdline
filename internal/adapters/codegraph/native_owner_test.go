package codegraph

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodeGraphNativeOwnerEOF(t *testing.T) {
	cache := os.Getenv("CODEGRAPH_NATIVE_TEST_CACHE")
	if cache == "" {
		t.Skip("set CODEGRAPH_NATIVE_TEST_CACHE for pinned native acceptance")
	}
	b := testBuilder(t, nil, nil)
	b.cache = cache
	rt, err := b.runtime(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"serve", "build"} {
		t.Run(mode, func(t *testing.T) {
			root := fixtureRoot(t)
			root, err = canonical(root)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "serve" {
				os.Mkdir(filepath.Join(root, ".codegraph"), 0700)
				raw, _ := json.Marshal(map[string]string{"root": root, "version": Version})
				os.WriteFile(filepath.Join(root, ownerFile), raw, 0600)
			}
			bundle := t.TempDir()
			lib := filepath.Join(bundle, "lib", "dist")
			put := func(name, text string) {
				p := filepath.Join(lib, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			method := "open"
			if mode == "build" {
				method = "init"
			}
			put("index.js", fmt.Sprintf(`module.exports=require(%q);const original=module.exports.CodeGraph.%s;module.exports.CodeGraph.%s=async function(...args){console.error('STARTUP_ENTERED');await new Promise(r=>setTimeout(r,10000));return original.apply(this,args)};`, filepath.Join(rt.Bundle, "lib", "dist", "index.js"), method, method))
			for _, name := range []string{"extraction/index.js", "file-limits.js", "extraction/grammars.js", "project-config.js", "mcp/tools.js"} {
				put(name, fmt.Sprintf("module.exports=require(%q);", filepath.Join(rt.Bundle, "lib", "dist", filepath.FromSlash(name))))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, rt.Node, "--liftoff-only", rt.Bridge, bundle, mode, root, bridgeLimits())
			cmd.Env = parserEnvironment(t.TempDir())
			cmd.Stdout = io.Discard
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{}, 1)
			go func() {
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					if strings.Contains(scanner.Text(), "STARTUP_ENTERED") {
						select {
						case entered <- struct{}{}:
						default:
						}
					}
				}
			}()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("SDK startup exited before barrier: %v", err)
			case <-ctx.Done():
				cancel()
				<-done
				t.Fatal("SDK startup barrier not reached")
			}
			input.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				cancel()
				<-done
				t.Fatal("owner EOF left native startup running")
			}
		})
	}
}

func TestCodeGraphNativeSupportedSourceReceipt(t *testing.T) {
	cache := os.Getenv("CODEGRAPH_NATIVE_TEST_CACHE")
	if cache == "" {
		t.Skip("set CODEGRAPH_NATIVE_TEST_CACHE")
	}
	b := testBuilder(t, nil, nil)
	b.cache = cache
	rt, err := b.runtime(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), rt.Node, "-e", `const g=require(process.argv[1]);process.stdout.write(JSON.stringify({extensions:Object.keys(g.EXTENSION_MAP),languages:[...new Set([...g.getSupportedLanguages(),...Object.values(g.EXTENSION_MAP)])].filter(x=>g.isLanguageSupported(x))}))`, filepath.Join(rt.Bundle, "lib", "dist", "extraction", "grammars.js"))
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var native struct{ Extensions, Languages []string }
	if err := json.Unmarshal(raw, &native); err != nil {
		t.Fatal(err)
	}
	for _, ext := range native.Extensions {
		if !sourceName("source"+ext, extensionSet()) {
			t.Errorf("pinned extension omitted: %s", ext)
		}
	}
	for _, language := range native.Languages {
		found := false
		for _, got := range sourceTypes.Languages {
			found = found || got == language
		}
		if !found {
			t.Errorf("pinned language omitted: %s", language)
		}
	}
}

func TestCodeGraphNativeConfiguredFormats(t *testing.T) {
	cache := os.Getenv("CODEGRAPH_NATIVE_TEST_CACHE")
	if cache == "" {
		t.Skip("set CODEGRAPH_NATIVE_TEST_CACHE")
	}
	root := t.TempDir()
	files := map[string]string{
		"codegraph.json": `{"extensions":{".own":"python"}}`,
		"custom.own":     "def CustomOld():\n    return 1\n",
		"custom.two":     "def CustomNew():\n    return 2\n",
		"window.pyw":     "def WindowEntry():\n    return 1\n",
		"types.hxx":      "int HeaderEntry();\n",
		"mapper.xml":     `<mapper namespace="test.Mapper"><select id="find">SELECT 1</select></mapper>`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBuilder(cache, func() bool { return true }, nil)
	defer b.Close(context.Background())
	if _, err := b.Start(root, "formats"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	for b.Status(root).State == "running" {
		if time.Now().After(deadline) {
			t.Fatal("formats build timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := b.Status(root); s.State != "succeeded" {
		t.Fatal(s)
	}
	svc, err := b.ensureService(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	var status serviceStatus
	if err := svc.call(t.Context(), "status", "", &status); err != nil || status.State != Matched {
		t.Fatal(status, err)
	}
	// Changing a custom source with its old timestamp must invalidate the graph.
	custom := filepath.Join(root, "custom.own")
	info, _ := os.Stat(custom)
	os.WriteFile(custom, []byte("def CustomNow():\n    return 1\n"), 0600)
	os.Chtimes(custom, info.ModTime(), info.ModTime())
	if s, err := Inspect(t.Context(), root); err != nil || s.State == Matched {
		t.Fatal("custom edit escaped source check", s, err)
	}
	if err := svc.call(t.Context(), "sync", "", &status); err != nil || status.State != Matched {
		t.Fatal(status, err)
	}
	// Swap equal-length extension mappings without changing mtime. Both native
	// scan and the independent inventory must re-read and prune the old mapping.
	config := filepath.Join(root, "codegraph.json")
	info, _ = os.Stat(config)
	os.WriteFile(config, []byte(`{"extensions":{".two":"python"}}`), 0600)
	os.Chtimes(config, info.ModTime(), info.ModTime())
	if err := svc.call(t.Context(), "sync", "", &status); err != nil || status.State != Matched {
		t.Fatal(status, err)
	}
	db, err := openStore(t.Context(), filepath.Join(root, ".codegraph"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"custom.two", "window.pyw", "types.hxx", "mapper.xml"} {
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM files WHERE path=?`, name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("native format %s count=%d error=%v", name, count, err)
		}
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM files WHERE path='custom.own'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("old extension not pruned", count, err)
	}
}
