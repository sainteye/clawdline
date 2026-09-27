package main

// Held-out correctness and performance checks, copied into the run after Codex exits.

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func source(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i:]
	if end != "" {
		if j := strings.Index(s, end); j >= 0 {
			return s[:j]
		}
	}
	return s
}

func TestHeldoutP1BatchUsers(t *testing.T) {
	s := between(source(t, "report.go"), "func BuildReport", "func reportHandler")
	if !strings.Contains(s, "GetUsers(") || strings.Contains(s, ".GetUser(") {
		t.Fatalf("BuildReport has not replaced per-order GetUser with GetUsers")
	}
}

func TestHeldoutP2ConcurrentReports(t *testing.T) {
	if strings.Contains(source(t, "report.go"), "reportMu") {
		t.Fatalf("report generation still has a process-wide mutex")
	}
}

func TestHeldoutP3CompileRegexOnce(t *testing.T) {
	s := between(source(t, "report.go"), "func cleanNote", "func uniqueSorted")
	if strings.Contains(s, "MustCompile") || strings.Contains(s, "Compile(") {
		t.Fatalf("cleanNote still compiles regular expressions per call")
	}
}

func TestHeldoutP4LinearDistinct(t *testing.T) {
	items := make([]string, 30000)
	for i := range items {
		items[i] = strings.Repeat("x", i%11) + string(rune(0x1000+i))
	}
	start := time.Now()
	got := uniqueSorted(items)
	if len(got) != len(items) {
		t.Fatalf("unique len %d, want %d", len(got), len(items))
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("distinct selection is still superlinear: %v", elapsed)
	}
}

func TestHeldoutP5BatchProducts(t *testing.T) {
	s := between(source(t, "report.go"), "func BuildReport", "func reportHandler")
	if !strings.Contains(s, "GetProducts(") || strings.Contains(s, ".GetProduct(") {
		t.Fatalf("BuildReport has not replaced per-order GetProduct with GetProducts")
	}
}

func TestHeldoutP6BatchTotals(t *testing.T) {
	s := between(source(t, "report.go"), "func BuildReport", "func reportHandler")
	if !strings.Contains(s, "CustomerTotals(") || strings.Contains(s, ".CustomerTotal(") {
		t.Fatalf("BuildReport has not replaced per-order CustomerTotal with CustomerTotals")
	}
}

func TestHeldoutP7SingleJSONEncoding(t *testing.T) {
	s := between(source(t, "report.go"), "func reportHandler", "")
	if strings.Contains(s, "json.Unmarshal") || strings.Count(s, "json.Marshal")+strings.Count(s, "json.NewEncoder") > 1 {
		t.Fatalf("handler still performs a JSON round trip")
	}
}

func TestHeldoutP8EndToEndLatency(t *testing.T) {
	s := NewStore(100, 180)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = BuildReport(s) }()
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("eight concurrent reports took %v", elapsed)
	}
}

func TestHeldoutFunctionalReport(t *testing.T) {
	r := BuildReport(NewStore(60, 400))
	if len(r.Lines) != 400 {
		t.Fatalf("lines %d", len(r.Lines))
	}
	total := 0
	for _, l := range r.Lines {
		total += l.Amount
		if l.Customer == "" || l.Product == "" || l.CustomerTotal <= 0 || strings.ContainsAny(l.Note, "<>\t") {
			t.Fatalf("bad line: %+v", l)
		}
	}
	if total != r.Total || !sort.StringsAreSorted(r.AllTags) || !sort.StringsAreSorted(r.Customers) {
		t.Fatalf("summary mismatch: total=%d/%d tags=%v customers=%v", total, r.Total, r.AllTags, r.Customers)
	}
}

func TestHeldoutFunctionalHTTP(t *testing.T) {
	rec := httptest.NewRecorder()
	NewServer(NewStore(10, 30)).ServeHTTP(rec, httptest.NewRequest("GET", "/report", nil))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("response: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var r Report
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || len(r.Lines) != 30 {
		t.Fatalf("json: %v lines=%d", err, len(r.Lines))
	}
}
