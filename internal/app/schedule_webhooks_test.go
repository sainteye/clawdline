package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/schedulewebhook"
)

type webhookCloudFake struct {
	claim    *schedulewebhook.Claim
	receipts []schedulewebhook.Receipt
	// refuse, when set, is Cloud's answer to a receipt it will not take.
	refuse func(schedulewebhook.Receipt) error
}

type investigationCloudFake struct {
	*webhookCloudFake
	bundle    []byte
	readErr   error
	reads     int
	afterRead func()
}

func (f *investigationCloudFake) InvestigationBundle(context.Context, schedulewebhook.Claim) ([]byte, error) {
	f.reads++
	if f.afterRead != nil {
		f.afterRead()
	}
	return f.bundle, f.readErr
}

func investigationRuntimeFixture(t *testing.T, bound bool) (*w4Fixture, *investigationCloudFake, *ScheduleWebhookRuntime, schedulewebhook.Claim) {
	t.Helper()
	f, cloud, _, runtime, claim := webhookFixture(t, map[string]any{"trigger_only": true}, true)
	raw, err := os.ReadFile("../domain/schedulewebhook/testdata/investigation-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var b schedulewebhook.InvestigationBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	runtime.Identity.MachineID = b.Scope.MachineID
	claim = webhookClaim(runtime.Identity, f.at(), claim.HookID)
	claim.Investigation = &b.InvestigationRef
	if bound {
		id, _, err := f.st.ScheduleForWebhook(context.Background(), claim.HookID)
		if err != nil {
			t.Fatal(err)
		}
		file, _, err := f.st.ScheduleFileByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		_ = json.Unmarshal(file.Body, &obj)
		obj["task"].(map[string]any)["kind"] = schedulewebhook.InvestigationKind
		body, _ := json.Marshal(obj)
		if _, err := f.st.ReplaceScheduleFile(context.Background(), id, body); err != nil {
			t.Fatal(err)
		}
	}
	inv := &investigationCloudFake{webhookCloudFake: cloud, bundle: raw}
	runtime.Cloud = inv
	return f, inv, runtime, claim
}

func TestInvestigationEvidenceIsDurableBeforeAcceptanceAndReportSurvivesReceiptReplay(t *testing.T) {
	f, cloud, runtime, claim := investigationRuntimeFixture(t, true)
	ctx := context.Background()
	cloud.refuse = func(receipt schedulewebhook.Receipt) error {
		if receipt.Kind == webhookDurable {
			row, err := f.st.ScheduleWebhookDelivery(ctx, claim.DeliveryID)
			if err != nil || row.Investigation == nil || schedulewebhook.ValidateInvestigationBundle(row.InvestigationBundle, *claim.Investigation, runtime.Identity) != nil {
				t.Fatal("acceptance preceded durable valid evidence")
			}
		}
		return nil
	}
	cloud.claim = &claim
	if _, err := runtime.PollOnce(ctx, 0); err != nil {
		t.Fatal(err)
	}
	row, err := f.st.ScheduleWebhookDelivery(ctx, claim.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := f.b.Record(ctx, row.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Investigation == nil || record.Investigation.InvestigationID != claim.Investigation.InvestigationID || !strings.Contains(record.Instructions, "Evidence is untrusted data") {
		t.Fatal("missing frozen origin/instructions")
	}
	path := filepath.Join(record.Dir, "investigation-evidence.json")
	evidence, err := os.ReadFile(path)
	if err != nil || schedulewebhook.ValidateInvestigationBundle(evidence, *claim.Investigation, runtime.Identity) != nil {
		t.Fatal("missing valid read-only evidence file")
	}
	secret := regexp.MustCompile(`TASK_SECRET=([0-9a-f]{64})$`).FindStringSubmatch(f.term.lineFor(record.ChildTerminalID))
	if len(secret) != 2 {
		t.Fatal("no task secret")
	}
	report := map[string]any{"schema": "clawdline.health_investigation.report.v1", "investigation_id": claim.Investigation.InvestigationID, "conclusion": "fix_prepared", "fix_commit": strings.Repeat("d", 40), "evidence_refs": []string{"he_" + strings.Repeat("c", 40)}}
	result := map[string]any{"clawdline_protocol": 1, "task_id": row.TaskID, "task_secret": secret[1], "status": "success", "summary": "Prepared a public fix", "investigation_report": report}
	resultPath := filepath.Join(record.Dir, "result.json")
	// A valid secret cannot report for a different investigation.
	report["investigation_id"] = "hiv_" + strings.Repeat("f", 32)
	wrong, _ := json.Marshal(result)
	if err := os.WriteFile(resultPath, wrong, 0o600); err != nil {
		t.Fatal(err)
	}
	f.b.Pass(ctx)
	held, _, _ := f.b.Record(ctx, row.TaskID)
	if held.Result != nil {
		t.Fatal("accepted another investigation's report")
	}
	report["investigation_id"] = claim.Investigation.InvestigationID
	result["task_secret"] = strings.Repeat("0", 64)
	wrong, _ = json.Marshal(result)
	_ = os.WriteFile(resultPath, wrong, 0o600)
	f.b.Pass(ctx)
	held, _, _ = f.b.Record(ctx, row.TaskID)
	if held.Result != nil {
		t.Fatal("accepted wrong secret")
	}
	result["task_secret"] = secret[1]
	correct, _ := json.Marshal(result)
	_ = os.WriteFile(resultPath, correct, 0o600)
	f.b.Pass(ctx)
	held, _, err = f.b.Record(ctx, row.TaskID)
	if err != nil || held.Result == nil || len(held.Result.InvestigationReport) == 0 {
		t.Fatal("authenticated report was not collected")
	}
	var sent []byte
	cloud.refuse = func(receipt schedulewebhook.Receipt) error {
		if receipt.Kind == webhookTerminal {
			sent, _ = json.Marshal(receipt)
			t.Logf("terminal receipt wire fixture: %s", sent)
			return errors.New("injected receipt transport failure")
		}
		return nil
	}
	if _, err := runtime.PollOnce(ctx, 0); err == nil {
		t.Fatal("missing injected send failure")
	}
	pending, err := f.st.ScheduleWebhookDelivery(ctx, claim.DeliveryID)
	if err != nil || pending.InvestigationReport == nil || !bytes.Equal(pending.PendingReceipt, sent) {
		t.Fatal("report/receipt was not durable before send")
	}
	// Reopen the real SQLite store: retry must use the identical receipt,
	// even if the untrusted result file changes after collection.
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime.Store = st
	runtime.Book.Store = st
	_ = os.WriteFile(resultPath, []byte(`{}`), 0o600)
	cloud.refuse = func(receipt schedulewebhook.Receipt) error {
		if receipt.Kind == webhookTerminal {
			raw, _ := json.Marshal(receipt)
			if !bytes.Equal(raw, sent) {
				t.Fatal("receipt changed across restart")
			}
		}
		return nil
	}
	if _, err := runtime.PollOnce(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if f.term.opened() != 1 {
		t.Fatal("recovery opened a second task")
	}
}

func TestInvestigationFailureNeverDispatchesAnOrdinarySchedule(t *testing.T) {
	for _, mode := range []string{"unbound", "download", "corrupt", "journal"} {
		t.Run(mode, func(t *testing.T) {
			f, cloud, runtime, claim := investigationRuntimeFixture(t, mode != "unbound")
			switch mode {
			case "download":
				cloud.readErr = errors.New("injected timeout")
			case "corrupt":
				cloud.bundle = []byte(`{}`)
			case "journal":
				cloud.afterRead = func() { _ = f.st.Close() }
			}
			cloud.claim = &claim
			_, err := runtime.PollOnce(context.Background(), 0)
			if mode == "unbound" {
				if err != nil {
					t.Fatal(err)
				}
				if len(cloud.receipts) != 2 || cloud.receipts[1].Kind != webhookRefused {
					t.Fatalf("no typed refusal: %+v", cloud.receipts)
				}
			} else if err == nil {
				t.Fatal("missing injected failure")
			}
			if mode == "corrupt" && !strings.Contains(fmt.Sprint(err), "investigation_invalid: envelope.schema") {
				t.Fatalf("refusal does not name its stage and field: %v", err)
			}
			if f.term.opened() != 0 {
				t.Fatal("failed investigation opened a task")
			}
			if mode == "download" || mode == "corrupt" {
				if len(cloud.receipts) != 0 {
					t.Fatal("accepted evidence that was not saved")
				}
			}
		})
	}
}

func TestInvestigationTaskResultRoundTripsItsTypedExtension(t *testing.T) {
	raw := json.RawMessage(`{"schema":"clawdline.health_investigation.report.v1","investigation_id":"hiv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","conclusion":"needs_evidence","fix_commit":null,"evidence_refs":[]}`)
	result := taskdir.Result{InvestigationReport: raw}
	encoded, _ := json.Marshal(result)
	var got taskdir.Result
	if json.Unmarshal(encoded, &got) != nil || !bytes.Equal(got.InvestigationReport, raw) {
		t.Fatal("report was dropped from the durable task record")
	}
}

// A dispatch that may have been admitted must be retried under its original
// id. Only the broker's positive absence answer permits forgetting a run.
type ambiguousInvestigationBroker struct {
	inner ScheduleBroker
	ids   []string
	lost  bool
}

func (b *ambiguousInvestigationBroker) DispatchScheduled(ctx context.Context, run orchestrator.ScheduledRun) (orchestrator.ScheduledDispatch, error) {
	b.ids = append(b.ids, run.TaskID)
	out, err := b.inner.DispatchScheduled(ctx, run)
	if err == nil && !b.lost {
		b.lost = true
		return orchestrator.ScheduledDispatch{}, errors.New("injected lost dispatch response")
	}
	return out, err
}

func TestInvestigationAmbiguousAdmissionResumesTheSameTask(t *testing.T) {
	f, cloud, runtime, claim := investigationRuntimeFixture(t, true)
	broker := &ambiguousInvestigationBroker{inner: f.book.Broker}
	f.book.Broker = broker
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err == nil {
		t.Fatal("missing ambiguous outcome")
	}
	if len(cloud.receipts) != 1 || cloud.receipts[0].Kind != webhookDurable {
		t.Fatal("an ambiguous dispatch sent a false refusal")
	}

	row, err := f.st.ScheduleWebhookDelivery(context.Background(), claim.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	file, _, err := f.st.ScheduleFileByID(context.Background(), row.ScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	_ = json.Unmarshal(file.Body, &changed)
	changed["enabled"] = false
	changed["when"] = map[string]any{"at": "10:00", "days": "daily"}
	changed["task"].(map[string]any)["kind"] = "custom"
	body, _ := json.Marshal(changed)
	if _, err := f.st.ReplaceScheduleFile(context.Background(), row.ScheduleID, body); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(broker.ids) != 2 || broker.ids[0] != broker.ids[1] || f.term.opened() != 1 {
		t.Fatalf("duplicate task: %v, tabs %d", broker.ids, f.term.opened())
	}
}

func TestInvestigationLeaseExpiryAndEvidenceDiskFailureNeverLaunch(t *testing.T) {
	for _, mode := range []string{"lease", "delivery_expiry", "disk", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			f, cloud, runtime, claim := investigationRuntimeFixture(t, true)
			if mode == "lease" {
				cloud.afterRead = func() { f.set(f.at().Add(2 * time.Minute)) }
			}
			if mode == "delivery_expiry" {
				claim.ExpiresAt = f.at().Add(5 * time.Second).UTC().Format(time.RFC3339Nano)
				claim.Lease.ExpiresAt = f.at().Add(time.Minute).UTC().Format(time.RFC3339Nano)
				canonical, _ := json.Marshal(map[string]any{"schema": schedulewebhook.ProtocolSchema, "delivery_id": claim.DeliveryID, "hook_id": claim.HookID, "hook_generation": claim.HookGeneration, "account_id": runtime.Identity.AccountID, "machine_id": runtime.Identity.MachineID, "accepted_at": claim.AcceptedAt, "expires_at": claim.ExpiresAt})
				sum := sha256.Sum256(canonical)
				claim.DeliveryDigest = hex.EncodeToString(sum[:])
				cloud.afterRead = func() { f.set(f.at().Add(6 * time.Second)) }
			}
			if mode == "ordinary" {
				claim.Investigation = nil
			}
			if mode == "disk" {
				cloud.refuse = func(receipt schedulewebhook.Receipt) error {
					if receipt.Kind == webhookDurable {
						row, err := f.st.ScheduleWebhookDelivery(context.Background(), claim.DeliveryID)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.MkdirAll(filepath.Dir(f.b.Tasks.Path(row.TaskID)), 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(f.b.Tasks.Path(row.TaskID), []byte("injected filesystem obstruction"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return nil
				}
			}
			cloud.claim = &claim
			_, err := runtime.PollOnce(context.Background(), 0)
			if mode == "lease" || mode == "delivery_expiry" {
				if err == nil || len(cloud.receipts) != 0 {
					t.Fatal("accepted an expired evidence lease")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if f.term.opened() != 0 {
				t.Fatal("unsafe investigation launched")
			}
		})
	}
}

func (f *webhookCloudFake) Activate(_ context.Context, _, _ string, at int64) (int64, error) {
	return at + 1, nil
}
func (f *webhookCloudFake) Claim(context.Context, int) (schedulewebhook.ClaimResult, error) {
	claim := f.claim
	f.claim = nil
	return schedulewebhook.ClaimResult{Delivery: claim, ServerTime: "2026-09-18T09:00:30Z"}, nil
}
func (f *webhookCloudFake) Receipt(_ context.Context, delivery string, receipt schedulewebhook.Receipt) (schedulewebhook.ReceiptAck, error) {
	f.receipts = append(f.receipts, receipt)
	if f.refuse != nil {
		if err := f.refuse(receipt); err != nil {
			return schedulewebhook.ReceiptAck{}, err
		}
	}
	return schedulewebhook.ReceiptAck{DeliveryID: delivery, ReceiptVersion: receipt.ReceiptVersion,
		State: "accepted", AcknowledgedAt: "2026-09-18T09:00:30Z"}, nil
}

func webhookClaim(identity schedulewebhook.Identity, now time.Time, hook string) schedulewebhook.Claim {
	c := schedulewebhook.Claim{DeliveryID: "swd_" + strings.Repeat("a", 26), HookID: hook,
		HookGeneration: 1, AcceptedAt: now.UTC().Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Hour).UTC().Format(time.RFC3339Nano), Attempt: 1,
		Lease: schedulewebhook.Lease{Token: "swhl_" + strings.Repeat("a", 43), Revision: 1,
			ExpiresAt: now.Add(time.Minute).UTC().Format(time.RFC3339Nano)}}
	canonical, _ := json.Marshal(map[string]any{
		"schema": schedulewebhook.ProtocolSchema, "delivery_id": c.DeliveryID, "hook_id": c.HookID,
		"hook_generation": c.HookGeneration, "account_id": identity.AccountID,
		"machine_id": identity.MachineID, "accepted_at": c.AcceptedAt, "expires_at": c.ExpiresAt,
	})
	sum := sha256.Sum256(canonical)
	c.DeliveryDigest = hex.EncodeToString(sum[:])
	return c
}

// Before the webhook runtime existed the bound hook was never claimed. This
// exercises the real broker path and proves admission is journaled and
// receipted in order, with receipt v1 not leaking the preallocated task id.
func TestWebhookDeliveryDispatchesExactlyOnceAndReceiptsAdmission(t *testing.T) {
	f := newW4(t)
	id := f.schedule(t, "5c000020-0000-4000-8000-000000000020", "webhook repair", map[string]any{"claims": []string{}})
	hook := "swh_" + strings.Repeat("a", 26)
	if err := f.st.BindScheduleWebhook(context.Background(), hook, id, "", f.at()); err != nil {
		t.Fatal(err)
	}
	identity := schedulewebhook.Identity{AccountID: "acct_test", MachineID: "mac_test"}
	cloud := &webhookCloudFake{}
	claim := webhookClaim(identity, f.at(), hook)
	cloud.claim = &claim
	runtime := &ScheduleWebhookRuntime{Cloud: cloud, Identity: identity, Store: f.st, Book: f.book,
		Build: "test+1", Now: f.at}
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := f.term.opened(); got != 1 {
		t.Fatalf("opened %d tabs, want one", got)
	}
	if len(cloud.receipts) != 2 || cloud.receipts[0].Kind != webhookDurable ||
		cloud.receipts[0].TaskID != nil || cloud.receipts[1].Kind != webhookAccepted ||
		cloud.receipts[1].TaskID == nil {
		t.Fatalf("receipts: %+v", cloud.receipts)
	}
	row, err := f.st.ScheduleWebhookDelivery(context.Background(), claim.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != webhookAccepted || row.CloudAcknowledgedThrough != 2 || row.TaskID == "" {
		t.Fatalf("journal after dispatch: %+v", row)
	}
	// Replaying the same durable delivery cannot open a second tab.
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := f.term.opened(); got != 1 {
		t.Fatalf("replay opened %d tabs, want one", got)
	}
}

// countingBroker is the broker with a count in front of it, so a test can say
// DispatchScheduled was never reached rather than that no tab happened to open.
type countingBroker struct {
	inner ScheduleBroker
	calls int
}

func (c *countingBroker) DispatchScheduled(ctx context.Context, run orchestrator.ScheduledRun) (orchestrator.ScheduledDispatch, error) {
	c.calls++
	return c.inner.DispatchScheduled(ctx, run)
}

// webhookFixture is one bound schedule and a runtime around a fake Cloud.
func webhookFixture(t *testing.T, when map[string]any, enabled bool) (*w4Fixture, *webhookCloudFake, *countingBroker, *ScheduleWebhookRuntime, schedulewebhook.Claim) {
	t.Helper()
	f := newW4(t)
	id := "5c000021-0000-4000-8000-000000000021"
	body, _ := json.Marshal(map[string]any{
		"clawdline_schedule": 1, "schedule_id": id, "title": "deploy",
		"when": when, "enabled": enabled, "created_at": f.at().Add(-72 * time.Hour).Unix(),
		"task": map[string]any{"assistant": "claude", "project_dir": f.dir, "title": "deploy",
			"instructions": "deploy it", "claims": []string{}},
	})
	if err := f.st.CreateScheduleFile(context.Background(), id, body, f.at().Add(-72*time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	hook := "swh_" + strings.Repeat("b", 26)
	if err := f.st.BindScheduleWebhook(context.Background(), hook, id, "", f.at()); err != nil {
		t.Fatal(err)
	}
	counting := &countingBroker{inner: f.b}
	f.book.Broker = counting
	identity := schedulewebhook.Identity{AccountID: "acct_test", MachineID: "mac_test"}
	cloud := &webhookCloudFake{}
	claim := webhookClaim(identity, f.at(), hook)
	runtime := &ScheduleWebhookRuntime{Cloud: cloud, Identity: identity, Store: f.st, Book: f.book,
		Build: "test+1", Now: f.at, Log: func(string, ...any) {}}
	return f, cloud, counting, runtime, claim
}

// C2, failure injected: Cloud refuses the version-1 receipt because the
// delivery's deadline passed. The task must not start — not on this poll, not
// on a later one, and not if Cloud offers the same delivery again — and the
// refusal is not retried.
func TestAnExpiredDeliveryNeverReachesTheBroker(t *testing.T) {
	f, cloud, counting, runtime, claim := webhookFixture(t, map[string]any{"at": "09:00", "days": "daily"}, true)
	cloud.refuse = func(r schedulewebhook.Receipt) error {
		return &adaptercloud.APIError{Status: 409, Code: "delivery_expired", Message: "the delivery's deadline has passed"}
	}
	var logged []string
	runtime.Log = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatalf("an expired delivery is an outcome, not an error: %v", err)
	}
	row, err := f.st.ScheduleWebhookDelivery(context.Background(), claim.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != webhookExpired || row.TerminalAt == 0 || len(row.PendingReceipt) != 0 {
		t.Fatalf("journal after delivery_expired: %+v", row)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "expired") {
		t.Fatalf("log lines: %q", logged)
	}
	// Later polls, and the same delivery offered again, change nothing.
	for i := 0; i < 2; i++ {
		cloud.claim = &claim
		if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
	}
	if counting.calls != 0 || f.term.opened() != 0 {
		t.Fatalf("an expired delivery reached the broker %d times and opened %d tabs", counting.calls, f.term.opened())
	}
	if len(cloud.receipts) != 1 || cloud.receipts[0].ReceiptVersion != 1 {
		t.Fatalf("receipts sent: %+v, want only the refused version 1", cloud.receipts)
	}
	if runs := f.runs(t, row.ScheduleID); len(runs) != 0 {
		t.Fatalf("an expired delivery made runs: %+v", runs)
	}
}

// Drive-it-red control for the test above: the same fixture, with Cloud
// accepting the receipt, does reach the broker once.
func TestAnAcceptedDeliveryReachesTheBrokerOnce(t *testing.T) {
	_, cloud, counting, runtime, claim := webhookFixture(t, map[string]any{"at": "09:00", "days": "daily"}, true)
	cloud.claim = &claim
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if counting.calls != 1 {
		t.Fatalf("broker reached %d times, want once", counting.calls)
	}
}

// C5: a trigger-only schedule runs by webhook while enabled, and a disabled
// one is refused with the stable code a refusal receipt carries.
func TestATriggerOnlyScheduleRunsByWebhookOnlyWhileEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		_, cloud, counting, runtime, claim := webhookFixture(t, map[string]any{"trigger_only": true}, enabled)
		cloud.claim = &claim
		if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if len(cloud.receipts) != 2 || cloud.receipts[0].Kind != webhookDurable {
			t.Fatalf("enabled=%v receipts: %+v", enabled, cloud.receipts)
		}
		last := cloud.receipts[1]
		if enabled {
			if counting.calls != 1 || last.Kind != webhookAccepted {
				t.Fatalf("an enabled trigger-only schedule: %d dispatches, receipt %+v", counting.calls, last)
			}
			continue
		}
		if counting.calls != 0 || last.Kind != webhookRefused || last.OutcomeCode == nil ||
			*last.OutcomeCode != "schedule_disabled" {
			t.Fatalf("a disabled trigger-only schedule: %d dispatches, receipt %+v", counting.calls, last)
		}
	}
}

// Every state the broker ends a task in is reported as a terminal outcome,
// and no other state is: a new terminal state with no mapping would leave the
// caller of `webhook fire` waiting forever.
func TestEveryTerminalTaskStateHasAWebhookOutcome(t *testing.T) {
	want := map[orchestrator.State]string{
		orchestrator.StateSuccess: "success", orchestrator.StateFailure: "failure",
		orchestrator.StateTimeout: "timed_out", orchestrator.StateCancelled: "cancelled",
		orchestrator.StateSpawnFailed: "spawn_failed",
	}
	for _, s := range []orchestrator.State{orchestrator.StateQueued, orchestrator.StateSpawning,
		orchestrator.StateBriefed, orchestrator.StateSuccess, orchestrator.StateFailure,
		orchestrator.StateTimeout, orchestrator.StateCancelled, orchestrator.StateSpawnFailed, ""} {
		got := webhookTerminalOutcome(string(s))
		if got != want[s] || (got != "") != s.Terminal() {
			t.Fatalf("state %q reported as %q, want %q", s, got, want[s])
		}
	}
}

// Cloud's real shape: a svc_api incident's evidence, leased to this Mac. The
// executor stays the Mac; only the evidence source differs.
func TestInvestigationOfACloudServiceIncidentRunsOnThisMachine(t *testing.T) {
	f, cloud, runtime, claim := investigationRuntimeFixture(t, true)
	raw := bytes.ReplaceAll(cloud.bundle, []byte(`"machine_id": "`+runtime.Identity.MachineID+`"`), []byte(`"machine_id": "svc_api"`))
	var b schedulewebhook.InvestigationBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	canonical, err := schedulewebhook.CanonicalInvestigationJSON(b.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	ref := *claim.Investigation
	fresh := "sha256:" + hex.EncodeToString(sum[:])
	cloud.bundle = bytes.Replace(raw, []byte(ref.ContentHash), []byte(fresh), 1)
	ref.ContentHash = fresh
	claim.Investigation = &ref
	cloud.claim = &claim
	if runtime.Identity.MachineID == "svc_api" {
		t.Fatal("the executor must not be the evidence source")
	}
	if _, err := runtime.PollOnce(context.Background(), 0); err != nil {
		t.Fatalf("service-sourced investigation refused: %v", err)
	}
	if f.term.opened() != 1 {
		t.Fatalf("opened %d tasks, want 1", f.term.opened())
	}
}
