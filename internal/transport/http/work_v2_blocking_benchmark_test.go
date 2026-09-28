package http

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// TestBoardBlockingLoadDistribution is an opt-in, deterministic comparison of
// the old request shape and the current one. Each sample starts eight Board
// reads and one create together while a terminal-backed Project catalog costs
// 10 ms. "legacy" spells out the former per-card relation and Project reads,
// including response projection inside the create transaction. "batched"
// uses the production page and pre-transaction projection paths.
//
// Run with:
//
//	CLAWDLINE_PERF_BOARD_BLOCKING=1 go test ./internal/transport/http \
//	  -run TestBoardBlockingLoadDistribution -count=1 -v
func TestBoardBlockingLoadDistribution(t *testing.T) {
	if os.Getenv("CLAWDLINE_PERF_BOARD_BLOCKING") != "1" {
		t.Skip("set CLAWDLINE_PERF_BOARD_BLOCKING=1 to run the distribution")
	}
	for _, mode := range []string{"legacy", "batched"} {
		t.Run(mode, func(t *testing.T) {
			s, _, project := sessionItemServer(t)
			for n := 0; n < app.WorkV2ListPageLimit; n++ {
				if _, err := s.workV2().Create(context.Background(), app.NewWorkV2{
					ProjectID: project, ProjectPath: "/project", Kind: work.KindIssue,
					Title: fmt.Sprintf("Seed %02d", n), Description: "A benchmark row.", Actor: "benchmark",
				}, nil); err != nil {
					t.Fatal(err)
				}
			}
			base := s.inventory.Read(context.Background())
			s.readings = app.NewInventoryReading(func(ctx context.Context) session.Inventory {
				select {
				case <-time.After(10 * time.Millisecond):
				case <-ctx.Done():
				}
				return base
			}, time.Nanosecond)

			samples := make([]time.Duration, 12)
			for sample := range samples {
				start := make(chan struct{})
				errs := make(chan error, 9)
				var group sync.WaitGroup
				read := func() error {
					if mode == "batched" {
						page, err := s.workV2().ListPage(context.Background(), "", "open", "", "")
						if err == nil {
							itemOf := s.workV2ItemProjector(context.Background())
							for _, row := range page.Rows {
								_ = itemOf(row)
							}
						}
						return err
					}
					items, _, err := s.store.WorkV2ItemsPage(context.Background(), "", "", "open", "", 0, "", app.WorkV2ListPageLimit)
					if err != nil {
						return err
					}
					for _, item := range items {
						if _, err = s.store.WorkV2Documents(context.Background(), item.ID); err != nil {
							return err
						}
						if _, err = s.store.WorkV2Images(context.Background(), item.ID); err != nil {
							return err
						}
						if _, err = s.store.WorkV2Steps(context.Background(), item.ID); err != nil {
							return err
						}
						if _, err = s.store.WorkV2ActiveClaim(context.Background(), item.ID); err != nil {
							return err
						}
						_ = s.workV2ItemOf(s.workV2Projects(context.Background()), app.WorkV2View{Item: item})
					}
					return nil
				}
				for range 8 {
					group.Add(1)
					go func() {
						defer group.Done()
						<-start
						errs <- read()
					}()
				}
				group.Add(1)
				go func(n int) {
					defer group.Done()
					<-start
					catalog := s.workV2Projects(context.Background())
					_, err := s.workV2().Create(context.Background(), app.NewWorkV2{
						ProjectID: project, ProjectPath: "/project", Kind: work.KindIssue,
						Title: fmt.Sprintf("Write %s %02d", mode, n), Description: "A concurrent write.", Actor: "benchmark",
					}, func(v app.WorkV2View) (key store.ReceiptKey, answer store.ReceiptAnswer, complete bool) {
						if mode == "legacy" {
							_ = s.workV2ItemOf(s.workV2Projects(context.Background()), v)
						} else {
							_ = s.workV2ItemOf(catalog, v)
						}
						return key, answer, false
					})
					errs <- err
				}(sample)

				began := time.Now()
				close(start)
				group.Wait()
				samples[sample] = time.Since(began)
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
			median := samples[len(samples)/2]
			p95 := samples[len(samples)-1]
			t.Logf("mode=%s load=8-read+1-write samples=%d median=%s p95=%s all=%v",
				mode, len(samples), median, p95, samples)
		})
	}
}
