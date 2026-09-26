package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func verificationFixture(id string) Verification {
	return Verification{ID: id, Title: "a check", Why: "because", StartedAt: 100, DueAt: 200,
		SourceKind: "compaction_compare", SourceSince: "14d", ScheduleID: "sched-1", CreatedAt: 50,
		Criteria: []VerificationCriterion{{Text: "first"}, {Text: "second"}}}
}

// A record comes back as it went in, with its criteria unset and in order,
// and the list puts the open ones first by due time.
func TestAVerificationIsKeptWithItsCriteriaInOrder(t *testing.T) {
	st, ctx := openSnippetStore(t)
	late := verificationFixture("v-late")
	late.DueAt = 900
	if _, created, err := st.CreateVerification(ctx, late, "", 10); err != nil || !created {
		t.Fatal(err, created)
	}
	got, created, err := st.CreateVerification(ctx, verificationFixture("v-soon"), "", 10)
	if err != nil || !created {
		t.Fatal(err, created)
	}
	if got.Status != "open" || len(got.Criteria) != 2 || got.Criteria[1].Text != "second" ||
		got.Criteria[1].Index != 1 || got.Criteria[0].State != "unset" || got.SourceSince != "14d" {
		t.Fatalf("%+v", got)
	}
	if err := st.CloseVerification(ctx, "v-soon", "accepted", "held", time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	third := verificationFixture("v-mid")
	third.DueAt = 500
	if _, _, err := st.CreateVerification(ctx, third, "", 10); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Verifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range rows {
		order = append(order, r.ID)
	}
	if len(order) != 3 || order[0] != "v-mid" || order[1] != "v-late" || order[2] != "v-soon" {
		t.Fatalf("order %v", order)
	}
}

// A create with a key already used answers the first record, unchanged, and
// the store's limit is counted inside the write.
func TestAVerificationCreateKeyAndLimit(t *testing.T) {
	st, ctx := openSnippetStore(t)
	if _, created, err := st.CreateVerification(ctx, verificationFixture("v-1"), "k1", 2); err != nil || !created {
		t.Fatal(err)
	}
	again, created, err := st.CreateVerification(ctx, verificationFixture("v-2"), "k1", 2)
	if err != nil || created || again.ID != "v-1" {
		t.Fatalf("a repeated key made %+v %v %v", again.ID, created, err)
	}
	if _, _, err := st.CreateVerification(ctx, verificationFixture("v-3"), "k3", 2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateVerification(ctx, verificationFixture("v-4"), "k4", 2); !errors.Is(err, ErrVerificationFull) {
		t.Fatalf("the third record was not refused: %v", err)
	}
}

// Notes append in order, a repeated key is the same note, the per-record
// limit holds, and a schedule-bound write only lands on a record linked to
// that schedule.
func TestAVerificationNoteAppendsOnceAndOnlyWhereItMay(t *testing.T) {
	st, ctx := openSnippetStore(t)
	if _, _, err := st.CreateVerification(ctx, verificationFixture("v-1"), "", 10); err != nil {
		t.Fatal(err)
	}
	n := VerificationNote{At: 400, AuthorKind: "person", Text: "looked"}
	first, err := st.AppendVerificationNote(ctx, "v-1", n, "nk", "", 2)
	if err != nil || first.ID == 0 {
		t.Fatal(err, first)
	}
	again, err := st.AppendVerificationNote(ctx, "v-1", VerificationNote{At: 401, AuthorKind: "person", Text: "other"}, "nk", "", 2)
	if err != nil || again.ID != first.ID || again.Text != "looked" {
		t.Fatalf("a repeated key wrote %+v %v", again, err)
	}
	if _, err := st.AppendVerificationNote(ctx, "v-1", n, "", "other-schedule", 2); !errors.Is(err, ErrVerificationSchedule) {
		t.Fatalf("a note for another schedule: %v", err)
	}
	if _, err := st.AppendVerificationNote(ctx, "v-1", VerificationNote{At: 402, AuthorKind: "session", Author: "task:t1", Text: "readout"}, "", "sched-1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendVerificationNote(ctx, "v-1", n, "", "", 2); !errors.Is(err, ErrVerificationNotesFull) {
		t.Fatalf("the third note: %v", err)
	}
	if _, err := st.AppendVerificationNote(ctx, "v-none", n, "", "", 2); !errors.Is(err, ErrVerificationNotFound) {
		t.Fatalf("a note on nothing: %v", err)
	}
	got, _, _ := st.Verification(ctx, "v-1")
	if len(got.Notes) != 2 || got.Notes[1].Author != "task:t1" || got.UpdatedAt != 402 {
		t.Fatalf("%+v", got)
	}
}

// A criterion is marked while the record is open; a closed record keeps what
// it was closed on; a verdict is not overwritten; an open record is deleted
// only with force, and its criteria and notes go with it.
func TestAVerificationClosesOnceAndIsDeletedOnlyWhenClosedOrForced(t *testing.T) {
	st, ctx := openSnippetStore(t)
	for _, id := range []string{"v-1", "v-2"} {
		if _, _, err := st.CreateVerification(ctx, verificationFixture(id), "", 10); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Unix(500, 0)
	if err := st.SetVerificationCriterion(ctx, "v-1", 1, "passed", now); err != nil {
		t.Fatal(err)
	}
	if err := st.SetVerificationCriterion(ctx, "v-1", 5, "passed", now); !errors.Is(err, ErrVerificationCriterion) {
		t.Fatalf("criterion 5: %v", err)
	}
	if err := st.DeleteVerification(ctx, "v-1", false); !errors.Is(err, ErrVerificationOpen) {
		t.Fatalf("an open record was deleted: %v", err)
	}
	if err := st.CloseVerification(ctx, "v-1", "rejected", "it did not hold", now); err != nil {
		t.Fatal(err)
	}
	if err := st.CloseVerification(ctx, "v-1", "rejected", "again", now); err != nil {
		t.Fatalf("closing it the same way again: %v", err)
	}
	if err := st.CloseVerification(ctx, "v-1", "accepted", "changed my mind", now); !errors.Is(err, ErrVerificationClosed) {
		t.Fatalf("a verdict was overwritten: %v", err)
	}
	if err := st.SetVerificationCriterion(ctx, "v-1", 0, "failed", now); !errors.Is(err, ErrVerificationClosed) {
		t.Fatalf("a closed record's criterion changed: %v", err)
	}
	got, _, _ := st.Verification(ctx, "v-1")
	if got.Status != "rejected" || got.CloseReason != "it did not hold" || got.ClosedAt != 500 ||
		got.Criteria[1].State != "passed" || got.Criteria[1].UpdatedAt != 500 {
		t.Fatalf("%+v", got)
	}
	if err := st.DeleteVerification(ctx, "v-1", false); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteVerification(ctx, "v-2", true); err != nil {
		t.Fatalf("force: %v", err)
	}
	if err := st.DeleteVerification(ctx, "v-2", true); !errors.Is(err, ErrVerificationNotFound) {
		t.Fatalf("deleted twice: %v", err)
	}
	var left int
	if err := st.db.QueryRowContext(context.Background(),
		`SELECT (SELECT COUNT(*) FROM verification_criteria) + (SELECT COUNT(*) FROM verification_notes)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d criteria and notes outlived their records (%v)", left, err)
	}
}

// A seed is planted once. Planting it again writes nothing; deleting it keeps
// it deleted; a record the person already made for the same source and
// schedule stands in for it.
func TestASeedIsPlantedOnceAndStaysDeleted(t *testing.T) {
	st, ctx := openSnippetStore(t)
	seed := verificationFixture("v-seed")
	seed.Seed = "the-seed"
	if planted, err := st.SeedVerification(ctx, seed, 10); err != nil || !planted {
		t.Fatal(err, planted)
	}
	seed.ID = "v-seed-2"
	if planted, err := st.SeedVerification(ctx, seed, 10); err != nil || planted {
		t.Fatalf("planted twice: %v %v", planted, err)
	}
	if err := st.DeleteVerification(ctx, "v-seed", true); err != nil {
		t.Fatal(err)
	}
	seed.ID = "v-seed-3"
	if planted, err := st.SeedVerification(ctx, seed, 10); err != nil || planted {
		t.Fatalf("a deleted seed came back: %v %v", planted, err)
	}
	rows, _ := st.Verifications(ctx)
	if len(rows) != 0 {
		t.Fatalf("%d records", len(rows))
	}

	// On a store where the person made the same record first.
	other, ctx := openSnippetStore(t)
	if _, _, err := other.CreateVerification(ctx, verificationFixture("v-mine"), "", 10); err != nil {
		t.Fatal(err)
	}
	seed.ID = "v-seed-4"
	if planted, err := other.SeedVerification(ctx, seed, 10); err != nil || planted {
		t.Fatalf("planted beside the person's own: %v %v", planted, err)
	}
	rows, _ = other.Verifications(ctx)
	if len(rows) != 1 || rows[0].ID != "v-mine" {
		t.Fatalf("%+v", rows)
	}
}
