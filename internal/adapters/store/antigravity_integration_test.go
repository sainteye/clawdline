package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func consentStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}
func consentCommand(t *testing.T) AntigravityConsentCommand {
	t.Helper()
	return AntigravityConsentCommand{ProjectRoot: filepath.Join(t.TempDir(), "synthetic-project"), Action: AntigravityConsentPrepare, OperationID: "enable-1", HelperSHA256: strings.Repeat("a", 64), PlanSHA256: strings.Repeat("b", 64), Actor: "person-device", Key: "prepare-1"}
}
func consentApply(t *testing.T, s *Store, c AntigravityConsentCommand, want error) AntigravityProjectConsent {
	t.Helper()
	got, err := s.ApplyAntigravityProjectConsent(context.Background(), c)
	if err != want {
		t.Fatalf("action %s rev %d: %v, want %v", c.Action, c.ExpectedRevision, err, want)
	}
	return got
}
func consentRead(t *testing.T, s *Store, root string) AntigravityProjectConsent {
	t.Helper()
	got, err := s.AntigravityProjectConsent(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAntigravityConsentLifecycleAndReplay(t *testing.T) {
	s, _ := consentStore(t)
	c := consentCommand(t)
	if got := consentRead(t, s, c.ProjectRoot); got.Phase != AntigravityConsentAbsent || got.Revision != 0 {
		t.Fatal(got)
	}
	pending := consentApply(t, s, c, nil)
	if pending.Phase != AntigravityConsentPreparing || pending.Revision != 1 {
		t.Fatal(pending)
	}
	if got := consentApply(t, s, c, nil); got != pending {
		t.Fatal("lost prepare acknowledgment changed outcome", got)
	}
	changed := c
	changed.PlanSHA256 = strings.Repeat("c", 64)
	consentApply(t, s, changed, AntigravityConsentKeyConflict)
	active := c
	active.Action = AntigravityConsentActivate
	active.ExpectedRevision = 1
	active.Key = "activate-1"
	for _, field := range []string{"operation", "helper", "plan", "revision"} {
		bad := active
		bad.Key = "bad-" + field
		want := AntigravityConsentDigestConflict
		switch field {
		case "operation":
			bad.OperationID = "other"
			want = AntigravityConsentOperationConflict
		case "helper":
			bad.HelperSHA256 = strings.Repeat("c", 64)
		case "plan":
			bad.PlanSHA256 = strings.Repeat("c", 64)
		case "revision":
			bad.ExpectedRevision = 0
			want = AntigravityConsentVersionConflict
		}
		first := consentApply(t, s, bad, want)
		if got := consentApply(t, s, bad, want); got != first {
			t.Fatal("refusal replay drift")
		}
	}
	enabled := consentApply(t, s, active, nil)
	if enabled.Phase != AntigravityConsentActive || enabled.Revision != 2 {
		t.Fatal(enabled)
	}
	disabled := active
	disabled.Action = AntigravityConsentDisable
	disabled.ExpectedRevision = 2
	disabled.Key = "disable-1"
	disabled.OperationID = "revoke-1"
	disabled.HelperSHA256 = ""
	disabled.PlanSHA256 = ""
	revoked := consentApply(t, s, disabled, nil)
	if revoked.Phase != AntigravityConsentDisabled || revoked.Revision != 3 {
		t.Fatal(revoked)
	}
	// Filesystem removal is deliberately absent (or may fail); authorization
	// is already disabled and neither historical acknowledgment can restore it.
	if consentApply(t, s, active, nil) != enabled {
		t.Fatal("active receipt was not exact")
	}
	if consentApply(t, s, c, nil) != pending {
		t.Fatal("prepare receipt was not exact")
	}
	if got := consentRead(t, s, c.ProjectRoot); got != revoked {
		t.Fatal("old replay reauthorized", got)
	}
	stale := active
	stale.Key = "delayed-complete"
	consentApply(t, s, stale, AntigravityConsentVersionConflict)
	changedAction := disabled
	changedAction.Key = active.Key
	consentApply(t, s, changedAction, AntigravityConsentKeyConflict)
	next := c
	next.ExpectedRevision = 3
	next.Key = "prepare-2"
	next.OperationID = "enable-2"
	p2 := consentApply(t, s, next, nil)
	stale.ExpectedRevision = p2.Revision
	stale.Key = "old-operation-current-revision"
	consentApply(t, s, stale, AntigravityConsentOperationConflict)
}

func TestAntigravityConsentRestartAndForeignState(t *testing.T) {
	s, dir := consentStore(t)
	c := consentCommand(t)
	pending := consentApply(t, s, c, nil)
	if _, err := s.SeedBoardSettings(context.Background(), BoardSettings{Revision: 17, Enabled: true}, "synthetic"); err != nil {
		t.Fatal(err)
	}
	foreign := ReceiptKey{Scope: "foreign", Actor: "actor", Key: "key"}
	if _, err := s.ClaimReceipt(context.Background(), foreign, "digest", ReceiptPolicy{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteReceipt(context.Background(), foreign, ReceiptAnswer{Status: 201, Body: []byte("foreign-answer")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := consentRead(t, reopened, c.ProjectRoot); got != pending {
		t.Fatal(got)
	}
	if got := consentApply(t, reopened, c, nil); got != pending {
		t.Fatal(got)
	}
	active := c
	active.Action = AntigravityConsentActivate
	active.ExpectedRevision = 1
	active.Key = "complete"
	consentApply(t, reopened, active, nil)
	b, err := reopened.BoardSettings(context.Background())
	if err != nil || b.Revision != 17 || !b.Enabled {
		t.Fatal(b, err)
	}
	receipt, err := reopened.LookupReceipt(context.Background(), foreign)
	if err != nil || string(receipt.Answer.Body) != "foreign-answer" {
		t.Fatal(receipt, err)
	}
}

func TestAntigravityConsentFailureRollsBackStateAndReceipt(t *testing.T) {
	for _, fault := range []string{"row", "receipt"} {
		t.Run(fault, func(t *testing.T) {
			s, _ := consentStore(t)
			c := consentCommand(t)
			target, operation := "antigravity_project_consent", "INSERT"
			if fault == "receipt" {
				target, operation = "request_receipts", "UPDATE"
			}
			_, err := s.db.Exec(`CREATE TRIGGER consent_fault BEFORE ` + operation + ` ON ` + target + ` BEGIN SELECT RAISE(ABORT, 'synthetic private path'); END`)
			if err != nil {
				t.Fatal(err)
			}
			consentApply(t, s, c, AntigravityConsentWriteFailed)
			if got := consentRead(t, s, c.ProjectRoot); got.Phase != AntigravityConsentAbsent {
				t.Fatal("partially committed", got)
			}
			receipt, err := s.LookupReceipt(context.Background(), ReceiptKey{Scope: "antigravity.project.consent.v1", Actor: c.Actor, Key: c.Key})
			if err != nil || receipt.Outcome != ReceiptMissing {
				t.Fatal(receipt, err)
			}
			if _, err := s.db.Exec(`DROP TRIGGER consent_fault`); err != nil {
				t.Fatal(err)
			}
			consentApply(t, s, c, nil)
		})
	}
}

func TestAntigravityConsentCrossHandleRevocationRace(t *testing.T) {
	s, dir := consentStore(t)
	other, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	c := consentCommand(t)
	consentApply(t, s, c, nil)
	active := c
	active.Action = AntigravityConsentActivate
	active.ExpectedRevision = 1
	active.Key = "race-active"
	disable := c
	disable.Action = AntigravityConsentDisable
	disable.ExpectedRevision = 1
	disable.Key = "race-disable"
	disable.HelperSHA256 = ""
	disable.PlanSHA256 = ""
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, cmd := range []AntigravityConsentCommand{active, disable} {
		wg.Add(1)
		go func(i int, cmd AntigravityConsentCommand) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			handle := s
			if i == 1 {
				handle = other
			}
			_, err := handle.ApplyAntigravityProjectConsent(ctx, cmd)
			results <- err
		}(i, cmd)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch err {
		case nil:
			success++
		case AntigravityConsentVersionConflict:
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	cur := consentRead(t, s, c.ProjectRoot)
	if cur.Phase == AntigravityConsentActive {
		disable.ExpectedRevision = cur.Revision
		disable.Key = "final-disable"
		cur = consentApply(t, other, disable, nil)
	}
	// Receipt may be success or refusal depending on the race, but no replay
	// can reapply the activation after a successfully committed revocation.
	_, _ = s.ApplyAntigravityProjectConsent(context.Background(), active)
	if got := consentRead(t, other, c.ProjectRoot); got.Phase != AntigravityConsentDisabled || got.Revision != cur.Revision {
		t.Fatal(got)
	}
}

func TestAntigravityConsentCapacityAndValidation(t *testing.T) {
	s, _ := consentStore(t)
	c := consentCommand(t)
	c.Key = "capacity-overflow"
	for i := 0; i < AntigravityProjectConsentLimit; i++ {
		cmd := c
		cmd.ProjectRoot = filepath.Join(c.ProjectRoot, fmt.Sprint(i))
		cmd.Key = fmt.Sprint("prepare-", i)
		consentApply(t, s, cmd, nil)
	}
	if n, err := s.AntigravityProjectConsentCount(context.Background()); err != nil || n != AntigravityProjectConsentLimit {
		t.Fatal(n, err)
	}
	consentApply(t, s, c, AntigravityConsentCapacity)
	disable := c
	disable.ProjectRoot = filepath.Join(c.ProjectRoot, "0")
	disable.Action = AntigravityConsentDisable
	disable.ExpectedRevision = 1
	disable.Key = "disable-at-cap"
	disable.HelperSHA256 = ""
	disable.PlanSHA256 = ""
	consentApply(t, s, disable, nil)
	// A disabled tombstone still occupies its row; no eviction or revision reset.
	newProject := c
	newProject.Key = "still-full"
	consentApply(t, s, newProject, AntigravityConsentCapacity)
	for _, root := range []string{"relative", c.ProjectRoot + "/..", c.ProjectRoot + "/", c.ProjectRoot + "\n", string(filepath.Separator) + strings.Repeat("x", AntigravityConsentRootBytesLimit)} {
		bad := c
		bad.ProjectRoot = root
		consentApply(t, s, bad, AntigravityConsentInvalid)
	}
	for _, bad := range []AntigravityConsentCommand{
		{ProjectRoot: c.ProjectRoot},
		func() AntigravityConsentCommand { b := c; b.HelperSHA256 = strings.Repeat("A", 64); return b }(),
		func() AntigravityConsentCommand {
			b := c
			b.Actor = strings.Repeat("x", AntigravityConsentIdentityBytesLimit+1)
			return b
		}(),
	} {
		consentApply(t, s, bad, AntigravityConsentInvalid)
	}
}

func TestAntigravityConsentUnreadableAndBusy(t *testing.T) {
	s, dir := consentStore(t)
	c := consentCommand(t)
	other, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	consentApply(t, s, c, AntigravityConsentBusy)
	tx.Rollback()
	consentApply(t, s, c, nil)
	if _, err := s.db.Exec(`DROP TABLE antigravity_project_consent`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AntigravityProjectConsent(context.Background(), c.ProjectRoot); err != AntigravityConsentUnreadable {
		t.Fatal(err)
	}
	c.Key = "read-fails"
	consentApply(t, s, c, AntigravityConsentUnreadable)
	if _, err := s.AntigravityProjectConsentCount(context.Background()); err != AntigravityConsentUnreadable {
		t.Fatal(err)
	}
}

func TestAntigravityConsentDeletionCannotResetRevision(t *testing.T) {
	s, _ := consentStore(t)
	c := consentCommand(t)
	c.Action = AntigravityConsentDisable
	c.HelperSHA256, c.PlanSHA256 = "", ""
	revoked := consentApply(t, s, c, nil)
	if _, err := s.db.Exec(`DELETE FROM antigravity_project_consent WHERE project_root = ?`, c.ProjectRoot); err == nil {
		t.Fatal("deleting a revocation allowed the revision to reset")
	}
	if got := consentRead(t, s, c.ProjectRoot); got != revoked {
		t.Fatal(got)
	}
}

func TestAntigravityConsentReplacedPreparationAcrossRestart(t *testing.T) {
	s, dir := consentStore(t)
	c := consentCommand(t)
	consentApply(t, s, c, nil)
	next := c
	next.ExpectedRevision, next.Key, next.OperationID = 1, "replace", "new-operation"
	consentApply(t, s, next, nil)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	oldComplete := c
	oldComplete.Action, oldComplete.ExpectedRevision, oldComplete.Key = AntigravityConsentActivate, 1, "late-complete"
	consentApply(t, s2, oldComplete, AntigravityConsentVersionConflict)
	disable := c
	disable.Action, disable.ExpectedRevision, disable.Key = AntigravityConsentDisable, 2, "disable-after-restart"
	disable.HelperSHA256, disable.PlanSHA256 = "", ""
	revoked := consentApply(t, s2, disable, nil)
	consentApply(t, s2, c, nil)
	consentApply(t, s2, next, nil)
	if got := consentRead(t, s2, c.ProjectRoot); got != revoked {
		t.Fatal("replayed preparation revived consent", got)
	}
}
