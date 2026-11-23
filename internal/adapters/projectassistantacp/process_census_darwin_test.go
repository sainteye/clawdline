//go:build darwin

package projectassistantacp

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestDarwinProcessCensusIncludesCurrentBirthAndRespectsBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rows, err := processIdentities(ctx, capacity.MaximumBotProcessCensusBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.pid != os.Getpid() {
			continue
		}
		if row.ppid != os.Getppid() || row.pgid <= 0 || row.stamp == "" || deadProcess(row) {
			t.Fatalf("current process census identity: %+v", row)
		}
		if _, err := processIdentities(ctx, 1); !errors.Is(err, io.ErrShortBuffer) {
			t.Fatalf("oversized census result: %v", err)
		}
		return
	}
	t.Fatal("current process was missing from the census")
}
