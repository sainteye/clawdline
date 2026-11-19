package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestResourcesUseGoReturnsOnlyTheVerifiedCachePath(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/v1/orchestrator/resources/use-go" {
			t.Fatal(r.URL.Path)
		}
		return 200, `{"path":"/managed/project/go-build"}`
	})
	var stdout, stderr bytes.Buffer
	if code := runResources(&stdout, &stderr, b, []string{"use-go", "e9200000-0000-4000-8000-000000000001"}); code != 0 || strings.TrimSpace(stdout.String()) != "/managed/project/go-build" {
		t.Fatal(code, stdout.String(), stderr.String())
	}
}
