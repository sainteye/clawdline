//go:build darwin

package terminal

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (t *ITerm) NativeTerminalIdentity(ctx context.Context, s session.Session) (string, error) {
	if !itermMayAct(ctx) {
		return "", ITermScanOff{Op: "operate the Session terminal"}
	}
	return nativeTerminalIdentity(s)
}
