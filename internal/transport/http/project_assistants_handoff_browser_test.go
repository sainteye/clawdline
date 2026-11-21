package http

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandoffCardBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_HANDOFF_CARD_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_HANDOFF_CARD_ARTIFACTS and PLAYWRIGHT_MODULE")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, probe := newHandoffSource(t)
	source := httptest.NewServer(f.h)
	t.Cleanup(source.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-handoff-card.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+source.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	out, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("browser handoff: %v\n%s", err, out)
	}
	x := waitInputTurn(t, f)
	if len(x.Turns) != 7 || len(x.Messages) != 14 || len(probe.prompts) != 7 || len(probe.opens) != 7 {
		t.Fatalf("browser did not use exactly six explicit new inputs: turns=%d messages=%d prompts=%d opens=%v", len(x.Turns), len(x.Messages), len(probe.prompts), probe.opens)
	}
	for i, language := range []string{"en", "zh-Hant"} {
		quoted, kept, adopted := i*3+1, i*3+2, i*3+3
		if !probe.opens[quoted] || !probe.opens[kept] || probe.opens[adopted] || probe.opening[quoted] != "" || probe.opening[kept] != "" ||
			!strings.Contains(probe.opening[adopted], "Concurrent source edit "+language) {
			t.Fatalf("%s did not keep then adopt the exact source revision: opens=%v", language, probe.opens)
		}
	}
	t.Log(string(out))
}
