//go:build !darwin && !linux

package process

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func WorkingDirectory(context.Context, int) (string, bool)      { return "", false }
func VerifyRemoteControl(context.Context, session.Session) bool { return false }

func RemoteControlGone(context.Context, session.Session) (bool, error) {
	return false, errors.New("native process verification is unavailable")
}
