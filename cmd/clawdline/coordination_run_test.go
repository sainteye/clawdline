package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCoordinatedLandingRunsOnlyAfterGrantAndReleases(t *testing.T) {
	var moves []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moves = append(moves, r.URL.Path)
		if r.URL.Path == "/v1/orchestrator/leases" {
			fmt.Fprint(w, `{"state":"granted"}`)
		} else {
			fmt.Fprint(w, `{"state":"released"}`)
		}
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "test", client: server.Client()}
	var stdout, stderr bytes.Buffer
	ran := false
	code := runCoordinatedOperation(&stdout, &stderr, b, coordinationRunOptions{resource: "landing", checkout: t.TempDir(), maxWait: time.Minute},
		[]string{"git", "merge"}, time.Now, time.Sleep,
		func(key string) string {
			if key == "CODEX_THREAD_ID" {
				return "379d0000-0000-4000-8000-000000000001"
			}
			return ""
		},
		func(argv, env []string) (int, error) {
			ran = true
			if len(moves) != 1 {
				t.Fatalf("command ran before grant: %v", moves)
			}
			return 0, nil
		})
	if code != 0 || !ran || len(moves) != 2 || moves[1] != "/v1/orchestrator/leases/release" {
		t.Fatalf("code %d, ran %v, moves %v, stderr %q", code, ran, moves, stderr.String())
	}
}

func TestCoordinatedRestartNeverRunsAfterQueueDeadline(t *testing.T) {
	var moves []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moves = append(moves, r.URL.Path)
		fmt.Fprint(w, `{"state":"queued","position":1}`)
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "test", client: server.Client()}
	var stdout, stderr bytes.Buffer
	clock := time.Unix(0, 0)
	code := runCoordinatedOperation(&stdout, &stderr, b, coordinationRunOptions{resource: "daemon_restart", maxWait: time.Second},
		[]string{"restart"}, func() time.Time { return clock }, func(d time.Duration) { clock = clock.Add(d) },
		func(key string) string {
			if key == "CODEX_THREAD_ID" {
				return "379d0000-0000-4000-8000-000000000001"
			}
			return ""
		},
		func([]string, []string) (int, error) { t.Fatal("ran without a grant"); return 0, nil })
	if code != heavyExitTimedOut || len(moves) != 3 || moves[2] != "/v1/orchestrator/leases/cancel" {
		t.Fatalf("code %d, moves %v, stderr %q", code, moves, stderr.String())
	}
}
