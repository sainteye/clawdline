//go:build darwin || linux

package http

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// Only adapter contracts are synthetic here. This does not claim native
// permission acceptance, project consent or a live provider tool invocation.
type policyReader struct {
	process ports.AntigravityProcess
	plan    antigravity.NativeExecutionPlan
}

func (r *policyReader) ReadAntigravityProcess(ctx context.Context, _ int) (ports.AntigravityProcess, error) {
	return r.process, ctx.Err()
}
func (r *policyReader) ReadAntigravityExecutionPlan(ctx context.Context, _ int) (antigravity.NativeExecutionPlan, error) {
	return r.plan, ctx.Err()
}

type productionPolicyFixture struct {
	server  *Server
	owner   *antigravitylaunch.Store
	reader  *policyReader
	inputs  *antigravityPolicyInputs
	project antigravityProjectPolicy
	req     projects.LaunchRequest
	image   antigravity.ImageFingerprint
}

func policyHash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func newProductionPolicyFixture(t *testing.T) *productionPolicyFixture {
	t.Helper()
	s, state := runtimeServer(t)
	root := filepath.Dir(state)
	stop, err := s.StartAntigravityLaunchStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	workspace := filepath.Join(root, "project")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "synthetic-standalone")
	if err := os.WriteFile(executable, []byte("synthetic native image"), 0700); err != nil {
		t.Fatal(err)
	}
	p := ports.AntigravityProcess{PID: 321, ParentPID: 123, Birth: "2026-10-22T10:00:00.123456Z", Executable: executable, TTY: "ttySyntheticPolicy", TerminalID: "%synthetic-policy", BootID: "synthetic-boot", KernelGeneration: "synthetic-generation"}
	r := &policyReader{process: p, plan: antigravity.NativeExecutionPlan{Process: p, WorkspaceCWD: workspace, Mode: "plan", Effort: "high", Digest: policyHash("synthetic argv")}}
	bridge, err := antigravity.NewBridge(root, filepath.Join(root, "synthetic-observations.json"), executable, r, func(string, string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	s.SetAntigravityBridge(bridge)
	t.Cleanup(func() { s.SetAntigravityBridge(nil) })
	image, err := bridge.ImageFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f := &productionPolicyFixture{server: s, owner: s.antigravityLaunchStore.Load(), reader: r, image: image,
		req:     projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: workspace, Memory: "synthetic immutable memory", ReasoningEffort: "high"},
		project: antigravityProjectPolicy{Revision: policyHash("synthetic opt-in"), HookDigest: policyHash("synthetic hook plan"), HelperDigest: policyHash("synthetic helper")}}
	f.inputs = &antigravityPolicyInputs{
		project: func(_ context.Context, root string) (antigravityProjectPolicy, error) {
			if root != workspace {
				return antigravityProjectPolicy{}, fmt.Errorf("wrong synthetic scope")
			}
			return f.project, nil
		},
		behavior: func(got antigravity.ImageFingerprint, ceiling string) (antigravityNativeBehavior, error) {
			return antigravityNativeBehavior{ID: "synthetic-behavior-v1", ImageDigest: image.Digest, Platform: image.Platform, Architecture: image.Architecture, Ceiling: "ask", Mode: "plan", ToolDecision: "force_ask"}, nil
		},
		effective: func(context.Context, antigravitylaunch.Record, ports.AntigravityProcess, antigravity.NativeExecutionPlan) error {
			return nil
		},
	}
	s.antigravityPolicyInputs.Store(f.inputs)
	return f
}
func (f *productionPolicyFixture) prepare(t *testing.T) (string, antigravitylaunch.Record) {
	t.Helper()
	id := uuid.NewString()
	p, err := f.server.prepareProductionAntigravity(context.Background(), f.req, ports.AntigravityLaunchIntent{LaunchID: id, Ceiling: "ask"})
	if err != nil || p.LaunchID != id || p.Request.Executable != f.image.Executable || p.Request.PermissionMode != "plan" || len(p.PrivateEnv) != 1 || p.AttachTerminal == nil || p.AbortBeforeOpen == nil {
		t.Fatal("shared preparation missing", err)
	}
	if err := p.AttachTerminal(context.Background(), "tmux", f.reader.process.TerminalID); err != nil {
		t.Fatal(err)
	}
	record, err := f.owner.PreparedRecord(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return id, record
}
func (f *productionPolicyFixture) bind(t *testing.T, id string) ports.AntigravityBinding {
	t.Helper()
	ctx := context.Background()
	p := f.reader.process
	if err := f.owner.PinProcess(ctx, id, p); err != nil {
		t.Fatal(err)
	}
	b := ports.AntigravityBinding{Process: p, ConversationID: uuid.NewString(), TranscriptPath: filepath.Join(f.req.ProjectRoot, "synthetic-native.jsonl"), Event: "PreToolUse", Sequence: 1, ObservedAt: time.Now(), Verified: true}
	if err := f.owner.RecordBinding(ctx, id, b, antigravitylaunch.KeepConversation); err != nil {
		t.Fatal(err)
	}
	return b
}
func TestProductionAntigravityPreparationReusesOneIntentAndCanAbort(t *testing.T) {
	f := newProductionPolicyFixture(t)
	id, record := f.prepare(t)
	again, err := f.server.prepareProductionAntigravity(context.Background(), f.req, ports.AntigravityLaunchIntent{LaunchID: id, Ceiling: "ask"})
	if err != nil || again.LaunchID != id {
		t.Fatal("same intent not reused", err)
	}
	if err := f.server.checkProductionAntigravityPolicy(context.Background(), record, f.reader.process); err != nil {
		t.Fatal(err)
	}
	if err := again.AbortBeforeOpen(context.Background()); err == nil {
		t.Fatal("abort revoked an attached terminal")
	}
	freshID := uuid.NewString()
	unopened, err := f.server.prepareProductionAntigravity(context.Background(), f.req, ports.AntigravityLaunchIntent{LaunchID: freshID, Ceiling: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	if err := unopened.AbortBeforeOpen(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.PreparedRecord(context.Background(), freshID); err == nil {
		t.Fatal("aborted launch retained authority")
	}
	if _, err := f.server.prepareProductionAntigravity(context.Background(), f.req, ports.AntigravityLaunchIntent{LaunchID: freshID, Ceiling: "ask"}); err == nil {
		t.Fatal("aborted ID prepared again")
	}

}
func TestProductionAntigravityRejectsChangedInputsBeforeEffects(t *testing.T) {
	for _, kind := range []string{"consent", "hook", "helper", "mode", "model", "effort", "conversation", "workspace", "process", "effective", "runtime", "inputs", "revoke-during-inspection", "revoke-during-effective"} {
		t.Run(kind, func(t *testing.T) {
			f := newProductionPolicyFixture(t)
			id, record := f.prepare(t)
			switch kind {
			case "consent":
				f.project.Revision = policyHash("new consent")
			case "hook":
				f.project.HookDigest = policyHash("new hooks")
			case "helper":
				f.project.HelperDigest = policyHash("new helper")
			case "mode":
				f.reader.plan.Mode = "auto"
			case "model":
				f.reader.plan.Model = "different-model"
			case "effort":
				f.reader.plan.Effort = "max"
			case "conversation":
				f.reader.plan.ConversationID = uuid.NewString()
			case "workspace":
				f.reader.plan.WorkspaceCWD = filepath.Dir(f.req.ProjectRoot)
			case "process":
				f.reader.plan.Process.KernelGeneration = "another-generation"
			case "effective":
				f.inputs.effective = func(context.Context, antigravitylaunch.Record, ports.AntigravityProcess, antigravity.NativeExecutionPlan) error {
					return fmt.Errorf("synthetic changed effective mode")
				}
			case "runtime":
				f.server.SetAntigravityBridge(nil)
			case "inputs":
				f.server.antigravityPolicyInputs.Store(&antigravityPolicyInputs{})
			case "revoke-during-inspection":
				f.inputs.project = func(context.Context, string) (antigravityProjectPolicy, error) {
					_ = f.owner.RevokeLaunch(context.Background(), id)
					return f.project, nil
				}
			case "revoke-during-effective":
				f.inputs.effective = func(context.Context, antigravitylaunch.Record, ports.AntigravityProcess, antigravity.NativeExecutionPlan) error {
					return f.owner.RevokeLaunch(context.Background(), id)
				}
			}
			if err := f.server.checkProductionAntigravityPolicy(context.Background(), record, f.reader.process); err == nil {
				t.Fatal("changed policy accepted")
			}
		})
	}
}
func TestProductionAntigravityNeverActivatesMissingProductionEvidence(t *testing.T) {
	for _, kind := range []string{"no-inputs", "no-project", "no-native-row", "no-effective", "wrong-image", "edits", "full"} {
		t.Run(kind, func(t *testing.T) {
			f := newProductionPolicyFixture(t)
			ceiling := "ask"
			switch kind {
			case "no-inputs":
				f.server.antigravityPolicyInputs.Store(nil)
			case "no-project":
				f.inputs.project = nil
			case "no-native-row":
				f.inputs.behavior = nil
			case "no-effective":
				f.inputs.effective = nil
			case "wrong-image":
				f.inputs.behavior = func(antigravity.ImageFingerprint, string) (antigravityNativeBehavior, error) {
					return antigravityNativeBehavior{ID: "synthetic", ImageDigest: policyHash("wrong image"), Platform: f.image.Platform, Architecture: f.image.Architecture, Ceiling: "ask", Mode: "plan", ToolDecision: "force_ask"}, nil
				}
			case "edits", "full":
				ceiling = kind
			}
			if _, err := f.server.prepareProductionAntigravity(context.Background(), f.req, ports.AntigravityLaunchIntent{LaunchID: uuid.NewString(), Ceiling: ceiling}); err == nil {
				t.Fatal("missing production evidence accepted")
			}
			stats, err := f.owner.Stats(context.Background())
			if err != nil || stats.Launches != 0 {
				t.Fatal("refused policy reserved launch", err)
			}
		})
	}
}
func TestProductionAntigravityRevokedManagedProcessCannotBecomeUnmanaged(t *testing.T) {
	for _, kind := range []string{"active", "inactive", "revoked", "missing-owner"} {
		t.Run(kind, func(t *testing.T) {
			f := newProductionPolicyFixture(t)
			id, _ := f.prepare(t)
			binding := f.bind(t, id)
			switch kind {
			case "inactive":
				if err := f.owner.Deactivate(context.Background(), id); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := f.owner.RevokeLaunch(context.Background(), id); err != nil {
					t.Fatal(err)
				}
			case "missing-owner":
				f.server.antigravityLaunchStore.Store(nil)
			}
			response, err := f.server.managedAntigravityToolResponse(context.Background(), binding)
			if kind == "active" {
				if err != nil || string(response) != `{"decision":"force_ask"}` {
					t.Fatal("managed tool policy not selected", err)
				}
			} else {
				if err == nil || len(response) != 0 {
					t.Fatal("unavailable managed tool used observer fallback", err)
				}
				if err := f.server.checkAntigravityActionPolicy(context.Background(), binding); err == nil {
					t.Fatal("managed action borrowed unmanaged IO")
				}
				if _, err := f.server.managedAntigravityResponse(context.Background(), binding); kind != "missing-owner" && err == nil {
					t.Fatal("known inactive process borrowed unmanaged context response")
				}
			}
		})
	}
	f := newProductionPolicyFixture(t)
	binding := ports.AntigravityBinding{Process: f.reader.process, ConversationID: uuid.NewString(), Event: "PreToolUse", Verified: true}
	if err := f.server.checkAntigravityActionPolicy(context.Background(), binding); err != nil {
		t.Fatal("positively unmanaged process blocked", err)
	}
	response, err := f.server.managedAntigravityToolResponse(context.Background(), binding)
	if err != nil || string(response) != `{"decision":"ask"}`+"\n" {
		t.Fatal("unmanaged observer changed", err)
	}
}
