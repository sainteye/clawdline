//go:build darwin || linux

package terminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAntigravityFirstInputChecksAgainBetweenBufferAndPaste(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "ready"
		if revoke {
			name = "revoked-after-buffer"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "effects")
			binary := filepath.Join(dir, "inert-tmux")
			t.Setenv("AG_SYNTHETIC_EFFECT_LOG", log)
			script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$AG_SYNTHETIC_EFFECT_LOG\"\nif [ \"$1\" = load-buffer ]; then cat > /dev/null; fi\n"
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			host := &Tmux{Binary: binary}
			s := session.Session{ID: "%42", Backend: session.BackendTmux, Assistant: session.AssistantAntigravity}
			checks := 0
			check := func(context.Context) error {
				checks++
				if revoke && checks >= 2 {
					return errors.New("synthetic authorization revoked")
				}
				return nil
			}
			err := host.TypeAntigravityFirst(context.Background(), s, "private synthetic text", check)
			if revoke {
				if err == nil {
					t.Fatal("revoked paste admitted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err = host.EnterAntigravityFirst(context.Background(), s, check); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := "load-buffer\npaste-buffer\nsend-keys\n"
			wantChecks := 3
			if revoke {
				want = "load-buffer\ndelete-buffer\n"
				wantChecks = 2
			}
			if string(data) != want || checks != wantChecks || strings.Contains(string(data), "private synthetic text") {
				t.Fatalf("effects=%q checks=%d", data, checks)
			}
		})
	}
}
