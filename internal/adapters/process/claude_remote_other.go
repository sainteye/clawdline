//go:build !darwin && !linux

package process

import (
	"context"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func WorkingDirectory(context.Context, int) (string, bool)      { return "", false }
func VerifyRemoteControl(context.Context, session.Session) bool { return false }
