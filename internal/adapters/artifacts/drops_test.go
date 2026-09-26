package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// pushed is the notices one reading of a row owes a person's phone: what the
// capacity beat hands the outbox rather than only writes down.
func pushed(t *testing.T, name string, r capacity.Reading, now time.Time) []capacity.Event {
	t.Helper()
	var entry capacity.Entry
	for _, e := range capacity.Register() {
		if e.Name == name {
			entry = e
		}
	}
	if entry.Name == "" {
		t.Fatalf("no row %s", name)
	}
	_, events := capacity.NewTracker().Observe(capacity.Resolved{Entry: entry, Limit: entry.Limit}, r, now)
	var out []capacity.Event
	for _, e := range events {
		if e.Kind == capacity.EventNotify && capacity.Pushes(entry) {
			out = append(out, e)
		}
	}
	return out
}

// The cache as somebody who sends pictures every day leaves it: more than
// forty ordinary screenshots, all of them this morning's. That is the cache
// working, not a cache too small, and a phone is not buzzed about it.
func TestADropCacheInItsUsualStateDoesNotPush(t *testing.T) {
	d := NewDrops(filepath.Join(t.TempDir(), "state"))
	now := time.Now()
	picture := make([]byte, 300<<10)
	for i := 0; i < 45; i++ {
		if _, err := d.Store(picture, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if got := pushed(t, capacity.ArtifactsDrops, d.Reading(), now.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("forty-five screenshots from this morning pushed %+v", got)
	}
}

// store writes one picture of size bytes into d as though it were sent at at.
func store(t *testing.T, d *Drops, size int, at time.Time) string {
	t.Helper()
	path, err := d.Store(make([]byte, size), at)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func testDrops(t *testing.T, keep int, maxBytes int64) *Drops {
	t.Helper()
	return &Drops{Dir: filepath.Join(t.TempDir(), "drops"), Keep: keep, MaxBytes: maxBytes,
		MaxAge: DropsAgeLimit, Young: DropsYoungLimit}
}

// limits N16: every picture the drop cache removes was typed into a prompt as
// a path, so each removal is counted — by age as expired, by bytes as evicted
// — and the row is measurable before it.
func TestDropsCountWhatTheyPrune(t *testing.T) {
	d := testDrops(t, 1, 250)
	if r := d.Reading(); !r.Known || r.Used != 0 {
		t.Fatalf("an empty cache reads %+v", r)
	}
	heard := 0
	d.OnStored(func() { heard++ })
	now := time.Now()
	store(t, d, 10, now.Add(-10*24*time.Hour))
	store(t, d, 100, now.Add(-2*24*time.Hour))
	store(t, d, 100, now.Add(-time.Hour))
	store(t, d, 100, now)
	r := d.ReadingAt(now)
	if r.Used != 200 || r.Counters.Expired != 1 || r.Counters.Evicted != 1 || heard != 4 {
		t.Fatalf("after four stores: %+v, heard %d", r, heard)
	}
	if r.WindowSeconds != int64(DropsAgeLimit/time.Second) {
		t.Fatalf("the reading does not say how long a picture is kept: %+v", r)
	}
}

// A file past retention goes, and one within it stays, when neither is among
// the newest Keep.
func TestADropPastItsAgeIsRemoved(t *testing.T) {
	d := testDrops(t, 1, 1<<20)
	now := time.Now()
	old := store(t, d, 10, now.Add(-DropsAgeLimit-time.Hour))
	week := store(t, d, 10, now.Add(-DropsAgeLimit+time.Hour))
	store(t, d, 10, now)
	if exists(old) || !exists(week) {
		t.Fatalf("past its age kept %v, within its age kept %v", exists(old), exists(week))
	}
}

// Age is measured on every reading too, so a cache nobody has sent to for a
// week does not keep what the next picture would let go.
func TestAReadingLetsGoWhatIsPastItsAge(t *testing.T) {
	d := testDrops(t, 1, 1<<20)
	now := time.Now()
	first := store(t, d, 10, now)
	last := store(t, d, 10, now.Add(time.Second))
	r := d.ReadingAt(now.Add(DropsAgeLimit + time.Hour))
	if exists(first) || !exists(last) || r.Used != 10 || r.Counters.Expired != 1 {
		t.Fatalf("a week on: first kept %v, last kept %v, %+v", exists(first), exists(last), r)
	}
}

// Forty is no longer a wall: an afternoon of screenshots under the byte cap is
// all kept, the one typed into a prompt this morning included.
func TestYoungDropsAreKeptPastTheCountWhileUnderTheByteCap(t *testing.T) {
	d := testDrops(t, DropsKeep, 1<<20)
	now := time.Now()
	morning := store(t, d, 100, now.Add(-4*time.Hour))
	for i := 0; i < 2*DropsKeep; i++ {
		store(t, d, 100, now.Add(time.Duration(i)*time.Second))
	}
	r := d.ReadingAt(now.Add(time.Hour))
	if !exists(morning) || r.Used != int64(100*(2*DropsKeep+1)) || r.Counters.Evicted+r.Counters.Expired != 0 {
		t.Fatalf("morning kept %v, %+v", exists(morning), r)
	}
}

// Past the byte cap the oldest goes first, whatever its age, and the newest
// is never the one removed.
func TestTheByteCapLetsTheOldestGoFirst(t *testing.T) {
	d := testDrops(t, DropsKeep, 300)
	now := time.Now()
	var paths []string
	for i := 0; i < 5; i++ {
		paths = append(paths, store(t, d, 100, now.Add(time.Duration(i)*time.Minute)))
	}
	for i, p := range paths {
		if want := i >= 2; exists(p) != want {
			t.Fatalf("picture %d kept %v, want %v", i, exists(p), want)
		}
	}
	huge := store(t, d, 1000, now.Add(time.Hour))
	if !exists(huge) || d.ReadingAt(now.Add(time.Hour)).Used != 1000 {
		t.Fatal("a picture larger than the cap was not kept alone: it was just typed into a prompt")
	}
}

// Age alone never removes the newest Keep: somebody who sends a picture a
// fortnight still has the last ones they sent.
func TestTheNewestDropsSurviveTheirAge(t *testing.T) {
	d := testDrops(t, 3, 1<<20)
	now := time.Now()
	var paths []string
	for i := 5; i > 0; i-- {
		paths = append(paths, store(t, d, 10, now.Add(-time.Duration(i)*DropsAgeLimit)))
	}
	d.ReadingAt(now)
	for i, p := range paths {
		if want := i >= 2; exists(p) != want {
			t.Fatalf("picture %d, %d weeks old: kept %v, want %v", i, 5-i, exists(p), want)
		}
	}
}

// The one thing about this cache a phone hears: the byte cap removing a
// picture less than a day old. The cap removing a three-day-old one is the
// cache working, said in diagnostics and the log and not pushed.
func TestOnlyAYoungEvictionPushes(t *testing.T) {
	now := time.Now()
	ordinary := testDrops(t, DropsKeep, 250)
	store(t, ordinary, 100, now.Add(-3*24*time.Hour))
	store(t, ordinary, 100, now.Add(-time.Hour))
	store(t, ordinary, 100, now)
	if r := ordinary.YoungReadingAt(now); r.Used != 0 || ordinary.ReadingAt(now).Counters.Evicted != 1 {
		t.Fatalf("a three-day-old picture let go reads young %+v", r)
	}
	if got := pushed(t, capacity.ArtifactsDropsYoung, ordinary.YoungReadingAt(now), now); len(got) != 0 {
		t.Fatalf("an ordinary eviction pushed %+v", got)
	}
	if got := pushed(t, capacity.ArtifactsDrops, ordinary.ReadingAt(now), now); len(got) != 0 {
		t.Fatalf("the byte row pushed %+v", got)
	}

	young := testDrops(t, DropsKeep, 250)
	store(t, young, 100, now.Add(-2*time.Hour))
	store(t, young, 100, now.Add(-time.Hour))
	store(t, young, 100, now)
	r := young.YoungReadingAt(now)
	got := pushed(t, capacity.ArtifactsDropsYoung, r, now)
	if r.Used != 1 || r.Counters.Evicted != 1 || len(got) != 1 || got[0].Payload["state"] != string(capacity.Full) {
		t.Fatalf("a two-hour-old picture let go: %+v, pushed %+v", r, got)
	}
}

// A day without a young eviction is written down as the row going back to
// ok, and not pushed: nothing happened to tell anybody about, and a pushed
// recovery would spend the day's notice the next time one does happen.
func TestAYoungEvictionIsForgottenQuietlyAfterADay(t *testing.T) {
	d := testDrops(t, DropsKeep, 250)
	now := time.Now()
	for i := 0; i < 3; i++ {
		store(t, d, 100, now.Add(time.Duration(i)*time.Minute))
	}
	entry := capacity.Entry{}
	for _, e := range capacity.Register() {
		if e.Name == capacity.ArtifactsDropsYoung {
			entry = e
		}
	}
	res := capacity.Resolved{Entry: entry, Limit: entry.Limit}
	tracker := capacity.NewTracker()
	if st, _ := tracker.Observe(res, d.YoungReadingAt(now), now); st.State != capacity.Full || st.Notices != 1 {
		t.Fatalf("young eviction: %+v", st)
	}
	later := now.Add(DropsYoungLimit + time.Hour)
	st, events := tracker.Observe(res, d.YoungReadingAt(later), later)
	if st.State != capacity.OK {
		t.Fatalf("a day on: %+v", st)
	}
	for _, e := range events {
		if e.Kind == capacity.EventNotify {
			t.Fatalf("a day without a young eviction announced itself: %+v", e)
		}
	}
	title, body := capacity.NoticeText(entry, capacity.Full, 2, 1, time.Time{}, nil)
	for _, want := range []string{"2 張", capacity.ArtifactsDrops, "重新貼一次"} {
		if !strings.Contains(title+body, want) {
			t.Errorf("the notice %q / %q lacks %q", title, body, want)
		}
	}
}

// The adapter's bounds and the register's rows are one number each.
func TestTheDropBoundsAreTheRegistersRows(t *testing.T) {
	if capacity.Default(capacity.ArtifactsDrops) != MaxDropsBytes {
		t.Fatalf("artifacts.drops is %d, the cache caps at %d", capacity.Default(capacity.ArtifactsDrops), MaxDropsBytes)
	}
}
