package main

// Held-out check, copied into the run's working copy after the session ends.
// Never shown to the model.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

func heldoutGet(t *testing.T, url string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b
}

// The report for a fixed store must be byte-identical to the original service's.
func TestHeldoutGolden(t *testing.T) {
	for _, size := range [][2]int{{7, 60}, {300, 1000}} {
		srv := httptest.NewServer(NewServer(NewStore(size[0], size[1])))
		body := heldoutGet(t, srv.URL+"/report")
		srv.Close()
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		want := os.Getenv("HELDOUT_GOLDEN_" + strconv.Itoa(size[1]))
		if want == "" {
			t.Logf("GOLDEN_%d=%s", size[1], got)
		} else if got != want {
			t.Fatalf("report for %v differs from the original (sha %s)", size, got)
		}
	}
}

// Wall time for 16 requests at concurrency 8, median of 3 rounds, printed as HELDOUT_MS.
func TestHeldoutSpeed(t *testing.T) {
	srv := httptest.NewServer(NewServer(NewStore(300, 1000)))
	defer srv.Close()
	heldoutGet(t, srv.URL+"/report")
	var rounds []float64
	for r := 0; r < 3; r++ {
		start := time.Now()
		var wg sync.WaitGroup
		jobs := make(chan int)
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range jobs {
					heldoutGet(t, srv.URL+"/report")
				}
			}()
		}
		for i := 0; i < 16; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		rounds = append(rounds, float64(time.Since(start).Microseconds())/1000)
	}
	sort.Float64s(rounds)
	t.Logf("HELDOUT_MS=%.1f", rounds[1])
}
