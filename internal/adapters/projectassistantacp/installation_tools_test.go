package projectassistantacp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestSupportedFloorsCompareNumerically(t *testing.T) {
	for _, c := range []struct {
		have, floor string
		want        bool
	}{
		{"2.1.325 (Claude Code)", "2.1.323", true}, {"2.1.323", "2.1.323", true}, {"2.1.322", "2.1.323", false},
		{"2.10.0", "2.9.9", true}, {"v24.1.0", "22.0.0", true}, {"v20.19.0", "22.0.0", false}, {"unknown", "22.0.0", false},
	} {
		if got := versionAtLeast(c.have, c.floor); got != c.want {
			t.Errorf("versionAtLeast(%q, %q) = %v", c.have, c.floor, got)
		}
	}
}

func fakeTool(t *testing.T, dir, name, output string) {
	t.Helper()
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	script := "#!/bin/sh\necho '" + output + "'\n"
	if e := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
}

func TestToolsAreTheInstalledOnesAtOrAboveTheirFloors(t *testing.T) {
	ctx := context.Background()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	old, current, cliOld, cliNew := filepath.Join(root, "old"), filepath.Join(root, "current"), filepath.Join(root, "cli-old"), filepath.Join(root, "cli-new")
	fakeTool(t, old, "node", "v20.0.0")
	fakeTool(t, old, "npm", "10.0.0")
	if _, e := findTools(ctx, root, "person", []string{old}); pa.Code(e) != pa.ProviderVersionUnsupported || !strings.Contains(e.Error(), "native_node_version") {
		t.Fatalf("old Node was not reported as unsupported: %v", e)
	}
	fakeTool(t, current, "node", "v24.1.0")
	if _, e := findTools(ctx, root, "person", []string{current}); pa.Code(e) != pa.CLINotInstalled || !strings.Contains(e.Error(), "native_node") {
		t.Fatalf("Node without npm was accepted: %v", e)
	}
	fakeTool(t, current, "npm", "11.3.0")
	if _, e := findTools(ctx, root, "person", []string{old, current}); pa.Code(e) != pa.CLINotInstalled || !strings.Contains(e.Error(), "native_cli") {
		t.Fatalf("missing Claude Code was not reported: %v", e)
	}
	fakeTool(t, cliOld, "claude", "2.1.322 (Claude Code)")
	if _, e := findTools(ctx, root, "person", []string{old, current, cliOld}); pa.Code(e) != pa.ProviderVersionUnsupported || !strings.Contains(e.Error(), "native_cli_version") {
		t.Fatalf("old Claude Code was not reported as unsupported: %v", e)
	}
	fakeTool(t, cliNew, "claude", "2.1.325 (Claude Code)")
	tools, e := findTools(ctx, root, "person", []string{old, current, cliOld, cliNew})
	if e != nil {
		t.Fatal(e)
	}
	if tools.Node != filepath.Join(current, "node") || tools.CLI != filepath.Join(cliNew, "claude") || tools.CLIVersion != "2.1.325" || tools.NodeVersion != "24.1.0" {
		t.Fatalf("wrong tools: %+v", tools)
	}
}

func TestAnUpdatedClaudeLauncherIsFollowed(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	bin, versions := filepath.Join(root, "bin"), filepath.Join(root, "versions")
	fakeTool(t, bin, "node", "v24.1.0")
	fakeTool(t, bin, "npm", "11.3.0")
	fakeTool(t, versions, "2.1.323", "2.1.323 (Claude Code)")
	fakeTool(t, versions, "2.1.330", "2.1.330 (Claude Code)")
	launcher := filepath.Join(bin, "claude")
	if e := os.Symlink(filepath.Join(versions, "2.1.323"), launcher); e != nil {
		t.Fatal(e)
	}
	first, e := findTools(context.Background(), root, "person", []string{bin})
	if e != nil || first.CLIVersion != "2.1.323" {
		t.Fatal(first, e)
	}
	// Claude Code's updater repoints its launcher; nothing here was pinned to it.
	if e = os.Remove(launcher); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(filepath.Join(versions, "2.1.330"), launcher); e != nil {
		t.Fatal(e)
	}
	second, e := findTools(context.Background(), root, "person", []string{bin})
	if e != nil || second.CLIVersion != "2.1.330" || second.CLI != filepath.Join(versions, "2.1.330") {
		t.Fatal(second, e)
	}
}

func TestVersionOutputOverItsBoundIsRefused(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", capacity.MaximumBotToolProbeBytes) + " 2.1.325"
	script := "#!/bin/sh\nprintf '%s' '" + long + "'\n"
	if e := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := probeVersion(context.Background(), filepath.Join(dir, "claude"), []string{"PATH=/usr/bin:/bin"}); e == nil {
		t.Fatal("unbounded version output was accepted")
	}
}

func TestAnAdapterBuildThatDiffersFromTheSealIsDiscarded(t *testing.T) {
	state, bin := t.TempDir(), t.TempDir()
	fakeTool(t, bin, "node", "v24.1.0")
	// A stand-in npm writes a tree that is not the bundled lockfile's.
	npm := "#!/bin/sh\ntest -f package-lock.json || exit 3\nmkdir -p node_modules/@agentclientprotocol/claude-agent-acp/dist\necho changed > node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js\n"
	if e := os.WriteFile(filepath.Join(bin, "npm"), []byte(npm), 0700); e != nil {
		t.Fatal(e)
	}
	tools := nativeTools{Node: filepath.Join(bin, "node"), NPM: filepath.Join(bin, "npm")}
	if _, e := installAdapter(context.Background(), state, tools, state, "person"); pa.Code(e) != pa.ProviderVersionUnsupported || !strings.Contains(e.Error(), "native_adapter_digest") {
		t.Fatalf("unsealed adapter tree was accepted: %v", e)
	}
	entries, e := os.ReadDir(filepath.Dir(adapterDirectory(state)))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused build left files: %v", entries)
	}
}

func TestTheBundledLockfileOmitsNoPackageTheSealCovers(t *testing.T) {
	data, e := claudeAdapterAssets.ReadFile("assets/claude-agent-acp/package-lock.json")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), `"node_modules/@agentclientprotocol/claude-agent-acp": {`) || !strings.Contains(string(data), `"version": "`+ClaudeAdapterVersion+`"`) {
		t.Fatal("bundled lockfile does not name the supplied adapter version")
	}
}
