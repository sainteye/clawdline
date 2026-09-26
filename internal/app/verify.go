package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// Things waiting to be verified (docs/verifications.md).
//
// A person who is told "remember to check this on the third" forgets it. A
// verification is that sentence kept: what is being checked and why, when it
// is due, the few sentences it will be judged by, where its evidence comes
// from — read live every time the record is opened — and the notes written on
// it over time, until the person accepts or rejects it and deletes it.

// The bounds (docs/limits.md N45). Every one is refused at the door by name:
// nothing here is evicted, because every row is something a person asked to
// be reminded of.
const (
	// verificationTotalLimit is how many records the store keeps, open and
	// closed together. The next is refused until one is deleted.
	verificationTotalLimit = 200
	// verificationCriteriaLimit is how many sentences one record is judged by.
	verificationCriteriaLimit = 12
	// verificationNotesLimit is how many notes one record takes; a daily
	// readout for half a year fits.
	verificationNotesLimit = 200
	// The text lengths, in characters.
	verificationTitleLimit     = 200
	verificationWhyLimit       = 4000
	verificationCriterionLimit = 500
	verificationNoteLimit      = 8000
	verificationReasonLimit    = 2000
	verificationAuthorLimit    = 200
)

// VerificationSourceCompaction is the one data source there is: the
// compaction comparison (UsageLedger.CompareCompaction), over `since`.
const VerificationSourceCompaction = "compaction_compare"

// The states a record and a criterion can be in.
const (
	VerificationOpen     = "open"
	VerificationAccepted = "accepted"
	VerificationRejected = "rejected"

	CriterionUnset  = "unset"
	CriterionPassed = "passed"
	CriterionFailed = "failed"

	AuthorPerson  = "person"
	AuthorSession = "session"
)

// VerificationError is a refusal with the code a route answers it under.
type VerificationError struct {
	Code    string
	Message string
}

func (e *VerificationError) Error() string { return e.Code + ": " + e.Message }

func verificationRefusal(code, format string, a ...any) error {
	return &VerificationError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Verifications is the service the routes and the seed use.
type Verifications struct {
	Store *store.Store
	// Usage resolves a `compaction_compare` source. Nil answers the data as
	// unreadable, not the record as broken.
	Usage *UsageLedger
	Now   func() time.Time
	// Location is where the seed's calendar dates are read; nil is the
	// machine's own zone, which is where its schedule fires.
	Location *time.Location
}

func (v *Verifications) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// VerificationSource is where a record's evidence is read from.
type VerificationSource struct {
	Kind  string
	Since string
}

// VerificationInput is a new record as a route or a command asked for it.
type VerificationInput struct {
	Title, Why string
	// StartedAt zero is now.
	StartedAt  time.Time
	DueAt      time.Time
	Criteria   []string
	Source     *VerificationSource
	ScheduleID string
}

// VerificationAuthor is who wrote a note.
type VerificationAuthor struct {
	Kind string
	Name string
}

// VerificationData is a source read live: exactly one of its answers, or the
// sentence saying why it could not be read.
type VerificationData struct {
	Kind       string
	Compaction *CompactionComparison
	Error      string
}

// List is every record, open ones first by due time.
func (v *Verifications) List(ctx context.Context) ([]store.Verification, error) {
	return v.Store.Verifications(ctx)
}

// Get is one record with its data source read now.
func (v *Verifications) Get(ctx context.Context, id string) (store.Verification, *VerificationData, error) {
	rec, found, err := v.Store.Verification(ctx, id)
	if err != nil {
		return store.Verification{}, nil, err
	}
	if !found {
		return store.Verification{}, nil, notFoundVerification(id)
	}
	return rec, v.resolve(ctx, rec), nil
}

func (v *Verifications) resolve(ctx context.Context, rec store.Verification) *VerificationData {
	switch rec.SourceKind {
	case "":
		return nil
	case VerificationSourceCompaction:
		out := &VerificationData{Kind: rec.SourceKind}
		if v.Usage == nil {
			out.Error = "The token ledger is not read on this daemon."
			return out
		}
		got, err := v.Usage.CompareCompactionSince(ctx, rec.SourceSince)
		if err != nil {
			out.Error = "The comparison could not be read: " + err.Error()
			return out
		}
		out.Compaction = &got
		return out
	}
	// A kind stored by a newer daemon: say so rather than guess.
	return &VerificationData{Kind: rec.SourceKind, Error: "This daemon does not read the data source " + rec.SourceKind + "."}
}

// Create checks a new record and writes it. key, when not empty, makes a
// repeated create answer the first one's record.
func (v *Verifications) Create(ctx context.Context, in VerificationInput, key string) (store.Verification, bool, error) {
	now := v.now()
	rec, err := verificationRecord(in, now)
	if err != nil {
		return store.Verification{}, false, err
	}
	rec.ID = newVerificationID()
	got, created, err := v.Store.CreateVerification(ctx, rec, key, verificationTotalLimit)
	if errors.Is(err, store.ErrVerificationFull) {
		return got, false, verificationRefusal("verification_limit_reached",
			"This machine keeps at most %d verifications; delete a closed one first.", verificationTotalLimit)
	}
	return got, created, err
}

func verificationRecord(in VerificationInput, now time.Time) (store.Verification, error) {
	title := strings.TrimSpace(in.Title)
	why := strings.TrimSpace(in.Why)
	if err := textBound("title", title, 1, verificationTitleLimit); err != nil {
		return store.Verification{}, err
	}
	if err := textBound("why", why, 0, verificationWhyLimit); err != nil {
		return store.Verification{}, err
	}
	started := in.StartedAt
	if started.IsZero() {
		started = now
	}
	if in.DueAt.IsZero() {
		return store.Verification{}, verificationRefusal("bad_request", "due_at is required.")
	}
	if in.DueAt.Before(started) {
		return store.Verification{}, verificationRefusal("bad_request", "due_at is before started_at.")
	}
	if len(in.Criteria) > verificationCriteriaLimit {
		return store.Verification{}, verificationRefusal("bad_request",
			"A verification is judged by at most %d criteria.", verificationCriteriaLimit)
	}
	rec := store.Verification{Title: title, Why: why, StartedAt: started.Unix(), DueAt: in.DueAt.Unix(),
		CreatedAt: now.Unix(), Criteria: []store.VerificationCriterion{}}
	for i, c := range in.Criteria {
		c = strings.TrimSpace(c)
		if err := textBound(fmt.Sprintf("criteria[%d]", i), c, 1, verificationCriterionLimit); err != nil {
			return store.Verification{}, err
		}
		rec.Criteria = append(rec.Criteria, store.VerificationCriterion{Index: i, Text: c, State: CriterionUnset})
	}
	if in.Source != nil {
		kind, since, err := checkSource(*in.Source, now)
		if err != nil {
			return store.Verification{}, err
		}
		rec.SourceKind, rec.SourceSince = kind, since
	}
	schedule := strings.TrimSpace(in.ScheduleID)
	if schedule != "" && !verificationIDShape(schedule) {
		return store.Verification{}, verificationRefusal("bad_request",
			"schedule_id is letters, digits and dashes, at most 64.")
	}
	rec.ScheduleID = schedule
	return rec, nil
}

// checkSource refuses a kind this daemon cannot read by its name, and a
// parameter the kind would refuse when read.
func checkSource(s VerificationSource, now time.Time) (string, string, error) {
	switch s.Kind {
	case VerificationSourceCompaction:
		since := strings.TrimSpace(s.Since)
		if since == "" {
			since = "14d"
		}
		if _, err := ParseCompareSince(since, now); err != nil {
			return "", "", verificationRefusal("bad_request", "source.since: %v.", err)
		}
		return s.Kind, since, nil
	}
	return "", "", verificationRefusal("unknown_source",
		"The data source %q is not one this daemon reads; it reads %s.", s.Kind, VerificationSourceCompaction)
}

// AppendNote writes one note, by the person or by a session.
func (v *Verifications) AppendNote(ctx context.Context, id, text string, by VerificationAuthor, key string) (store.VerificationNote, error) {
	return v.appendNote(ctx, id, text, by, key, "")
}

// AppendScheduledNote is the one write a scheduled task's own secret opens:
// a note on a record linked to the schedule the task runs for, written under
// the task's id. A task that no schedule started writes nothing here.
func (v *Verifications) AppendScheduledNote(ctx context.Context, schedule, task, id, text, key string) (store.VerificationNote, error) {
	if schedule == "" {
		return store.VerificationNote{}, verificationRefusal("not_scheduled",
			"Only a task a schedule started may write a note with its task secret.")
	}
	return v.appendNote(ctx, id, text, VerificationAuthor{Kind: AuthorSession, Name: "task:" + task}, key, schedule)
}

func (v *Verifications) appendNote(ctx context.Context, id, text string, by VerificationAuthor, key, schedule string) (store.VerificationNote, error) {
	text = strings.TrimSpace(text)
	if err := textBound("text", text, 1, verificationNoteLimit); err != nil {
		return store.VerificationNote{}, err
	}
	if by.Kind != AuthorPerson && by.Kind != AuthorSession {
		return store.VerificationNote{}, verificationRefusal("bad_request", "A note is written by a person or a session.")
	}
	if err := textBound("author", by.Name, 0, verificationAuthorLimit); err != nil {
		return store.VerificationNote{}, err
	}
	n, err := v.Store.AppendVerificationNote(ctx, id, store.VerificationNote{At: v.now().Unix(),
		AuthorKind: by.Kind, Author: by.Name, Text: text}, key, schedule, verificationNotesLimit)
	switch {
	case errors.Is(err, store.ErrVerificationNotFound):
		return n, notFoundVerification(id)
	case errors.Is(err, store.ErrVerificationSchedule):
		return n, verificationRefusal("schedule_mismatch",
			"Verification %s is not linked to the schedule this task runs for.", id)
	case errors.Is(err, store.ErrVerificationNotesFull):
		return n, verificationRefusal("notes_limit_reached",
			"A verification takes at most %d notes.", verificationNotesLimit)
	}
	return n, err
}

// SetCriterion marks criterion index as passed, failed or unset again.
func (v *Verifications) SetCriterion(ctx context.Context, id string, index int, state string) error {
	switch state {
	case CriterionUnset, CriterionPassed, CriterionFailed:
	default:
		return verificationRefusal("bad_request", "A criterion is passed, failed or unset.")
	}
	err := v.Store.SetVerificationCriterion(ctx, id, index, state, v.now())
	switch {
	case errors.Is(err, store.ErrVerificationNotFound):
		return notFoundVerification(id)
	case errors.Is(err, store.ErrVerificationCriterion):
		return verificationRefusal("not_found", "Verification %s has no criterion %d.", id, index)
	case errors.Is(err, store.ErrVerificationClosed):
		return verificationRefusal("verification_closed", "Verification %s is closed; its criteria are what it was closed on.", id)
	}
	return err
}

// Close settles a record as accepted or rejected, with the reason.
func (v *Verifications) Close(ctx context.Context, id, status, reason string) error {
	if status != VerificationAccepted && status != VerificationRejected {
		return verificationRefusal("bad_request", "A verification closes as accepted or rejected.")
	}
	reason = strings.TrimSpace(reason)
	if err := textBound("reason", reason, 1, verificationReasonLimit); err != nil {
		return err
	}
	err := v.Store.CloseVerification(ctx, id, status, reason, v.now())
	switch {
	case errors.Is(err, store.ErrVerificationNotFound):
		return notFoundVerification(id)
	case errors.Is(err, store.ErrVerificationClosed):
		return verificationRefusal("verification_closed", "Verification %s is already closed the other way.", id)
	}
	return err
}

// Delete removes one record. An open one needs force.
func (v *Verifications) Delete(ctx context.Context, id string, force bool) error {
	err := v.Store.DeleteVerification(ctx, id, force)
	switch {
	case errors.Is(err, store.ErrVerificationNotFound):
		return notFoundVerification(id)
	case errors.Is(err, store.ErrVerificationOpen):
		return verificationRefusal("verification_open",
			"Verification %s is still open: accept or reject it first, or delete it with force.", id)
	}
	return err
}

func notFoundVerification(id string) error {
	return verificationRefusal("not_found", "There is no verification %s.", id)
}

func textBound(field, text string, least, most int) error {
	n := utf8.RuneCountInString(text)
	if n < least {
		return verificationRefusal("bad_request", "%s is required.", field)
	}
	if n > most {
		return verificationRefusal("bad_request", "%s is at most %d characters.", field, most)
	}
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return verificationRefusal("bad_request", "%s holds a control character.", field)
		}
	}
	return nil
}

// VerificationIDShape is what a verification or schedule id may look like:
// letters, digits and dashes, at most 64. The routes check it before the store.
func VerificationIDShape(id string) bool { return verificationIDShape(id) }

func verificationIDShape(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func newVerificationID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// The first record, planted by the daemon (docs/verifications.md "The first
// one"). On 2026-09-26 `claude_auto_compact_window` went to 300000 as an
// experiment, and a schedule that runs once reads the comparison out a week
// later. The seed is only planted on the machine that holds that schedule —
// on any other the experiment is not happening — and only once: its name is
// recorded the moment it is planted, so deleting the record keeps it deleted.
//
// The schedule is named by the SHA-256 of its id, not by the id: this
// repository is public, and a real schedule's id is one machine's, not the
// product's. The daemon finds it by hashing the ids it holds.
const compactionSeed = "compaction-window-300000"

// CompactionScheduleDigest is the SHA-256, in hex, of the experiment's
// schedule id. A variable so a test can point it at a fixture schedule.
var CompactionScheduleDigest = "ddb6757aa8782a4b0454010f83be3e486a341492b8fd48298b021c988f7ff546"

// ScheduleDigest is how a schedule id is compared with
// CompactionScheduleDigest.
func ScheduleDigest(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// SeedVerifications plants the records this daemon owes, answering whether
// it planted one.
func (v *Verifications) SeedVerifications(ctx context.Context) (bool, error) {
	ids, err := v.Store.ScheduleIDs(ctx)
	if err != nil {
		return false, err
	}
	schedule := ""
	for _, id := range ids {
		if ScheduleDigest(id) == CompactionScheduleDigest {
			schedule = id
			break
		}
	}
	if schedule == "" {
		return false, nil
	}
	loc := v.Location
	if loc == nil {
		loc = time.Local
	}
	rec := store.Verification{
		ID:    newVerificationID(),
		Seed:  compactionSeed,
		Title: "壓縮門檻實驗（300000）",
		Why: "2026-09-26 起 `claude_auto_compact_window` 設為 300000。比較設定前後派出的 child task：" +
			"成本有沒有下降、結果有沒有變差（docs/token-ledger.md「Did compacting early pay」）。",
		StartedAt:   time.Date(2026, 9, 26, 0, 0, 0, 0, loc).Unix(),
		DueAt:       time.Date(2026, 10, 3, 9, 0, 0, 0, loc).Unix(),
		SourceKind:  VerificationSourceCompaction,
		SourceSince: "14d",
		ScheduleID:  schedule,
		CreatedAt:   v.now().Unix(),
	}
	for i, text := range []string{
		"300000 組每個 task 成本明顯低於 before-setting",
		"成功率沒有下降、stalled 與重派沒有增加",
		"抽查 3–5 個壓縮過的 task：沒有遺失指示或重做",
	} {
		rec.Criteria = append(rec.Criteria, store.VerificationCriterion{Index: i, Text: text, State: CriterionUnset})
	}
	return v.Store.SeedVerification(ctx, rec, verificationTotalLimit)
}
