//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModeConfigUsesNativeCheckedChannel(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	ready(t, c)
	checks, applied := 0, 0
	if err := c.SetConfigOptionChecked(context.Background(), "mode", "auto_edit", func() error { checks++; return nil }, func(raw json.RawMessage) error { applied++; return nil }); err != nil {
		t.Fatal(err)
	}
	if checks != 1 || applied != 1 {
		t.Fatal(checks, applied)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "wire"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"configId":"mode"`) || !strings.Contains(string(raw), `"value":"auto_edit"`) || strings.Contains(string(raw), `"session/prompt"`) {
		t.Fatalf("wire=%s", raw)
	}
	snapshot := c.ConfigOptions()
	if !strings.Contains(string(snapshot), `"currentValue":"low"`) {
		t.Fatalf("native ACK snapshot=%s", snapshot)
	}
	snapshot[0] = 'X'
	if c.ConfigOptions()[0] == 'X' {
		t.Fatal("native snapshot aliases process state")
	}
	settings(t, dir, `{"useG1Credits":true,"auth":{"type":"oauth-personal"}}`)
	requireError(t, c.SetConfigOptionChecked(context.Background(), "mode", "yolo", nil, nil), "personal_settings_required", NotWritten)
	if got := methods(t, dir); len(got) != 4 {
		t.Fatal("unsafe mode reached peer", got)
	}
}
