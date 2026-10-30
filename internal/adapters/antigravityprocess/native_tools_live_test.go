//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

// Explicitly opted-in, metadata-only history diagnostics. Exactly initialize,
// authenticate and load; no new Session or prompt, no automatic retry. Private
// captures belong outside the repository and are never printed by this test.
func TestGeminiNativeHistoryToolCapture(t *testing.T) {
	config, id, cwd, path := os.Getenv("CLAWDLINE_GEMINI_FREE_RESUME_TEST_CONFIG"), os.Getenv("CLAWDLINE_GEMINI_NATIVE_HISTORY_ID"), os.Getenv("CLAWDLINE_GEMINI_NATIVE_HISTORY_CWD"), os.Getenv("CLAWDLINE_GEMINI_NATIVE_HISTORY_CAPTURE")
	if config == "" || id == "" || cwd == "" || path == "" {
		t.Skip("explicit native history capture inputs required")
	}
	raw, err := os.ReadFile(config)
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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("new private capture path required")
	}
	defer f.Close()
	life, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var mu sync.Mutex
	counts := map[string]int{}
	c, err := Start(life, Options{Executable: cfg.Executable, AccountDir: cfg.AccountDir, Args: cfg.Args, WorkingDir: cwd, MaxFrameBytes: 1 << 20, CallTimeout: 45 * time.Second, OnUpdate: func(ctx context.Context, frame json.RawMessage) error {
		mu.Lock()
		defer mu.Unlock()
		var m struct {
			Params struct {
				Update struct {
					Kind string `json:"sessionUpdate"`
				} `json:"update"`
			} `json:"params"`
		}
		_ = json.Unmarshal(frame, &m)
		counts[m.Params.Update.Kind]++
		_, err := f.Write(append(append([]byte{}, frame...), '\n'))
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = c.Initialize(life); err != nil {
		t.Fatal(err)
	}
	if _, err = c.AuthenticatePersonal(life); err != nil {
		t.Fatal(err)
	}
	if _, err = c.LoadSession(life, id); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("metadata RPCs=3; model prompts=0; update counts=%v", counts)
}
