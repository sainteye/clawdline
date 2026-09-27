// Load test: go run ./loadtest [-url http://$ADDR/report] [-n 20] [-c 8]
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

func main() {
	def := "127.0.0.1:8081"
	if a := os.Getenv("ADDR"); a != "" {
		def = a
	}
	url := flag.String("url", "http://"+def+"/report", "endpoint")
	n := flag.Int("n", 20, "requests")
	c := flag.Int("c", 8, "concurrency")
	flag.Parse()
	var mu sync.Mutex
	var lat []time.Duration
	jobs := make(chan int)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < *c; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				t := time.Now()
				resp, err := http.Get(*url)
				if err != nil {
					fmt.Println("error:", err)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				mu.Lock()
				lat = append(lat, time.Since(t))
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < *n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	if len(lat) == 0 {
		return
	}
	fmt.Printf("requests=%d concurrency=%d wall=%v p50=%v p95=%v\n", len(lat), *c, time.Since(start).Round(time.Millisecond), lat[len(lat)/2].Round(time.Millisecond), lat[len(lat)*95/100].Round(time.Millisecond))
}
