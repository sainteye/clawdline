//go:build darwin || linux

package projectsync

import (
	"context"
	"errors"
	"testing"
)

func TestOriginNamesANonRepositoryAtAFilesystemBoundary(t *testing.T) {
	// Git can append a filesystem-boundary explanation after its fatal
	// diagnostic. Classification must survive the real multi-line response.
	_, err := Origin(context.Background(), "/dev")
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("a non-repository across a filesystem boundary: %v", err)
	}
}
