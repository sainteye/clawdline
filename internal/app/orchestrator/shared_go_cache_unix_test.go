//go:build darwin || linux

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
	"github.com/sainteye/clawdline/internal/productcopy"
)

func TestEditingChildrenShareGoCachesWithoutWritingTaskWork(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	gitIn(t, b.Dir, "init", "-q")
	commitFile(t, b.Dir, "go.mod", "module fixture.invalid/project\n\ngo 1.25\n")
	launcher := &recordingLauncher{pane: "%90"}
	b.Launcher = launcher
	b.Type = (&typedKeys{}).Type
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%90", Backend: session.BackendTmux, Assistant: session.AssistantCodex},
		}
	}
	b.Screen = func(_ context.Context, pane string) (string, bool) {
		return screen(t, "codex-composer"), pane == "%90"
	}
	bin := t.TempDir()
	assistant := `#!/bin/sh
set -eu
mkdir -p "${GOCACHE:-$TASK_WORK/go-cache}" "${GOMODCACHE:-$TASK_WORK/go-mod}"
printf '%s\n%s\n' "${GOCACHE:-}" "${GOMODCACHE:-}"
if [ -f "${GOCACHE:-$TASK_WORK/go-cache}/fixture" ]; then
  cat "$GOCACHE/fixture" "$GOMODCACHE/fixture"
else
  printf build > "${GOCACHE:-$TASK_WORK/go-cache}/fixture"
  printf module > "${GOMODCACHE:-$TASK_WORK/go-mod}/fixture"
fi
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(assistant), 0700); err != nil {
		t.Fatal(err)
	}
	var first []string
	for i := 0; i < 2; i++ {
		inv, err := b.ReadInventory(ctx, b.Dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.NewString()
		writeBrief(t, b, id, b.Dir, map[string]any{
			"isolation": IsolationWorktree, "kind": "implementation", "assistant": "codex", "permission_mode": "full",
		})
		out, err := b.Dispatch(ctx, DispatchRequest{TaskID: id, Secret: w1Secret, Offered: true, Generation: inv.Generation})
		if err != nil {
			t.Fatal(err)
		}
		r, line := out.Record, launcher.line()
		if r.Worktree == nil {
			t.Fatal("dispatch did not create a worktree", r.Verdict)
		}
		work := filepath.Join(r.Dir, "work")
		cmd := exec.Command("sh", "-c", line)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "TASK_WORK="+work, "GOCACHE=", "GOMODCACHE=")
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("launch: %v\n%s", err, raw)
		}
		for _, name := range []string{"go-cache", "go-mod"} {
			if _, err := os.Stat(filepath.Join(work, name)); !os.IsNotExist(err) {
				t.Fatalf("default child populated work/%s: %v", name, err)
			}
		}
		fields := strings.Split(string(raw), "\n")
		if len(fields) != 3 || filepath.Base(fields[0]) != "go-build" || filepath.Base(fields[1]) != "go-mod" {
			t.Fatalf("launch did not export managed Go paths: %q", raw)
		}
		if i == 0 {
			first = fields
			continue
		}
		if fields[0] != first[0] || fields[1] != first[1] || fields[2] != "buildmodule" {
			t.Fatalf("second child did not reuse both caches: %q", raw)
		}
	}
}

func TestSharedGoTrimUsesChildLifetimeWithoutAResult(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
		tab   bool
		live  bool
		trim  bool
	}{
		{name: "launch failed", state: StateSpawnFailed, trim: true},
		{name: "cancelled and gone", state: StateCancelled, tab: true, trim: true},
		{name: "cancelled but present", state: StateCancelled, tab: true, live: true},
		{name: "running", state: StateBriefed, tab: true, live: true},
		{name: "waiting at dialog", state: StateSpawning, tab: true, live: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			enableTestResources(t, b)
			b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }
			b.Reading = func(context.Context) session.Inventory {
				rows := []session.Session{{ID: "%other", Backend: session.BackendTmux}}
				if tc.live {
					rows = append(rows, session.Session{ID: "%child", Backend: session.BackendTmux})
				}
				return session.Inventory{Sessions: rows, Sources: map[string]bool{"tmux": true}}
			}
			policy := taskresource.DefaultPolicy()
			policy.CacheBytes = 1
			b.Resources.Policy = func() taskresource.Policy { return policy }

			r := Record{Protocol: Protocol, ID: uuid.NewString(), Title: "Fixture", Assistant: "codex",
				ProjectDir: t.TempDir(), State: StateQueued, CreatedAt: b.now()}
			r.Dir = b.Tasks.Path(r.ID)
			if err := os.MkdirAll(filepath.Join(r.Dir, "artifacts"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := b.save(ctx, r, HashSecret(w1Secret), "task.queued"); err != nil {
				t.Fatal(err)
			}
			r, err := b.attachSharedGoCache(ctx, r, true)
			if err != nil || r.SharedGoCache == nil {
				t.Fatal(r.SharedGoCache, err)
			}
			bucket := filepath.Join(r.SharedGoCache.BuildPath, "ab")
			if err := os.MkdirAll(bucket, 0o700); err != nil {
				t.Fatal(err)
			}
			entry := filepath.Join(bucket, fmt.Sprintf("%064x-d", 1))
			if err := os.WriteFile(entry, []byte("cached"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = b.mutate(ctx, r.ID, "task.fixture", func(r *Record) error {
				r.State = tc.state
				if tc.tab {
					r.ChildTerminalID, r.ChildBackend, r.SpawnedAt = "%child", "tmux", b.now()
				}
				if tc.state == StateSpawning {
					r.AwaitingDialogSince = b.now()
				}
				if tc.state.Terminal() {
					r.FinishedAt = b.now()
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := b.SweepTaskResources(ctx, ""); err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(entry)
			if tc.trim && !os.IsNotExist(err) {
				t.Fatalf("idle cache entry with no result was retained: %v", err)
			}
			if !tc.trim && err != nil {
				t.Fatalf("a consumer still owns the cache: %v", err)
			}
			owner, err := b.resourceOwner(ctx, "task", r.ID)
			if err != nil || owner.OutputsSaved {
				t.Fatal("no-result task must not authorize its own scratch cleanup", owner, err)
			}
		})
	}
}

func TestSharedGoTrimBoundsFinalProcessTableRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stalled bool
		reason  string
	}{
		{name: "responsive", reason: "shared_cache_trimmed"},
		{name: "stalled", stalled: true, reason: taskresource.ProcessTableTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			enableTestResources(t, b)
			scope := ResourceOwnerScope()
			policy := taskresource.DefaultPolicy()
			policy.CacheBytes = 1
			b.Resources.Policy = func() taskresource.Policy { return policy }
			b.Reading = func(context.Context) session.Inventory {
				return session.Inventory{Sessions: []session.Session{{ID: "%other", Backend: session.BackendTmux}}, Sources: map[string]bool{"tmux": true}}
			}
			atFinalCheck := false
			b.Resources.Fault = func(point, _ string) error {
				if point == "after_intent" {
					atFinalCheck = true
				}
				return nil
			}
			stalled := make(chan struct{})
			release := make(chan struct{})
			b.ProcessCWDs = func(context.Context) ([]string, error) {
				if tc.stalled && atFinalCheck {
					close(stalled)
					<-release
				}
				return nil, nil
			}

			r := Record{Protocol: Protocol, ID: uuid.NewString(), Title: "Fixture", Assistant: "codex",
				ProjectDir: t.TempDir(), State: StateQueued, CreatedAt: b.now()}
			r.Dir = b.Tasks.Path(r.ID)
			if err := os.MkdirAll(r.Dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := b.save(ctx, r, HashSecret(w1Secret), "task.queued"); err != nil {
				t.Fatal(err)
			}
			r, err := b.attachSharedGoCache(ctx, r, true)
			if err != nil || r.SharedGoCache == nil {
				t.Fatal(r.SharedGoCache, err)
			}
			cache := r.SharedGoCache
			entry := filepath.Join(cache.BuildPath, "ab", fmt.Sprintf("%064x-d", 1))
			if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entry, []byte("cached"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = b.mutate(ctx, r.ID, "task.fixture", func(r *Record) error {
				r.State, r.FinishedAt = StateCancelled, b.now()
				r.ChildTerminalID, r.ChildBackend, r.SpawnedAt = "%child", "tmux", b.now()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := b.Resources.Plan(ctx, scope, []string{cache.ID})
			if err != nil || len(plan.Items) != 1 || plan.Items[0].Decision.Outcome != "eligible" {
				t.Fatalf("expected an eligible cache: %+v, %v", plan, err)
			}
			applyCtx, cancel := context.WithCancel(ctx)
			var workers sync.WaitGroup
			t.Cleanup(func() {
				cancel()
				close(release)
				workers.Wait()
			})
			type outcome struct {
				result taskresources.Result
				err    error
			}
			applied := make(chan outcome, 1)
			workers.Go(func() {
				result, err := b.Resources.Apply(applyCtx, scope, plan, uuid.NewString())
				applied <- outcome{result, err}
			})
			used := make(chan error, 1)
			watchdog := time.NewTimer(15 * time.Second)
			defer watchdog.Stop()
			if tc.stalled {
				select {
				case <-stalled:
				case early := <-applied:
					t.Fatalf("Apply never reached the final process-table check: %+v", early)
				case <-watchdog.C:
					t.Fatal("Apply did not reach the final process-table check")
				}
				workers.Go(func() {
					used <- b.Resources.UseSharedGo(ctx, scope, cache.Project, func(resource taskresource.Resource, _ string) error {
						if resource.ID != cache.ID {
							return fmt.Errorf("attached cache %s, want %s", resource.ID, cache.ID)
						}
						return nil
					})
				})
			}
			var got outcome
			select {
			case got = <-applied:
			case <-watchdog.C:
				select {
				case err := <-used:
					t.Fatalf("Apply stayed blocked after 15s but UseSharedGo returned: %v", err)
				default:
					t.Fatal("Apply stayed blocked in the process-table read and UseSharedGo could not acquire the lock after 15s")
				}
			}
			if got.err != nil || len(got.result.Items) != 1 || got.result.Items[0].Reason != tc.reason {
				t.Fatalf("unexpected cleanup result: %+v, %v", got.result, got.err)
			}
			if tc.stalled {
				row, err := b.Store.TaskResource(ctx, cache.ID)
				if err != nil || row.LastReason != taskresource.ProcessTableTimeout || row.LastOutcome != "kept" {
					t.Fatalf("standing cleanup decision lost the timeout: %+v, %v", row, err)
				}
				select {
				case err := <-used:
					if err != nil {
						t.Fatal(err)
					}
				case <-watchdog.C:
					t.Fatal("UseSharedGo could not acquire the released lock")
				}
				if raw, err := os.ReadFile(entry); err != nil || string(raw) != "cached" {
					t.Fatalf("uncertain cache was not retained: %q, %v", raw, err)
				}
			} else if _, err := os.Stat(entry); !os.IsNotExist(err) {
				t.Fatalf("confirmed idle cache was not trimmed: %v", err)
			}
			t.Logf("Apply returned %s", got.result.Items[0].Reason)
		})
	}
}

func TestSharedGoTrimRecoversAfterProcessTableSaturation(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	policy := taskresource.DefaultPolicy()
	policy.CacheBytes, policy.CacheTotal = 1, 1
	b.Resources.Policy = func() taskresource.Policy { return policy }
	b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }

	r := Record{Protocol: Protocol, ID: uuid.NewString(), Title: "Fixture", Assistant: "codex",
		ProjectDir: t.TempDir(), State: StateQueued, CreatedAt: b.now()}
	r.Dir = b.Tasks.Path(r.ID)
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.save(ctx, r, HashSecret(w1Secret), "task.queued"); err != nil {
		t.Fatal(err)
	}
	r, err := b.attachSharedGoCache(ctx, r, true)
	if err != nil || r.SharedGoCache == nil {
		t.Fatal(r.SharedGoCache, err)
	}
	entry := filepath.Join(r.SharedGoCache.BuildPath, "ab", fmt.Sprintf("%064x-d", 1))
	if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.mutate(ctx, r.ID, "task.fixture", func(r *Record) error {
		r.State, r.FinishedAt = StateSpawnFailed, b.now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scope := ResourceOwnerScope()
	plan, err := b.Resources.Plan(ctx, scope, []string{r.SharedGoCache.ID})
	if err != nil || len(plan.Items) != 1 || plan.Items[0].Decision.Outcome != "eligible" {
		t.Fatalf("expected an eligible cache before saturation: %+v, %v", plan, err)
	}

	const slots = capacity.ResourceProcessTableReadsLimit
	started, returned := make(chan struct{}, slots), make(chan error, slots)
	waitForActive := func(want int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		active, _ := ProcessTableReadStatus()
		for active != want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
			active, _ = ProcessTableReadStatus()
		}
		if active != want {
			t.Errorf("active process-table reads = %d, want %d", active, want)
		}
	}
	waitForActive(0)
	var release [slots]func()
	t.Cleanup(func() {
		for _, unblock := range release {
			unblock()
		}
		waitForActive(0)
	})
	for i := range release {
		blocked := make(chan struct{})
		release[i] = sync.OnceFunc(func() { close(blocked) })
		reader := &Broker{ProcessCWDs: func(context.Context) ([]string, error) {
			started <- struct{}{}
			<-blocked
			return nil, nil
		}}
		go func() {
			readCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_, err := reader.readProcessCWDs(readCtx)
			returned <- err
		}()
	}
	for range slots {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("fixture did not occupy every process-table slot")
		}
	}
	for range slots {
		if err := <-returned; err != context.DeadlineExceeded {
			t.Fatalf("expected the caller deadline to expire, got %v", err)
		}
	}
	waitForActive(slots)
	if _, stalled := ProcessTableReadStatus(); !stalled {
		t.Fatal("diagnostics did not show the saturated process table")
	}

	start := time.Now()
	page, err := b.Resources.Inventory(ctx, scope, "", taskresource.PageRowsLimit)
	elapsed := time.Since(start)
	if elapsed > time.Second || err != nil || len(page.Items) != 1 || page.Items[0].Decision.Reason != taskresource.ProcessTableStalled {
		t.Fatalf("inventory took %s, expected a prompt process_table_stalled decision: %+v, %v", elapsed, page, err)
	}
	t.Logf("inventory refused to infer idle in %s", elapsed)
	start = time.Now()
	_, err = b.Resources.Apply(ctx, scope, plan, uuid.NewString())
	var failure taskresources.Failure
	if elapsed := time.Since(start); elapsed > time.Second || !errors.As(err, &failure) || failure.Code != taskresource.ProcessTableStalled {
		t.Fatalf("apply took %s, expected process_table_stalled: %v", elapsed, err)
	}
	start = time.Now()
	err = b.AdmitHeavyResources(ctx)
	refusal, ok := err.(Refusal)
	if elapsed := time.Since(start); elapsed > time.Second || !ok || refusal.Code != taskresource.ProcessTableStalled {
		t.Fatalf("admission took %s, expected process_table_stalled: %v", elapsed, err)
	}
	t.Logf("admission refused in %s", time.Since(start))
	events, err := b.Store.LatestEvents(ctx, "resource.admission_refused", []string{"resources.admit"})
	diagnostic, ok := refusal.Extra["resource_diagnostic"].(taskresources.Diagnostic)
	var recorded taskresources.Diagnostic
	if err != nil || !ok || diagnostic.Code != taskresource.ProcessTableStalled || diagnostic.ResourceID != r.SharedGoCache.ID ||
		diagnostic.ContextState != "active" || json.Unmarshal(events["resources.admit"].Payload, &recorded) != nil ||
		recorded != diagnostic || !strings.Contains(refusal.Message, "End the stuck lsof") {
		t.Fatalf("admission lost its recovery detail or durable diagnostic: %+v, %+v, %v", refusal, events, err)
	}
	key := productcopy.HTTPRefusalKey(refusal.Message)
	if message := productcopy.HTTPRefusalText("zh-Hant", key, refusal.Message); !strings.Contains(message, "結束卡住的 lsof 或重啟 daemon 即可恢復") {
		t.Fatalf("admission recovery detail was not localized: %q", message)
	}
	if raw, err := os.ReadFile(entry); err != nil || string(raw) != "cached" {
		t.Fatalf("cache was not preserved while saturated: %q, %v", raw, err)
	}

	// One released slot is enough to resume fresh checks while the other
	// timed-out readers still own their slots.
	release[0]()
	waitForActive(slots - 1)
	if _, stalled := ProcessTableReadStatus(); stalled {
		t.Fatal("diagnostics still showed a stalled process table after a slot was released")
	}
	if _, _, err := b.SweepTaskResources(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("cache did not trim after a reader finished: %v", err)
	}
	t.Log("the next sweep trimmed the cache after one slot was released")
}

func TestSharedGoOwnerReadsConsumerPresenceOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	policy := taskresource.DefaultPolicy()
	policy.CacheBytes = 1
	b.Resources.Policy = func() taskresource.Policy { return policy }

	const consumers = 4
	project := t.TempDir()
	var last Record
	for i := 1; i <= consumers; i++ {
		r := Record{Protocol: Protocol, ID: fmt.Sprintf("d0000000-0000-4000-8000-%012d", i), Title: "Fixture",
			Assistant: "codex", ProjectDir: project, State: StateQueued, CreatedAt: b.now()}
		r.Dir = b.Tasks.Path(r.ID)
		if err := os.MkdirAll(r.Dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := b.save(ctx, r, HashSecret(w1Secret), "task.queued"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.attachSharedGoCache(ctx, r, true); err != nil {
			t.Fatal(err)
		}
		var err error
		last, err = b.mutate(ctx, r.ID, "task.fixture", func(r *Record) error {
			r.State, r.FinishedAt = StateCancelled, b.now()
			r.ChildTerminalID, r.ChildBackend, r.SpawnedAt = fmt.Sprintf("%%%d", i), "tmux", b.now()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.SharedGoCache == nil {
		t.Fatal("no shared cache was attached")
	}
	cache := last.SharedGoCache
	resource, err := b.Store.TaskResource(ctx, cache.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(cache.BuildPath, "ab", fmt.Sprintf("%064x-d", 1))
	if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}
	var inventoryReads, processReads int
	var cwds []string
	b.Reading = func(context.Context) session.Inventory {
		inventoryReads++
		return session.Inventory{Sessions: []session.Session{{ID: "%other", Backend: session.BackendTmux}}, Sources: map[string]bool{"tmux": true}}
	}
	b.ProcessCWDs = func(context.Context) ([]string, error) {
		processReads++
		return cwds, nil
	}
	t.Run("all consumers gone", func(t *testing.T) {
		owner, err := b.resourceOwner(ctx, resource.Owner.Kind, resource.Owner.ID)
		if err != nil || !owner.Complete || owner.Presence != "absent" || owner.UsePresence != "absent" {
			t.Fatalf("cache owner was not confirmed gone: %+v, %v", owner, err)
		}
		t.Logf("%d consumers, %d terminal inventory reads, %d process table reads", consumers, inventoryReads, processReads)
		if inventoryReads != 1 || processReads != 1 {
			t.Error("expected one terminal inventory read and one process table read per owner check")
		}
	})
	t.Run("last consumer still has a working directory", func(t *testing.T) {
		cwds = []string{last.Dir}
		defer func() { cwds = nil }()
		owner, err := b.resourceOwner(ctx, resource.Owner.Kind, resource.Owner.ID)
		if err != nil || owner.Complete || owner.Presence != "present" || owner.UsePresence != "present" {
			t.Fatalf("cache owner was not found in the process table: %+v, %v", owner, err)
		}
		if _, _, err := b.SweepTaskResources(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(entry); err != nil {
			t.Fatalf("cache in use was not retained: %v", err)
		}
	})
	t.Run("running consumer is not absent", func(t *testing.T) {
		if _, err := b.mutate(ctx, last.ID, "task.fixture", func(r *Record) error {
			r.State, r.FinishedAt = StateBriefed, time.Time{}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		owner, err := b.resourceOwner(ctx, resource.Owner.Kind, resource.Owner.ID)
		if err != nil || owner.Complete || owner.Presence == "absent" || owner.UsePresence == "absent" {
			t.Fatalf("running consumer was reported absent: %+v, %v", owner, err)
		}
	})
}

func TestReusedCallbackGroupDoesNotKeepACacheInUseOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process-group identity")
	}
	b, _ := newTestBroker(t)
	cmd := exec.Command("sleep", "20")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid
	started := leaderStart(pid)
	if started == 0 || callbackGroupPresence(pid) != "present" {
		t.Fatal("fixture process group was not observable")
	}
	r := Record{Dir: t.TempDir(), State: StateSuccess,
		Callback: &Callback{PID: pid, PGID: pid, LeaderStart: started - 5}}
	files := callbackFiles{r.Dir}
	lock, err := fence(files.lock())
	if err != nil || lock == nil {
		t.Fatal("fixture callback lock", err)
	}
	if got := b.callbackResourcePresence(r); got != "present" {
		unfence(lock)
		t.Fatal("held lock did not pin the cache", got)
	}
	unfence(lock)
	if got := b.callbackResourcePresence(r); got != "absent" {
		t.Fatal("reused number incorrectly pinned the cache", got)
	}
	r.Callback.LeaderStart = started
	if got := b.callbackResourcePresence(r); got != "present" {
		t.Fatal("active original group was treated as recycled", got)
	}
}

func TestHeavyCallbackCanContinueWithAnActiveOverTargetCache(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	project := t.TempDir()
	gitIn(t, project, "init", "-q")
	commitFile(t, project, "source.go", "package fixture\n")
	entry := filepath.Join("ab", fmt.Sprintf("%064x-d", 1))
	req := callbackRequest(t, uuid.NewString(), "sh", "-c", `mkdir -p "$GOCACHE/ab"; printf compiled > "$GOCACHE/`+filepath.ToSlash(entry)+`"; printf retained > "$CLAWDLINE_ARTIFACT_DIR/result.txt"; while ! test -e release; do sleep 0.05; done`)
	req.Dir, req.Intent = project, CallbackIntentHeavy
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	release := func() { _ = os.WriteFile(filepath.Join(project, "release"), nil, 0o600) }
	t.Cleanup(release)
	var r Record
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		r, _, err = b.Record(ctx, req.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(r.Dir, "artifacts", "result.txt")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callback did not publish its ready artifact", r.Verdict)
		}
		time.Sleep(10 * time.Millisecond)
	}
	p := taskresource.DefaultPolicy()
	p.CacheBytes = 1
	b.Resources.Policy = func() taskresource.Policy { return p }
	if err := b.AdmitHeavyResources(ctx); err != nil {
		t.Fatal("active callback could not acquire its next compile", err)
	}
	cacheEntry := filepath.Join(r.Callback.GoCachePath, entry)
	if _, err := os.Stat(cacheEntry); err != nil {
		t.Fatal("active cache entry was removed", err)
	}
	p.CacheTotal = p.CacheBytes
	if err := b.AdmitHeavyResources(ctx); err == nil {
		t.Fatal("aggregate cache pressure allowed another compile")
	}
	p.CacheTotal = taskresource.DefaultPolicy().CacheTotal
	release()
	if r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second); r.State != StateSuccess {
		t.Fatal(r.Verdict)
	}
	// Affordable admission leaves optional maintenance to the same periodic
	// sweeper, which still seals completed outputs without a person doing it.
	if err := b.AdmitHeavyResources(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cacheEntry); err != nil {
		t.Fatal("affordable admission performed optional eviction", err)
	}
	if _, _, err := b.SweepTaskResources(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cacheEntry); !os.IsNotExist(err) {
		t.Fatal("released cache was not trimmed", err)
	}
	if raw, err := os.ReadFile(filepath.Join(r.Dir, "artifacts", "result.txt")); err != nil || string(raw) != "retained" {
		t.Fatal("saved result changed", err)
	}
}

func TestHeavyCallbacksShareOnlyGoCacheAndReleasePerCommand(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	project := t.TempDir()
	gitIn(t, project, "init", "-q")
	commitFile(t, project, "source.go", "package fixture\n")
	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, project, "worktree", "add", "-q", "-b", "linked", linked)
	start := func(dir, script string) Record {
		req := callbackRequest(t, uuid.NewString(), "sh", "-c", script)
		req.Dir, req.Intent = dir, CallbackIntentHeavy
		if _, err := b.StartCallback(ctx, req); err != nil {
			t.Fatal(err)
		}
		r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
		if r.State != StateSuccess {
			t.Fatal(r.Verdict)
		}
		return r
	}
	first := start(project, `printf reused > "$GOCACHE/proof"; printf result > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	second := start(linked, `test "$(cat "$GOCACHE/proof")" = reused; printf result > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	if first.Callback.GoCacheID == "" || first.Callback.GoCacheID != second.Callback.GoCacheID || first.Callback.CachePath == second.Callback.CachePath || first.Callback.ScratchPath == second.Callback.ScratchPath {
		t.Fatal("cache sharing crossed scratch boundaries", first.Callback, second.Callback)
	}
	resource, err := b.Store.TaskResource(ctx, first.Callback.GoCacheID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := b.sharedGoOwner(ctx, resource.Owner.ID)
	if err != nil || !owner.Complete || !owner.OutputsSaved || owner.Presence != "absent" {
		t.Fatal(owner, err)
	}
	if !b.callbackResourceMetadataNeeded(ctx, first.ID) {
		t.Fatal("shared consumer exit proof can expire")
	}
	if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), resource.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{resource.ID})
	if err != nil || plan.Items[0].Decision.Outcome != "eligible" {
		t.Fatal(plan, err)
	}
	// An unregistered filename is retained. Below-budget shared entries remain
	// reusable, while all ordinary task caches keep independent ownership.
	result, err := b.Resources.Apply(ctx, ResourceOwnerScope(), plan, uuid.NewString())
	if err != nil || result.Items[0].Reason != "shared_cache_within_budget" {
		t.Fatal(result, err)
	}
	if raw, err := os.ReadFile(filepath.Join(first.Callback.GoCachePath, "proof")); err != nil || string(raw) != "reused" {
		t.Fatal(err)
	}
	for _, r := range []Record{first, second} {
		if _, err := os.Stat(filepath.Join(r.Dir, "artifacts", "result.txt")); err != nil {
			t.Fatal(err)
		}
	}
	// A later active consumer invalidates the reviewed old publication. It
	// cannot be made idle by an inspector or by keeping the Root alive.
	req := callbackRequest(t, uuid.NewString(), "sh", "-c", "sleep 2")
	req.Dir, req.Intent = project, CallbackIntentHeavy
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	// Acceptance precedes preparation. Protection starts with the durable
	// cache-consumer link, before the process can use the shared cache.
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, _, err := b.Record(ctx, req.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Callback.GoCacheID == resource.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("accepted callback never attached its shared cache")
		}
		time.Sleep(10 * time.Millisecond)
	}
	active, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{resource.ID})
	if err != nil || active.Items[0].Decision.Outcome == "eligible" {
		t.Fatal(active, err)
	}
	if _, err := b.Resources.Apply(ctx, ResourceOwnerScope(), plan, uuid.NewString()); err == nil {
		t.Fatal("stale consumer plan accepted")
	}
	denied := taskresources.Scope{Actor: "future-bot", Project: t.TempDir(), Inspect: true, Reclaim: true}
	if _, err := b.Resources.Plan(ctx, denied, []string{resource.ID}); err == nil {
		t.Fatal("cross-project cache inspection allowed")
	}
	settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
	// The original scratch floor remains independent of cache eviction.
	rows, _, _ := b.Store.TaskResourcesByOwnerPage(ctx, "task", first.ID, "")
	for _, row := range rows {
		if row.Class != taskresource.Cache {
			continue
		}
		if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), row.ID); err != nil {
			t.Fatal(err)
		}
		p, err := b.Resources.Plan(ctx, ResourceOwnerScope(), []string{row.ID})
		if err != nil || p.Items[0].Decision.Outcome != "eligible" {
			t.Fatal(p, err)
		}
	}
}
