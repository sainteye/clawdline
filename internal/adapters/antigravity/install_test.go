package antigravity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func installWorkspace(t *testing.T, data string) string {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if data != "" {
		if err = os.MkdirAll(filepath.Join(workspace, ".agents"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(workspace, ".agents", "hooks.json"), []byte(data), 0640); err != nil {
			t.Fatal(err)
		}
	}
	return workspace
}
func TestInstallPreviewMergeAndUninstall(t *testing.T) {
	unrelated := `{ "PreInvocation": [{"command": "keep me", "timeout": 7}] }`
	workspace := installWorkspace(t, `{"other":`+unrelated+`,"statusLine":{"command":"keep-status"},"future": [1, 2]}`)
	path := filepath.Join(workspace, ".agents", "hooks.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PreviewInstall(workspace, "/synthetic/helper with ' quote", false)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(before, unchanged) {
		t.Fatal("preview mutated configuration")
	}
	if !bytes.Contains(p.After, []byte(unrelated)) {
		t.Fatal("third-party block bytes changed")
	}
	if err = ApplyInstall(p); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rawObject(installed)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := rawObject(root["clawdline"])
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "PreToolUse", "PostToolUse", "Stop"} {
		if !json.Valid(owned[event]) {
			t.Fatalf("missing native hook event %s", event)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("file mode changed: %v %v", info, err)
	}
	p, err = PreviewInstall(workspace, p.Helper, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyInstall(p); err != nil {
		t.Fatal(err)
	}
	p, err = PreviewInstall(workspace, p.Helper, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyInstall(p); err != nil {
		t.Fatal(err)
	}
	removed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err = rawObject(removed)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := root["clawdline"]; exists {
		t.Fatal("owned hooks survived uninstall")
	}
	if !bytes.Equal(root["other"], []byte(unrelated)) || string(root["statusLine"]) != `{"command":"keep-status"}` || string(root["future"]) != `[1, 2]` {
		t.Fatal("unowned settings changed")
	}
}
func TestInstallConflictsAndDisabledIntegration(t *testing.T) {
	workspace := installWorkspace(t, `{"clawdline":{"enabled":false,"unknown":17},"other":{}}`)
	path := filepath.Join(workspace, ".agents", "hooks.json")
	before, _ := os.ReadFile(path)
	p, err := PreviewInstall(workspace, "/synthetic/helper", false)
	if err != nil || !p.Disabled {
		t.Fatalf("disabled: %+v %v", p, err)
	}
	if err = ApplyInstall(p); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("disabled integration changed")
	}
	workspace = installWorkspace(t, `{"other":{}}`)
	path = filepath.Join(workspace, ".agents", "hooks.json")
	p, err = PreviewInstall(workspace, "/synthetic/helper", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(`{"other":{"changed":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, ApplyInstall(p), "antigravity_config_changed")
	workspace = installWorkspace(t, `{"clawdline":{"PreInvocation":[{"command":"foreign"}]}}`)
	_, err = PreviewInstall(workspace, "/synthetic/helper", false)
	wantCode(t, err, "antigravity_owned_block_conflict")
	for _, bad := range []string{`null`, `[]`, `{invalid`, `{"x":1,"x":2}`} {
		workspace = installWorkspace(t, bad)
		_, err = PreviewInstall(workspace, "/synthetic/helper", false)
		if err == nil {
			t.Fatalf("accepted invalid configuration %s", bad)
		}
	}
}
func TestInstallRefusesSymlinkAndForgedPreview(t *testing.T) {
	workspace := installWorkspace(t, "")
	p, err := PreviewInstall(workspace, "/synthetic/helper", false)
	if err != nil {
		t.Fatal(err)
	}
	p.After = []byte(`{"clawdline":{"disabled":false}}`)
	wantCode(t, ApplyInstall(p), "antigravity_config_changed")
	workspace = installWorkspace(t, "")
	other := t.TempDir()
	if err = os.Symlink(other, filepath.Join(workspace, ".agents")); err != nil {
		t.Fatal(err)
	}
	_, err = PreviewInstall(workspace, "/synthetic/helper", false)
	wantCode(t, err, "antigravity_config_symlink")
}

func TestInstallDigestDetectsWhitespaceOnlyEditorChange(t *testing.T) {
	workspace := installWorkspace(t, `{"other":{}}`)
	p, err := PreviewInstall(workspace, "/synthetic/helper", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p.Path, []byte(" {\"other\":{}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, ApplyInstall(p), "antigravity_config_changed")
}
