//go:build !darwin

package clauderemote

import (
	"context"
	"errors"
)

func keychainCredentials(context.Context) ([]byte, error) {
	return nil, errors.New("Claude credentials are unavailable")
}
