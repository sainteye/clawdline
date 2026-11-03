//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Optional setup diagnostics make no model request and print no native payload.
func TestGeminiPersonalNativeSetupStages(t *testing.T) {
	path := os.Getenv("CLAWDLINE_GEMINI_FREE_RESUME_TEST_CONFIG")
	if path == "" {
		t.Skip("explicit personal CLI probe configuration required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("probe configuration unavailable")
	}
	var cfg struct {
		Executable, AccountDir string
		Args                   []string
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("invalid probe configuration")
	}
	life, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c, err := Start(life, Options{Executable: cfg.Executable, AccountDir: cfg.AccountDir, Args: cfg.Args, WorkingDir: t.TempDir(), MaxFrameBytes: 1 << 20, CallTimeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, stage := range []struct {
		name string
		call func(context.Context) (json.RawMessage, error)
	}{{"initialize", c.Initialize}, {"authenticate", c.AuthenticatePersonal}, {"new", c.NewSession}} {
		started := time.Now()
		t.Log("starting", stage.name)
		if _, err := stage.call(life); err != nil {
			t.Fatalf("%s after %s: %v", stage.name, time.Since(started), err)
		}
		t.Log(stage.name, "completed", time.Since(started))
	}
	t.Log("metadata RPCs=3; model prompts=0; personal session setup verified")
}
