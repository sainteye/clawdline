package apiperformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func unitRuntime() *Runtime {
	r := &Runtime{limits: HardLimits(), started: time.Now(), boot: "synthetic-boot", build: "synthetic-build", events: make(chan event, MaxQueue), queries: make(chan query, 1), readers: make(chan struct{}, MaxReaders), stop: make(chan struct{}), done: make(chan struct{})}
	r.lastTick.Store(time.Now().UnixMilli())
	r.setStoreState("ok")
	return r
}

func TestHistogramBoundariesAndMergedQuantiles(t *testing.T) {
	var a, b Histogram
	for _, d := range []time.Duration{0, time.Millisecond, time.Millisecond + 1, 2 * time.Millisecond, 60000 * time.Millisecond, 60000*time.Millisecond + 1} {
		a.add(d)
	}
	if a.Bins[0] != 2 || a.Bins[1] != 2 || a.Bins[14] != 1 || a.Bins[15] != 1 || a.Count != 6 {
		t.Fatalf("boundary bins: %+v", a)
	}
	if a.Quantile(100) != nil || a.Quantile(0) != nil {
		t.Fatal("overflow/invalid rank must be unknown")
	}
	if v := a.Quantile(50); v == nil || *v != 2 {
		t.Fatalf("p50: %v", v)
	}
	for range 94 {
		b.add(time.Millisecond)
	}
	a.merge(b)
	if a.Count != 100 || *a.Quantile(90) != 1 {
		t.Fatalf("merged distribution: %+v", a)
	}
	var empty Histogram
	if empty.Quantile(50) != nil {
		t.Fatal("empty histogram has a percentile")
	}
}

func TestExactWindowsAndCancellationAreSeparateFromServerErrors(t *testing.T) {
	h := emptyHistory()
	h.limits = HardLimits()
	end := time.Now().Truncate(time.Hour)
	for _, e := range []event{
		{at: end.Add(-time.Hour), route: "GET /v1/sessions/:id", status: 200, duration: time.Millisecond},
		{at: end.Add(-time.Nanosecond), route: "GET /v1/sessions/:id", status: 503, cancelled: true, duration: time.Second},
		{at: end.Add(-time.Second), route: "GET /v1/sessions/:id", status: 500, duration: 2 * time.Second},
		{at: end, route: "GET /v1/sessions/:id", status: 499, duration: 3 * time.Second},
		{at: end.Add(-time.Second), route: "GET /v1/events", opened: true},
		{at: end.Add(-time.Second), route: "GET /v1/events", stream: true, status: 200, duration: 10 * time.Minute},
	} {
		h.observe(e, MaxRoutes)
	}
	r := unitRuntime()
	v, err := r.read(&h, context.Background(), 1, end)
	if err != nil {
		t.Fatal(err)
	}
	if v.Totals.Completed != 4 || v.Totals.Status5xx != 1 || v.Totals.Cancelled != 1 || v.Totals.Finite.Count != 3 || v.Totals.StreamLifetime.Count != 1 || v.Totals.StreamOpened != 1 {
		t.Fatalf("interval totals: %+v", v.Totals)
	}
	if v.FromMS != end.Add(-time.Hour).UnixMilli() || v.UntilMS != end.UnixMilli() || len(v.Points) != 60 {
		t.Fatalf("window: %+v", v)
	}
	if h.Minutes[end.UnixMilli()].Total.Cancelled != 1 {
		t.Fatal("exclusive until boundary was lost")
	}
	for _, hours := range []int{6, 24, 168} {
		v, err = r.read(&h, context.Background(), hours, end)
		if err != nil || len(v.Points) != hours || v.ResolutionSeconds != 3600 {
			t.Fatalf("%dh resolution: %d %v", hours, len(v.Points), err)
		}
	}
}

func TestNoTrafficMissingPartialStaleAndMixedBoots(t *testing.T) {
	start := time.Now().Add(-2 * time.Minute).Truncate(time.Minute)
	h := emptyHistory()
	h.limits = HardLimits()
	h.source = Provenance{BootID: "boot-a", Build: "build-a"}
	for sec := 0; sec < 60; sec++ {
		h.cover(start.Add(time.Duration(sec)*time.Second), start.Add(time.Duration(sec+1)*time.Second))
	}
	b := h.Minutes[start.UnixMilli()]
	if p := point(b, start.UnixMilli(), 60000, time.Now().UnixMilli()); p.State != "no_traffic" || p.CoveredSeconds != 60 {
		t.Fatalf("covered zero: %+v", p)
	}
	if p := point(nil, start.UnixMilli(), 60000, time.Now().UnixMilli()); p.State != "missing" {
		t.Fatalf("absent: %+v", p)
	}
	h.source = Provenance{BootID: "boot-b", Build: "build-b"}
	h.observe(event{at: start, route: "GET /v1/health", status: 200}, MaxRoutes)
	if !b.Source.Mixed || b.Source.BootID != "" || b.Source.Build != "" {
		t.Fatalf("cross-boot attribution: %+v", b.Source)
	}
	r := unitRuntime()
	r.lastTick.Store(time.Now().Add(-6 * time.Second).UnixMilli())
	v, err := r.read(&h, context.Background(), 1, time.Now())
	if err != nil || !v.Stale || len(v.RecentMinutes) != 6 {
		t.Fatalf("freshness: %v %+v", err, v)
	}
	r.noteDrop(start)
	v, _ = r.read(&h, context.Background(), 1, time.Now())
	for _, p := range v.Points {
		if p.StartMS == start.UnixMilli() && p.State != "partial" {
			t.Fatalf("dropped events cannot prove no traffic: %+v", p)
		}
	}
}

func TestRouteOverflowAndRetentionRemainBounded(t *testing.T) {
	h := emptyHistory()
	h.limits = HardLimits()
	now := time.Now()
	for i := 0; i < MaxHours+20; i++ {
		h.observe(event{at: now.Add(time.Duration(i) * time.Hour), route: fmt.Sprintf("GET /route/%d", i), status: 200}, 2)
	}
	if len(h.Minutes) > MaxMinutes || len(h.Hours) > MaxHours || len(h.Routes) != 2 || h.Overflow == 0 {
		t.Fatalf("bounds: minutes=%d hours=%d routes=%d overflow=%d", len(h.Minutes), len(h.Hours), len(h.Routes), h.Overflow)
	}
	// A late queued event cannot expand either retention window.
	h.observe(event{at: now.Add(-time.Hour), route: strings.Repeat("x", MaxRouteBytes+1), status: 200}, 2)
	if len(h.Minutes) > MaxMinutes || len(h.Hours) > MaxHours {
		t.Fatal("late event expanded retention")
	}
	for _, b := range h.Hours {
		if len(b.Routes) > 3 {
			t.Fatal("overflow created extra keys")
		}
	}
	h.limits["hours"] = 2
	h.prune(h.Watermark)
	if len(h.Hours) > 2 {
		t.Fatal("lowered retention was ignored")
	}
}

func TestQueueReadersAndActiveSlotsShedWithoutBlocking(t *testing.T) {
	r := unitRuntime()
	r.events = make(chan event, 1)
	r.limits["active"] = 1
	a, b := r.Begin("GET /one"), r.Begin("GET /two")
	if r.active.Load() != 2 || r.activeUntracked.Load() != 1 {
		t.Fatal("active overflow must leave oldest unknown")
	}
	a.OpenStream()
	a.OpenStream()
	if r.streams.Load() != 1 {
		t.Fatal("stream double-open")
	}
	a.Done(200, false)
	a.Done(200, false)
	b.Done(499, true)
	if r.active.Load() != 0 || r.streams.Load() != 0 || r.dropped.Load() != 2 {
		t.Fatalf("nonblocking shed/dedup: %d %d %d", r.active.Load(), r.streams.Load(), r.dropped.Load())
	}
	r.readers <- struct{}{}
	r.readers <- struct{}{}
	if _, err := r.Query(context.Background(), 1, time.Time{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("reader admission: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Query(ctx, 1, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query: %v", err)
	}
	if _, err := r.Query(context.Background(), 2, time.Time{}); !errors.Is(err, ErrQuery) {
		t.Fatalf("query enum: %v", err)
	}
}

func TestWorstCaseHistoryAndReadingFitTheirByteCaps(t *testing.T) {
	h := emptyHistory()
	h.limits = HardLimits()
	now := time.Now().Truncate(time.Hour)
	for hour := 0; hour < MaxHours; hour++ {
		at := now.Add(time.Duration(hour-MaxHours+1) * time.Hour)
		for route := 0; route < MaxRoutes; route++ {
			h.observe(event{at: at, route: fmt.Sprintf("GET /v1/%03d/%s", route, strings.Repeat("x", 220)), status: 500, duration: time.Hour}, MaxRoutes)
		}
	}
	payload, err := json.Marshal(h)
	if err != nil || len(payload) > MaxSnapshotBytes {
		t.Fatalf("bounded history bytes=%d err=%v", len(payload), err)
	}
	r := unitRuntime()
	v, err := r.read(&h, context.Background(), 168, now)
	if err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(v)
	if err != nil || len(payload) > MaxQueryBytes {
		t.Fatalf("bounded query bytes=%d err=%v", len(payload), err)
	}
	t.Logf("full route/hour history=%d bytes; seven-day query=%d bytes", mustJSONSize(h), len(payload))
}

func mustJSONSize(v any) int { b, _ := json.Marshal(v); return len(b) }

func TestLocatedLossDoesNotMakeFutureCleanHoursPartial(t *testing.T) {
	r := unitRuntime()
	start := time.Now().Add(-2 * time.Minute).Truncate(time.Minute)
	h := emptyHistory()
	h.limits = HardLimits()
	for sec := 0; sec < 60; sec++ {
		h.cover(start.Add(time.Duration(sec)*time.Second), start.Add(time.Duration(sec+1)*time.Second))
	}
	r.noteDrop(start.Add(-time.Hour))
	v, err := r.read(&h, context.Background(), 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range v.Points {
		if p.StartMS == start.UnixMilli() && p.State != "no_traffic" {
			t.Fatalf("unrelated old loss tainted clean coverage: %+v", p)
		}
	}
	if len(r.lossHoursAt(start.Add(time.Duration(MaxHours+1)*time.Hour).UnixMilli())) != 0 {
		t.Fatal("loss markers exceeded retention")
	}
	if r.unlocatedLoss.Load() {
		t.Fatal("located loss became globally unknown")
	}
}

func BenchmarkBoundedAggregation(b *testing.B) {
	h := emptyHistory()
	h.limits = HardLimits()
	now := time.Now()
	for hour := 0; hour < MaxHours; hour++ {
		for route := 0; route < MaxRoutes; route++ {
			h.observe(event{at: now.Add(time.Duration(hour-MaxHours+1) * time.Hour), route: fmt.Sprintf("GET /%d", route), status: 200, duration: time.Millisecond}, MaxRoutes)
		}
	}
	for minute := 0; minute < MaxMinutes; minute++ {
		h.observe(event{at: now.Add(-time.Duration(minute) * time.Minute), route: "GET /0", status: 200, duration: time.Millisecond}, MaxRoutes)
	}
	e := event{at: now, route: "GET /0", status: 200, duration: time.Millisecond}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.observe(e, MaxRoutes)
	}
}

func BenchmarkRequestCompletion(b *testing.B) {
	r := unitRuntime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o := r.Begin("GET /v1/sessions/:id/info")
		o.Done(200, false)
		select {
		case <-r.events:
		default:
		}
	}
}

func BenchmarkBoundedSevenDayRead(b *testing.B) {
	h := emptyHistory()
	h.limits = HardLimits()
	now := time.Now().Truncate(time.Hour)
	for hour := 0; hour < MaxHours; hour++ {
		for route := 0; route < MaxRoutes; route++ {
			h.observe(event{at: now.Add(time.Duration(hour-MaxHours+1) * time.Hour), route: fmt.Sprintf("GET /%d", route), status: 200, duration: time.Millisecond}, MaxRoutes)
		}
	}
	r := unitRuntime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.read(&h, context.Background(), 168, now); err != nil {
			b.Fatal(err)
		}
	}
}
