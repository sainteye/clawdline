package botdevserver

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestServerLogKeepsOnlyTheBoundedLatestOutput(t *testing.T) {
	var log LogTail
	_, _ = log.Write([]byte(strings.Repeat("old", capacity.MaximumBotServerLogBytes)))
	_, _ = log.Write([]byte("\nlatest synthetic line\n"))
	got := log.Snapshot()
	if !got.Truncated || got.Bytes != capacity.MaximumBotServerLogBytes || len(got.Text) > capacity.MaximumBotServerLogBytes || !strings.HasSuffix(got.Text, "latest synthetic line\n") {
		t.Fatalf("tail=%+v", got)
	}
	_, _ = log.Write([]byte(strings.Repeat("\xffa", capacity.MaximumBotServerLogBytes)))
	got = log.Snapshot()
	if !utf8.ValidString(got.Text) || len(got.Text) > capacity.MaximumBotServerLogBytes || !got.Truncated {
		t.Fatal("invalid output escaped the text/retention bound")
	}
	var incomplete LogTail
	incomplete.incomplete()
	if got := incomplete.Snapshot(); got.Text != "" || !got.Truncated {
		t.Fatal("a failed read was presented as complete output")
	}
}

func TestServerLogConcurrentReadsDoNotBlockOrLoseTheFinalTail(t *testing.T) {
	var log LogTail
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				_, _ = log.Write([]byte("synthetic output\n"))
				_ = log.Snapshot()
			}
		}()
	}
	workers.Wait()
	_, _ = log.Write([]byte("final\n"))
	if !strings.HasSuffix(log.Snapshot().Text, "final\n") {
		t.Fatal("latest output lost")
	}
}
