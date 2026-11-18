//go:build darwin || linux

package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestUnknownResourceMeasurementDoesNotStartLegacyCleanup(t *testing.T) {
	b, ctx, r, cache := cacheTask(t, "%child", true)
	enableTestResources(t, b)
	marker := filepath.Join(cache, "retained")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	prior := b.Reading
	b.Reading = func(ctx context.Context) session.Inventory { reads++; return prior(ctx) }
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		return taskresource.Usage{}, errors.New("source measurement unavailable")
	}
	err := b.AdmitHeavyResources(ctx)
	if refusalCode(err) != "resource_legacy_measurement_unknown" {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Fatalf("unknown measurement triggered %d cleanup inventory reads", reads)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "keep" {
		t.Fatal("unknown admission changed retained legacy data", r.ID, err)
	}
}
