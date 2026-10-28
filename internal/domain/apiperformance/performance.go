// Package apiperformance keeps bounded operational aggregates on their owner's
// machine. Its input is a transport-sanitized route shape, never a raw request.
package apiperformance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/bits"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxQueue         = 1024
	MaxRoutes        = 128
	MaxRouteBytes    = 256
	MaxMinutes       = 61
	MaxHours         = 169
	MaxReaders       = 2
	MaxActive        = 512
	MaxQueryBytes    = 2 << 20
	MaxQuerySeconds  = 2
	MaxStoreBytes    = 64 << 20
	MaxSnapshotBytes = 32 << 20
	// One extra partial hour bounds the alignment overhead of seven full days.
	MaxRetentionSeconds = MaxHours * 60 * 60
	MaxCatchupSeconds   = 2
	FlushSeconds        = 15
)

var (
	ErrUnavailable = errors.New("api_performance_unavailable")
	ErrBusy        = errors.New("api_performance_busy")
	ErrQuery       = errors.New("api_performance_bad_query")
)

// HardLimits names the capacity rows. A caller may lower any bound.
func HardLimits() map[string]int64 {
	return map[string]int64{"queue": MaxQueue, "routes": MaxRoutes, "route_bytes": MaxRouteBytes,
		"minutes": MaxMinutes, "hours": MaxHours, "readers": MaxReaders, "active": MaxActive,
		"query_bytes": MaxQueryBytes, "query_seconds": MaxQuerySeconds, "store": MaxStoreBytes,
		"snapshot": MaxSnapshotBytes, "retention": MaxRetentionSeconds, "catchup": MaxCatchupSeconds}
}

// HistogramBoundsMS are inclusive upper bounds; the last bin is unbounded.
// Quantiles return upper bounds, or nil for an empty/overflow quantile. These
// histograms can be merged exactly; percentiles themselves are never averaged.
var HistogramBoundsMS = [...]int64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 30000, 60000}

type Histogram struct {
	Bins  [16]uint64 `json:"bins"`
	Count uint64     `json:"count"`
	SumMS float64    `json:"sum_ms"`
	MaxMS float64    `json:"max_ms"`
}

func (h *Histogram) add(d time.Duration) {
	if d < 0 {
		d = 0
	}
	ms := float64(d) / float64(time.Millisecond)
	i := 0
	for i < len(HistogramBoundsMS) && ms > float64(HistogramBoundsMS[i]) {
		i++
	}
	h.Bins[i]++
	h.Count++
	h.SumMS += ms
	if ms > h.MaxMS {
		h.MaxMS = ms
	}
}

func (h *Histogram) merge(b Histogram) {
	for i, n := range b.Bins {
		h.Bins[i] += n
	}
	h.Count += b.Count
	h.SumMS += b.SumMS
	if b.MaxMS > h.MaxMS {
		h.MaxMS = b.MaxMS
	}
}

func (h Histogram) Quantile(p uint64) *float64 {
	if h.Count == 0 || p == 0 || p > 100 {
		return nil
	}
	rank := (h.Count*p + 99) / 100
	var n uint64
	for i, c := range h.Bins {
		n += c
		if n >= rank {
			if i == len(HistogramBoundsMS) {
				return nil
			}
			v := float64(HistogramBoundsMS[i])
			return &v
		}
	}
	return nil
}

type Totals struct {
	Completed       uint64    `json:"completed"`
	Status2xx       uint64    `json:"status_2xx"`
	Status3xx       uint64    `json:"status_3xx"`
	Status4xx       uint64    `json:"status_4xx"`
	Status5xx       uint64    `json:"status_5xx"`
	Cancelled       uint64    `json:"cancelled"`
	StreamOpened    uint64    `json:"stream_opened"`
	StreamCompleted uint64    `json:"stream_completed"`
	Finite          Histogram `json:"finite_latency_ms"`
	StreamLifetime  Histogram `json:"stream_lifetime_ms"`
}

func (t *Totals) merge(b Totals) {
	t.Completed += b.Completed
	t.Status2xx += b.Status2xx
	t.Status3xx += b.Status3xx
	t.Status4xx += b.Status4xx
	t.Status5xx += b.Status5xx
	t.Cancelled += b.Cancelled
	t.StreamOpened += b.StreamOpened
	t.StreamCompleted += b.StreamCompleted
	t.Finite.merge(b.Finite)
	t.StreamLifetime.merge(b.StreamLifetime)
}

type event struct {
	at                        time.Time
	route                     string
	status                    int
	duration                  time.Duration
	stream, cancelled, opened bool
}

func (t *Totals) observe(e event) {
	if e.opened {
		t.StreamOpened++
		return
	}
	t.Completed++
	switch {
	case e.cancelled || e.status == 499:
		t.Cancelled++
	case e.status >= 500:
		t.Status5xx++
	case e.status >= 400:
		t.Status4xx++
	case e.status >= 300:
		t.Status3xx++
	default:
		t.Status2xx++
	}
	if e.stream {
		t.StreamCompleted++
		t.StreamLifetime.add(e.duration)
	} else {
		t.Finite.add(e.duration)
	}
}

// Buckets carry second-level coverage. A minute uses word zero; an hour uses
// all sixty words. Repeated ticks/restarts set bits instead of adding coverage.
type bucket struct {
	Start    int64             `json:"start_ms"`
	Coverage [60]uint64        `json:"coverage"`
	Total    Totals            `json:"total"`
	Routes   map[string]Totals `json:"routes"`
	Source   Provenance        `json:"source"`
}

// Mixed is explicit when a bucket straddles a restart/build. It does not
// attribute an old observation to the currently running build.
type Provenance struct {
	BootID string `json:"boot_id,omitempty"`
	Build  string `json:"build,omitempty"`
	Mixed  bool   `json:"mixed"`
}

func (b *bucket) provenance(source Provenance) {
	if b.Source.Mixed {
		return
	}
	if b.Source.BootID == "" {
		b.Source = source
		return
	}
	if b.Source.BootID != source.BootID || b.Source.Build != source.Build {
		b.Source = Provenance{Mixed: true}
	}
}

type history struct {
	Schema        int               `json:"schema"`
	Minutes       map[int64]*bucket `json:"minutes"`
	Hours         map[int64]*bucket `json:"hours"`
	Routes        map[string]bool   `json:"routes"`
	PersistedAt   int64             `json:"persisted_at_ms"`
	Dropped       uint64            `json:"dropped"`
	LossHours     []int64           `json:"loss_hours_ms"`
	UnlocatedLoss bool              `json:"unlocated_loss"`
	Overflow      uint64            `json:"route_overflow"`
	Watermark     int64             `json:"watermark_ms"`
	source        Provenance
	limits        map[string]int64
}

func emptyHistory() history {
	return history{Schema: 1, Minutes: map[int64]*bucket{}, Hours: map[int64]*bucket{}, Routes: map[string]bool{}}
}

func (h *history) bound(name string) int64 {
	if n := h.limits[name]; n > 0 {
		return n
	}
	return HardLimits()[name]
}

func floor(ms, width int64) int64 { return ms / width * width }

func getBucket(m map[int64]*bucket, ms, width int64) *bucket {
	k := floor(ms, width)
	b := m[k]
	if b == nil {
		b = &bucket{Start: k, Routes: map[string]Totals{}}
		m[k] = b
	}
	return b
}

func (h *history) prune(now int64) {
	if now > h.Watermark {
		h.Watermark = now
	} else {
		now = h.Watermark
	}
	for k := range h.Minutes {
		if k < floor(now, 60000)-(h.bound("minutes")-1)*60000 {
			delete(h.Minutes, k)
		}
	}
	for k := range h.Hours {
		if k < floor(now, 3600000)-(h.bound("hours")-1)*3600000 || k < floor(now-h.bound("retention")*1000, 3600000) {
			delete(h.Hours, k)
		}
	}
}

func (h *history) observe(e event, routeCap int) {
	if !h.Routes[e.route] {
		if len(h.Routes) >= routeCap || int64(len(e.route)) > h.bound("route_bytes") || e.route == "" || e.route == "OTHER (overflow)" {
			e.route = "OTHER (overflow)"
			h.Overflow++
		} else {
			h.Routes[e.route] = true
		}
	}
	for _, b := range []*bucket{getBucket(h.Minutes, e.at.UnixMilli(), 60000), getBucket(h.Hours, e.at.UnixMilli(), 3600000)} {
		b.provenance(h.source)
		b.Total.observe(e)
		t := b.Routes[e.route]
		t.observe(e)
		b.Routes[e.route] = t
	}
	h.prune(e.at.UnixMilli())
}

func (h *history) cover(from, to time.Time) {
	end := to.Unix()
	start := from.Unix()
	if end-start > h.bound("catchup") {
		start = end - h.bound("catchup")
	}
	for sec := start; sec < end; sec++ {
		for _, width := range []int64{60000, 3600000} {
			m := h.Minutes
			if width == 3600000 {
				m = h.Hours
			}
			b := getBucket(m, sec*1000, width)
			b.provenance(h.source)
			i := (sec*1000 - b.Start) / 1000
			b.Coverage[i/60] |= uint64(1) << (i % 60)
		}
	}
	h.prune(to.UnixMilli())
}

type Options struct {
	Dir, Build string
	// Queue and Routes may only lower their registered defaults.
	Queue, Routes int
	Limit         func(string) int64
}

type query struct {
	ctx    context.Context
	hours  int
	until  time.Time
	result chan answer
}
type answer struct {
	value Reading
	err   error
}

// Runtime's single aggregate worker owns history. Requests only touch atomics
// and a nonblocking queue. The independent SQLite writer owns its connection.
type Runtime struct {
	events          chan event
	queries         chan query
	readers         chan struct{}
	stop            chan struct{}
	done            chan struct{}
	closing         atomic.Bool
	admission       sync.RWMutex
	active          atomic.Int64
	streams         atomic.Int64
	activeSlots     [MaxActive]atomic.Int64
	activeUntracked atomic.Int64
	dropped         atomic.Uint64
	lossHours       [MaxHours]atomic.Int64
	unlocatedLoss   atomic.Bool
	overflow        atomic.Uint64
	queueUsed       atomic.Int64
	routeUsed       atomic.Int64
	minuteUsed      atomic.Int64
	hourUsed        atomic.Int64
	snapshotUsed    atomic.Int64
	oldestHour      atomic.Int64
	storeBytes      atomic.Int64
	storeFailures   atomic.Uint64
	storeAt         atomic.Int64
	storeState      atomic.Pointer[string]
	lastTick        atomic.Int64
	boot, build     string
	started         time.Time
	routeCap        int
	limits          map[string]int64
}

func New(o Options) *Runtime {
	limits := HardLimits()
	for name, bound := range limits {
		if o.Limit != nil {
			n := o.Limit(name)
			if n > 0 && n < bound {
				limits[name] = n
			}
		}
	}
	queue := o.Queue
	if queue <= 0 || queue > int(limits["queue"]) {
		queue = int(limits["queue"])
	}
	routes := o.Routes
	if routes <= 0 || routes > int(limits["routes"]) {
		routes = int(limits["routes"])
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	build := o.Build
	if len(build) > int(limits["route_bytes"]) {
		build = build[:int(limits["route_bytes"])]
	}
	readers := int(limits["readers"])
	r := &Runtime{events: make(chan event, queue), queries: make(chan query, 1), readers: make(chan struct{}, readers), stop: make(chan struct{}), done: make(chan struct{}), boot: hex.EncodeToString(id[:]), build: build, started: time.Now(), routeCap: routes, limits: limits}
	r.setStoreState("opening")
	go r.run(o.Dir)
	return r
}

func (r *Runtime) setStoreState(s string) { r.storeState.Store(&s) }

func (r *Runtime) Done() <-chan struct{} { return r.done }

func (r *Runtime) Limit(name string) int64 { return r.limits[name] }

type Operation struct {
	runtime  *Runtime
	start    time.Time
	slot     int
	route    string
	stream   bool
	finished bool
}

// Begin records active work without keeping a request, identifier or context.
// The returned operation belongs to the handler goroutine.
func (r *Runtime) Begin(route string) *Operation {
	if r == nil {
		return nil
	}
	now := time.Now()
	if int64(len(route)) > r.Limit("route_bytes") {
		route = "OTHER (overflow)"
	}
	o := &Operation{runtime: r, start: now, slot: -1, route: route}
	r.active.Add(1)
	for i := range r.activeSlots {
		if int64(i) >= r.Limit("active") {
			break
		}
		if r.activeSlots[i].CompareAndSwap(0, now.UnixMilli()) {
			o.slot = i
			break
		}
	}
	if o.slot < 0 {
		r.activeUntracked.Add(1)
	}
	return o
}

func (o *Operation) OpenStream() {
	if o == nil || o.stream || o.finished {
		return
	}
	o.stream = true
	o.runtime.streams.Add(1)
	o.runtime.offer(event{at: time.Now(), route: o.route, opened: true})
}

func (o *Operation) Done(status int, cancelled bool) {
	if o == nil || o.finished {
		return
	}
	o.finished = true
	r := o.runtime
	r.active.Add(-1)
	if o.slot >= 0 {
		r.activeSlots[o.slot].Store(0)
	} else {
		r.activeUntracked.Add(-1)
	}
	if o.stream {
		r.streams.Add(-1)
	}
	r.offer(event{at: time.Now(), route: o.route, status: status, cancelled: cancelled, duration: time.Since(o.start), stream: o.stream})
}

func (r *Runtime) offer(e event) {
	if !r.admission.TryRLock() {
		r.noteDrop(e.at)
		return
	}
	defer r.admission.RUnlock()
	if r.closing.Load() {
		r.noteDrop(e.at)
		return
	}
	select {
	case r.events <- e:
	default:
		r.noteDrop(e.at)
	}
}

func (r *Runtime) noteDrop(at time.Time) {
	r.dropped.Add(1)
	r.markLossHour(floor(at.UnixMilli(), 3600000))
}

func (r *Runtime) markLossHour(hour int64) {
	if hour <= 0 {
		r.unlocatedLoss.Store(true)
		return
	}
	slot := &r.lossHours[(hour/3600000)%MaxHours]
	// One CAS, no retries. Contention that cannot prove this hour was marked
	// falls back to explicitly unlocated loss on the completion path.
	old := slot.Load()
	if old >= hour || slot.CompareAndSwap(old, hour) {
		return
	}
	if slot.Load() < hour {
		r.unlocatedLoss.Store(true)
	}
}

func (r *Runtime) lossHoursAt(now int64) []int64 {
	result := []int64{}
	for i := range r.lossHours {
		hour := r.lossHours[i].Load()
		if hour > 0 && hour >= floor(now, 3600000)-int64(MaxHours-1)*3600000 {
			result = append(result, hour)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (r *Runtime) hasLoss(at int64) bool {
	if r.unlocatedLoss.Load() {
		return true
	}
	hour := floor(at, 3600000)
	return hour > 0 && r.lossHours[(hour/3600000)%MaxHours].Load() == hour
}

// Close requests a bounded shutdown. A stuck filesystem worker is abandoned
// when ctx expires; business shutdown never waits on it indefinitely.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.closing.CompareAndSwap(false, true) {
		close(r.stop)
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) run(dir string) {
	defer close(r.done)
	h := emptyHistory()
	writer := r.startStorage(dir)
	{
		loaded := <-writer.loaded
		if loaded.err == nil {
			h = loaded.history
			r.dropped.Add(h.Dropped)
			for _, hour := range h.LossHours {
				r.markLossHour(hour)
			}
			if h.UnlocatedLoss {
				r.unlocatedLoss.Store(true)
			}
			r.overflow.Store(h.Overflow)
		}
	}
	h.source = Provenance{BootID: r.boot, Build: r.build}
	h.limits = r.limits
	h.prune(time.Now().UnixMilli())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := time.Now().Truncate(time.Second).Add(time.Second)
	lastFlush := time.Now()
	r.lastTick.Store(lastFlush.UnixMilli())
	for {
		select {
		case e := <-r.events:
			h.observe(e, r.routeCap)
		case q := <-r.queries:
			v, err := r.read(&h, q.ctx, q.hours, q.until)
			q.result <- answer{v, err}
		case now := <-ticker.C:
			h.cover(last, now)
			last = now.Truncate(time.Second)
			r.lastTick.Store(now.UnixMilli())
			if now.Sub(lastFlush) >= FlushSeconds*time.Second {
				r.persist(writer, &h, now)
				lastFlush = now
			}
		case <-r.stop:
			r.admission.Lock()
			// No producer can wait on this drain, and the queue has a hard cap.
			for n := len(r.events); n > 0; n-- {
				h.observe(<-r.events, r.routeCap)
			}
			r.admission.Unlock()
			r.persist(writer, &h, time.Now())
			close(writer.pending)
			<-writer.done
			return
		}
		r.queueUsed.Store(int64(len(r.events)))
		r.routeUsed.Store(int64(len(h.Routes)))
		r.minuteUsed.Store(int64(len(h.Minutes)))
		r.hourUsed.Store(int64(len(h.Hours)))
		oldest := int64(0)
		for at := range h.Hours {
			if oldest == 0 || at < oldest {
				oldest = at
			}
		}
		r.oldestHour.Store(oldest)
		r.overflow.Store(h.Overflow)
	}
}

type Point struct {
	StartMS        int64      `json:"start_ms"`
	EndMS          int64      `json:"end_ms"`
	State          string     `json:"state"`
	CoveredSeconds int        `json:"covered_seconds"`
	Totals         Totals     `json:"totals"`
	Source         Provenance `json:"source"`
}

type RouteSummary struct {
	Route  string `json:"route"`
	Totals Totals `json:"totals"`
}

type Reading struct {
	Schema                    int            `json:"schema"`
	Source                    string         `json:"source"`
	BootID                    string         `json:"boot_id"`
	Build                     string         `json:"build"`
	AtMS                      int64          `json:"at_ms"`
	StartedMS                 int64          `json:"started_ms"`
	LastTickMS                int64          `json:"last_tick_ms"`
	Stale                     bool           `json:"stale"`
	FromMS                    int64          `json:"from_ms"`
	UntilMS                   int64          `json:"until_ms"`
	ResolutionSeconds         int            `json:"resolution_seconds"`
	RetentionFloorMS          int64          `json:"retention_floor_ms"`
	MinuteFloorMS             int64          `json:"minute_floor_ms"`
	PersistedAtMS             int64          `json:"persisted_at_ms"`
	CheckpointIntervalSeconds int            `json:"checkpoint_interval_seconds"`
	UnpersistedSeconds        int64          `json:"unpersisted_seconds"`
	StorageState              string         `json:"storage_state"`
	StoreFailures             uint64         `json:"store_failures"`
	Dropped                   uint64         `json:"dropped"`
	LossHoursMS               []int64        `json:"loss_hours_ms"`
	LossScopeKnown            bool           `json:"loss_scope_known"`
	QueuedEvents              int            `json:"queued_events"`
	RouteOverflow             uint64         `json:"route_overflow"`
	Active                    int64          `json:"active"`
	OpenStreams               int64          `json:"open_streams"`
	OldestActiveAgeMS         *int64         `json:"oldest_active_age_ms"`
	ActiveCoverageComplete    bool           `json:"active_coverage_complete"`
	HistogramUpperBoundsMS    []int64        `json:"histogram_upper_bounds_ms"`
	Points                    []Point        `json:"points"`
	RecentMinutes             []Point        `json:"recent_minutes"`
	Totals                    Totals         `json:"totals"`
	SlowRoutes                []RouteSummary `json:"slow_routes"`
	RecentSlowRoutes          []RouteSummary `json:"recent_slow_routes"`
}

func point(b *bucket, start, width int64, now int64) Point {
	p := Point{StartMS: start, EndMS: start + width, State: "missing"}
	if b == nil {
		return p
	}
	p.Totals = b.Total
	p.Source = b.Source
	for _, v := range b.Coverage {
		p.CoveredSeconds += bits.OnesCount64(v)
	}
	expected := int(width / 1000)
	if p.EndMS > now {
		expected = int((now - start) / 1000)
	}
	switch {
	case p.CoveredSeconds == 0 && p.Totals.Completed == 0 && p.Totals.StreamOpened == 0:
		p.State = "missing"
	case p.CoveredSeconds == 0:
		p.State = "partial"
	case p.CoveredSeconds < expected:
		p.State = "partial"
	case p.Totals.Completed == 0 && p.Totals.StreamOpened == 0:
		p.State = "no_traffic"
	default:
		p.State = "ok"
	}
	return p
}

func (r *Runtime) Query(ctx context.Context, hours int, until time.Time) (Reading, error) {
	if hours != 1 && hours != 6 && hours != 24 && hours != 168 {
		return Reading{}, ErrQuery
	}
	now := time.Now()
	if until.IsZero() {
		until = now
	}
	if until.After(now) || until.Before(now.Add(-time.Duration(r.Limit("retention"))*time.Second)) {
		return Reading{}, ErrQuery
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.Limit("query_seconds"))*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Reading{}, err
	}
	select {
	case r.readers <- struct{}{}:
		defer func() { <-r.readers }()
	default:
		return Reading{}, ErrBusy
	}
	q := query{ctx: ctx, hours: hours, until: until, result: make(chan answer, 1)}
	select {
	case <-r.stop:
		return Reading{}, ErrUnavailable
	default:
	}
	select {
	case r.queries <- q:
	default:
		return Reading{}, ErrBusy
	}
	select {
	case a := <-q.result:
		return a.value, a.err
	case <-ctx.Done():
		return Reading{}, ctx.Err()
	case <-r.done:
		return Reading{}, ErrUnavailable
	}
}

func (r *Runtime) read(h *history, ctx context.Context, hours int, until time.Time) (Reading, error) {
	now := time.Now().UnixMilli()
	width := int64(3600000)
	source := h.Hours
	if hours == 1 {
		width = 60000
		source = h.Minutes
	}
	end := floor(until.UnixMilli(), width)
	start := end - int64(hours)*3600000
	v := Reading{
		Schema: 1, Source: "owner_local", BootID: r.boot, Build: r.build,
		AtMS: now, StartedMS: r.started.UnixMilli(), LastTickMS: r.lastTick.Load(),
		FromMS: start, UntilMS: end, ResolutionSeconds: int(width / 1000),
		PersistedAtMS: r.storeAt.Load(), CheckpointIntervalSeconds: FlushSeconds,
		StorageState: *r.storeState.Load(), StoreFailures: r.storeFailures.Load(),
		Dropped: r.dropped.Load(), RouteOverflow: h.Overflow,
		Active: r.active.Load(), OpenStreams: r.streams.Load(),
		ActiveCoverageComplete: r.activeUntracked.Load() == 0,
		HistogramUpperBoundsMS: append([]int64{}, HistogramBoundsMS[:]...),
		Points:                 []Point{}, RecentMinutes: []Point{}, SlowRoutes: []RouteSummary{},
	}
	unpersistedSince := v.PersistedAtMS
	if unpersistedSince == 0 {
		unpersistedSince = v.StartedMS
	}
	v.UnpersistedSeconds = (now - unpersistedSince + 999) / 1000
	if v.UnpersistedSeconds < 0 {
		v.UnpersistedSeconds = 0
	}
	v.Stale = now < v.LastTickMS || now-v.LastTickMS > 5000 || now < v.PersistedAtMS || now < h.Watermark
	v.QueuedEvents = len(r.events)
	v.LossHoursMS = r.lossHoursAt(now)
	v.LossScopeKnown = !r.unlocatedLoss.Load()
	v.RetentionFloorMS = floor(now, 3600000) - (r.Limit("hours")-1)*3600000
	if cutoff := floor(now-r.Limit("retention")*1000, 3600000); cutoff > v.RetentionFloorMS {
		v.RetentionFloorMS = cutoff
	}
	v.MinuteFloorMS = floor(now, 60000) - (r.Limit("minutes")-1)*60000
	if v.ActiveCoverageComplete {
		oldest := now
		for i := range r.activeSlots {
			at := r.activeSlots[i].Load()
			if at > now {
				v.ActiveCoverageComplete = false
			}
			if at > 0 && at < oldest {
				oldest = at
			}
		}
		age := now - oldest
		if v.ActiveCoverageComplete {
			v.OldestActiveAgeMS = &age
		}
	}
	routes := map[string]Totals{}
	for at := start; at < end; at += width {
		if err := ctx.Err(); err != nil {
			return Reading{}, err
		}
		b := source[at]
		p := point(b, at, width, now)
		if (r.hasLoss(at) || v.QueuedEvents > 0) && p.State != "missing" {
			p.State = "partial"
		}
		v.Points = append(v.Points, p)
		v.Totals.merge(p.Totals)
		if b != nil {
			for k, t := range b.Routes {
				sum := routes[k]
				sum.merge(t)
				routes[k] = sum
			}
		}
	}
	minute := floor(now, 60000)
	recentRoutes := map[string]Totals{}
	for at := minute - 5*60000; at <= minute; at += 60000 {
		p := point(h.Minutes[at], at, 60000, now)
		if (r.hasLoss(at) || v.QueuedEvents > 0) && p.State != "missing" {
			p.State = "partial"
		}
		v.RecentMinutes = append(v.RecentMinutes, p)
		if b := h.Minutes[at]; b != nil {
			for k, t := range b.Routes {
				sum := recentRoutes[k]
				sum.merge(t)
				recentRoutes[k] = sum
			}
		}
	}
	for key, t := range routes {
		v.SlowRoutes = append(v.SlowRoutes, RouteSummary{key, t})
	}
	v.RecentSlowRoutes = []RouteSummary{}
	for key, t := range recentRoutes {
		v.RecentSlowRoutes = append(v.RecentSlowRoutes, RouteSummary{key, t})
	}
	sortRoutes := func(rows []RouteSummary) {
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.Totals.Finite.SumMS != b.Totals.Finite.SumMS {
				return a.Totals.Finite.SumMS > b.Totals.Finite.SumMS
			}
			return a.Route < b.Route
		})
	}
	sortRoutes(v.SlowRoutes)
	sortRoutes(v.RecentSlowRoutes)
	return v, nil
}

// Usage is an in-memory capacity reading; no measurement opens storage.
func (r *Runtime) Usage(name string) (int64, bool) {
	if r == nil {
		return 0, false
	}
	switch name {
	case "queue":
		return int64(len(r.events)), true
	case "routes":
		return r.routeUsed.Load(), true
	case "active":
		n := r.active.Load()
		if n > r.Limit("active") {
			n = r.Limit("active")
		}
		return n, true
	case "readers":
		return int64(len(r.readers)), true
	case "store":
		return r.storeBytes.Load(), *r.storeState.Load() == "ok"
	case "minutes":
		return r.minuteUsed.Load(), true
	case "hours":
		return r.hourUsed.Load(), true
	case "snapshot":
		return r.snapshotUsed.Load(), true
	case "retention":
		if at := r.oldestHour.Load(); at > 0 {
			return (time.Now().UnixMilli() - at) / 1000, true
		}
		return 0, true
	}
	return 0, true
}
