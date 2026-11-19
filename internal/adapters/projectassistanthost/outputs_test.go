package projectassistanthost

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestOutputToolsUseCurrentTurnAndDurableIdempotency(t *testing.T) {
	n := startNative(t, func(f *hostFixture) { f.o.Outputs = store.Outputs{Dir: t.TempDir()} })
	path := filepath.Join(n.f.repo, "report.html")
	if err := os.WriteFile(path, []byte("<h1>First</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	notify := false
	q := app.OutputRequest{Path: path, HTML: true, Title: "Report", Notify: &notify}
	ctx := context.Background()
	one, err := n.tools.PublishOutput(ctx, "first", q)
	if err != nil || one.Quiet {
		t.Fatal("new artifacts always produce a chat card", one, err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	retry, err := n.tools.PublishOutput(ctx, "first", q)
	if err != nil || retry != one {
		t.Fatal("retry depended on source", retry, err)
	}
	changed := q
	changed.Title = "Changed"
	if _, err = n.tools.PublishOutput(ctx, "first", changed); pa.Code(err) != pa.IdempotencyConflict {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("<h1>Latest</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	q.Artifact = one.ArtifactID
	q.Title = "Latest"
	two, err := n.tools.PublishOutput(ctx, "second", q)
	if err != nil || two.ArtifactID != one.ArtifactID || two.SHA256 == one.SHA256 || !two.Quiet {
		t.Fatal(two, err)
	}
	items, err := n.tools.ListArtifacts(ctx)
	// The library adds the initial display-title revision to the immutable
	// publication; every other field must still match the latest output.
	displayed := two
	displayed.TitleVersion = 1
	if err != nil || len(items) != 1 || items[0].Output != displayed || items[0].Path == "" {
		t.Fatal(items, err)
	}
	stored, err := os.ReadFile(items[0].Path)
	if err != nil || string(stored) != "<h1>Latest</h1>" {
		t.Fatal("native latest path", err)
	}
	x, err := n.f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	versions, cards := 0, 0
	for _, m := range x.Messages {
		if m.Output != nil {
			versions++
			if !m.Output.Quiet {
				cards++
			}
		}
	}
	if versions != 2 || cards != 1 {
		t.Fatal("quiet update changed the number of artifact cards", versions, cards)
	}
	replaceNativeTurn(t, n, false)
	if _, err = n.tools.PublishOutput(ctx, "stale", q); pa.Code(err) != pa.StaleGeneration {
		t.Fatal("stale execution published", err)
	}
	x, err = n.f.h.store.Export(ctx, n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range x.Messages {
		if m.Output != nil && m.Output.ID == "stale" {
			t.Fatal("stale card")
		}
	}
}

func TestOutputStopCancelsCopyWithoutHoldingActorOrPublishingPartialCard(t *testing.T) {
	type copying struct{}
	started, release := make(chan struct{}), make(chan struct{})
	var paused atomic.Bool
	n := startNative(t, func(f *hostFixture) {
		f.o.Outputs = store.Outputs{Dir: t.TempDir()}
		source := f.o.Source
		f.o.Source = func(ctx context.Context, key pa.BotKey, write bool) (pa.SourceStatus, error) {
			if ctx.Value(copying{}) == true && !paused.Load() {
				// Pause only once the snapshot has created its own temporary file.
				// Authorization and the pre-effect journal must remain unblocked.
				copyStarted := false
				if err := filepath.WalkDir(f.o.Outputs.Dir, func(_ string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					copyStarted = copyStarted || !d.IsDir()
					return nil
				}); err != nil {
					return pa.SourceStatus{}, err
				}
				if copyStarted && paused.CompareAndSwap(false, true) {
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			}
			return source(ctx, key, write)
		}
	})
	path := filepath.Join(n.f.repo, "large.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{1}, pa.MaxOutputChunkBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := n.tools.PublishOutput(context.WithValue(context.Background(), copying{}, true), "copy", app.OutputRequest{Path: path})
		done <- err
	}()
	fixtureReceive(t, started)
	s, err := n.f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Stop(context.Background(), app.StopRequest{Command: pa.Command{Key: n.bot.Key, Operation: pa.StopOperation, IdempotencyKey: "stop-copy"}, Fence: n.prompt.Turn.Fence, TurnID: n.prompt.Turn.ID})
	close(release)
	if err != nil {
		t.Fatal("copy held the actor", err)
	}
	if err = fixtureReceive(t, done); err == nil {
		t.Fatal("stopped copy succeeded")
	}
	x, err := n.f.h.store.Export(context.Background(), n.bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range x.Messages {
		if m.Output != nil {
			t.Fatal("partial card published")
		}
	}
	var residue []string
	_ = filepath.WalkDir(n.f.o.Outputs.Dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			residue = append(residue, path)
		}
		return err
	})
	if len(residue) != 0 {
		t.Fatal("unpublished copy retained", residue)
	}
}

func TestOutputToolsRefuseRevokedSourceAndReconfiguredExecution(t *testing.T) {
	for _, mode := range []string{"source", "project", "generation"} {
		t.Run(mode, func(t *testing.T) {
			n := startNative(t, func(f *hostFixture) { f.o.Outputs = store.Outputs{Dir: t.TempDir()} })
			path := filepath.Join(n.f.repo, "file")
			if err := os.WriteFile(path, []byte("private fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "source":
				n.f.revoked.Store(true)
			case "project":
				n.f.projectChanged.Store(true)
			case "generation":
				replaceNativeTurn(t, n, true)
			}
			if _, err := n.tools.PublishOutput(context.Background(), "late", app.OutputRequest{Path: path}); err == nil {
				t.Fatal("revoked execution published")
			}
			x, err := n.f.h.store.Export(context.Background(), n.bot.Key)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range x.Messages {
				if m.Output != nil {
					t.Fatal("revoked card")
				}
			}
		})
	}
}
