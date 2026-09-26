package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func openVerifications(t *testing.T) (*Verifications, context.Context) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	return &Verifications{Store: st, Now: func() time.Time { return now },
		Usage: &UsageLedger{Store: st, Now: func() time.Time { return now }}}, context.Background()
}

func refusalCode(err error) string {
	var r *VerificationError
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// The seed is planted only where the experiment's schedule is, only once,
// with the dates read in the machine's zone, and a deleted seed stays gone.
func TestTheCompactionSeedIsPlantedOnceWhereItsScheduleIs(t *testing.T) {
	v, ctx := openVerifications(t)
	taipei := time.FixedZone("UTC+8", 8*3600)
	v.Location = taipei
	if planted, err := v.SeedVerifications(ctx); err != nil || planted {
		t.Fatalf("planted on a machine without the schedule: %v %v", planted, err)
	}
	// A schedule whose id does not hash to the digest is not the experiment's.
	if err := v.Store.CreateScheduleFile(ctx, "5c000000-0000-4000-8000-000000000001", []byte(`{}`), v.now(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if planted, err := v.SeedVerifications(ctx); err != nil || planted {
		t.Fatalf("planted beside another schedule: %v %v", planted, err)
	}
	const fixture = "5c000000-0000-4000-8000-000000000002"
	defer func(was string) { CompactionScheduleDigest = was }(CompactionScheduleDigest)
	CompactionScheduleDigest = ScheduleDigest(fixture)
	if err := v.Store.CreateScheduleFile(ctx, fixture, []byte(`{}`), v.now(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if planted, err := v.SeedVerifications(ctx); err != nil || !planted {
		t.Fatalf("not planted: %v %v", planted, err)
	}
	if planted, err := v.SeedVerifications(ctx); err != nil || planted {
		t.Fatalf("planted twice: %v %v", planted, err)
	}
	rows, err := v.List(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%d records, %v", len(rows), err)
	}
	r := rows[0]
	if r.Title != "壓縮門檻實驗（300000）" || r.ScheduleID != fixture || r.Seed != compactionSeed ||
		r.SourceKind != VerificationSourceCompaction || r.SourceSince != "14d" || len(r.Criteria) != 3 ||
		!strings.HasPrefix(r.Criteria[2].Text, "抽查 3–5 個") {
		t.Fatalf("%+v", r)
	}
	if got := time.Unix(r.DueAt, 0).In(taipei).Format("2006-01-02 15:04"); got != "2026-10-03 09:00" {
		t.Fatalf("due %s", got)
	}
	if got := time.Unix(r.StartedAt, 0).In(taipei).Format("2006-01-02"); got != "2026-09-26" {
		t.Fatalf("started %s", got)
	}
	if err := v.Delete(ctx, r.ID, true); err != nil {
		t.Fatal(err)
	}
	if planted, err := v.SeedVerifications(ctx); err != nil || planted {
		t.Fatalf("a deleted seed came back: %v %v", planted, err)
	}
}

// What a record may say is checked before the store: an unknown data source
// is refused by its name, and every bound by the field it is about.
func TestAVerificationIsRefusedByWhatIsWrongWithIt(t *testing.T) {
	v, ctx := openVerifications(t)
	due := v.now().Add(7 * 24 * time.Hour)
	good := VerificationInput{Title: "a check", DueAt: due, Criteria: []string{"it holds"}}
	for _, tc := range []struct {
		name string
		edit func(in *VerificationInput)
		code string
		says string
	}{
		{"unknown source", func(in *VerificationInput) { in.Source = &VerificationSource{Kind: "weather"} }, "unknown_source", `"weather"`},
		{"bad since", func(in *VerificationInput) {
			in.Source = &VerificationSource{Kind: VerificationSourceCompaction, Since: "14w"}
		}, "bad_request", "source.since"},
		{"no title", func(in *VerificationInput) { in.Title = "  " }, "bad_request", "title"},
		{"long title", func(in *VerificationInput) { in.Title = strings.Repeat("字", verificationTitleLimit+1) }, "bad_request", "title"},
		{"no due", func(in *VerificationInput) { in.DueAt = time.Time{} }, "bad_request", "due_at"},
		{"due before start", func(in *VerificationInput) { in.StartedAt = due.Add(time.Hour) }, "bad_request", "due_at"},
		{"too many criteria", func(in *VerificationInput) {
			in.Criteria = make([]string, verificationCriteriaLimit+1)
			for i := range in.Criteria {
				in.Criteria[i] = "c"
			}
		}, "bad_request", "criteria"},
		{"empty criterion", func(in *VerificationInput) { in.Criteria = []string{"ok", ""} }, "bad_request", "criteria[1]"},
		{"control character", func(in *VerificationInput) { in.Why = "a\x1bb" }, "bad_request", "why"},
		{"schedule shape", func(in *VerificationInput) { in.ScheduleID = "../x" }, "bad_request", "schedule_id"},
	} {
		in := good
		tc.edit(&in)
		_, _, err := v.Create(ctx, in, "")
		if refusalCode(err) != tc.code || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	rec, created, err := v.Create(ctx, good, "")
	if err != nil || !created || rec.StartedAt != v.now().Unix() {
		t.Fatalf("%+v %v %v", rec, created, err)
	}
	if err := v.SetCriterion(ctx, rec.ID, 0, "maybe"); refusalCode(err) != "bad_request" {
		t.Fatalf("state maybe: %v", err)
	}
	if err := v.Close(ctx, rec.ID, "open", "why not"); refusalCode(err) != "bad_request" {
		t.Fatalf("closed as open: %v", err)
	}
	if err := v.Close(ctx, rec.ID, VerificationAccepted, ""); refusalCode(err) != "bad_request" {
		t.Fatalf("closed with no reason: %v", err)
	}
	if err := v.Delete(ctx, rec.ID, false); refusalCode(err) != "verification_open" {
		t.Fatalf("deleted while open: %v", err)
	}
	if _, err := v.AppendNote(ctx, "nothing-here", "hi", VerificationAuthor{Kind: AuthorPerson}, ""); refusalCode(err) != "not_found" {
		t.Fatalf("a note on nothing: %v", err)
	}
}

// A scheduled task writes only on a record linked to the schedule it runs
// for, signed with its task id; a task no schedule started writes nothing.
func TestAScheduledTaskWritesOnlyOnItsOwnSchedulesRecord(t *testing.T) {
	v, ctx := openVerifications(t)
	due := v.now().Add(time.Hour)
	linked, _, err := v.Create(ctx, VerificationInput{Title: "linked", DueAt: due, ScheduleID: "sched-1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := v.Create(ctx, VerificationInput{Title: "other", DueAt: due}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.AppendScheduledNote(ctx, "", "t1", linked.ID, "readout", ""); refusalCode(err) != "not_scheduled" {
		t.Fatalf("an unscheduled task: %v", err)
	}
	if _, err := v.AppendScheduledNote(ctx, "sched-1", "t1", other.ID, "readout", ""); refusalCode(err) != "schedule_mismatch" {
		t.Fatalf("another record: %v", err)
	}
	n, err := v.AppendScheduledNote(ctx, "sched-1", "t1", linked.ID, "readout", "")
	if err != nil || n.AuthorKind != AuthorSession || n.Author != "task:t1" {
		t.Fatalf("%+v %v", n, err)
	}
}

// The experiment's schedule is in this public repository only as a digest:
// 64 hex digits, and never the id itself.
func TestTheSeedsScheduleIsNamedOnlyByItsDigest(t *testing.T) {
	if len(CompactionScheduleDigest) != 64 || strings.Trim(CompactionScheduleDigest, "0123456789abcdef") != "" {
		t.Fatalf("digest %q", CompactionScheduleDigest)
	}
	if ScheduleDigest("a") != "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb" {
		t.Fatal("ScheduleDigest is not SHA-256 of the id")
	}
}

// Opening a record reads its data source now: the comparison is there for a
// `compaction_compare` record, and a record with no source has no data.
func TestAVerificationsDataIsReadWhenItIsOpened(t *testing.T) {
	v, ctx := openVerifications(t)
	due := v.now().Add(time.Hour)
	with, _, err := v.Create(ctx, VerificationInput{Title: "with", DueAt: due,
		Source: &VerificationSource{Kind: VerificationSourceCompaction, Since: "7d"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := v.Get(ctx, with.ID)
	if err != nil || data == nil || data.Kind != VerificationSourceCompaction || data.Compaction == nil || data.Error != "" {
		t.Fatalf("%+v %v", data, err)
	}
	if got := v.now().Sub(data.Compaction.Since); got != 7*24*time.Hour {
		t.Fatalf("since %v", got)
	}
	without, _, _ := v.Create(ctx, VerificationInput{Title: "without", DueAt: due}, "")
	if _, data, err := v.Get(ctx, without.ID); err != nil || data != nil {
		t.Fatalf("%+v %v", data, err)
	}
	v.Usage = nil
	if _, data, _ := v.Get(ctx, with.ID); data == nil || data.Error == "" {
		t.Fatalf("no ledger: %+v", data)
	}
}
