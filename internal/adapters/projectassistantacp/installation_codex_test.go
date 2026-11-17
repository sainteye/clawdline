package projectassistantacp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func fakeCodexFeatures(t *testing.T, line string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\ncat <<'OUT'\napps                                     stable             true\n" + line + "\nshell_tool                               stable             true\nOUT\n"
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestCodexMemoriesAreReadFromCodexItself(t *testing.T) {
	ctx, env := context.Background(), []string{"PATH=/usr/bin:/bin"}
	if on, e := codexMemoriesOn(ctx, fakeCodexFeatures(t, "memories                                 stable             false"), env); e != nil || on {
		t.Fatalf("off: %v %v", on, e)
	}
	if on, e := codexMemoriesOn(ctx, fakeCodexFeatures(t, "memories                                 stable             true"), env); e != nil || !on {
		t.Fatalf("on: %v %v", on, e)
	}
	if _, e := codexMemoriesOn(ctx, fakeCodexFeatures(t, "something_else                           stable             false"), env); pa.Code(e) != pa.MemoryIsolationUnverified {
		t.Fatalf("an absent memories line was admitted: %v", e)
	}
}

func TestTheInstalledProviderRoutesEachAgentToItsOwnSetup(t *testing.T) {
	state := t.TempDir()
	provider := InstalledProvider(state)
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex} {
		// No tools are searchable in tests, so each agent stops at its own CLI.
		_, e := provider.Capabilities(context.Background(), pa.Bot{Agent: agent})
		if pa.Code(e) != pa.CLINotInstalled && pa.Code(e) != pa.Unsupported {
			t.Fatalf("%s: %v", agent, e)
		}
	}
	if _, e := provider.Capabilities(context.Background(), pa.Bot{Agent: "other"}); pa.Code(e) != pa.AdapterNotFound {
		t.Fatalf("an unknown agent was admitted: %v", e)
	}
	entries, _ := os.ReadDir(state)
	if len(entries) != 0 {
		t.Fatal("a refused setup changed state")
	}
}

func TestTheCodexLockfileNamesTheSuppliedAdapter(t *testing.T) {
	data, e := claudeAdapterAssets.ReadFile("assets/codex-acp/package-lock.json")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), `"node_modules/@agentclientprotocol/codex-acp": {`) || !strings.Contains(string(data), `"version": "`+CodexAdapterVersion+`"`) {
		t.Fatal("bundled lockfile does not name the supplied Codex adapter")
	}
}

func TestManagedCodexMemoryProbeUsesOnlyItsSelectedHome(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(second, "memories-enabled"), []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	script := filepath.Join(root, "codex")
	if e := os.WriteFile(script, []byte("#!/bin/sh\nif [ -n \"$CODEX_HOME\" ] && [ -f \"$CODEX_HOME/memories-enabled\" ]; then echo 'memories stable true'; else echo 'memories stable false'; fi\n"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		home string
		on   bool
	}{{first, false}, {second, true}, {"", false}} {
		env := []string{"PATH=/usr/bin:/bin"}
		if test.home != "" {
			env = append(env, "CODEX_HOME="+test.home)
		}
		on, e := codexMemoriesOn(context.Background(), script, env)
		if e != nil || on != test.on {
			t.Fatalf("selected home memory policy: %t %v", on, e)
		}
	}
}
