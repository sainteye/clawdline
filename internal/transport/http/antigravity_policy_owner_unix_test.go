//go:build darwin || linux

package http

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/antigravityproject"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// Actual SQLite consent and owned hook files, with only the provider image and
// process adapter synthetic. No provider, account or running daemon is used.
func TestAntigravityPolicyPublicationUsesCurrentProjectConsent(t *testing.T) {
	f := projectCloudStandIn(t)
	s := f.server
	project, ok := s.workV2Project(t.Context(), f.place)
	if !ok {
		t.Fatal("synthetic catalog missing")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopProject, err := s.StartAntigravityProjectConsent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopProject()
	stopStore, err := s.StartAntigravityLaunchStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopStore()
	native := newProductionPolicyFixture(t)
	bridge := native.server.antigravityBridge.Load()
	s.SetAntigravityBridge(bridge)
	defer s.SetAntigravityBridge(nil)
	withdraw := s.publishAntigravityPolicy(ctx)
	defer withdraw()
	inputs := s.antigravityPolicyInputs.Load()
	if inputs == nil {
		t.Fatal("complete owners did not publish")
	}
	if _, err := inputs.project(ctx, project.Path); err != antigravityproject.NotActive {
		t.Fatal("absent consent became authority", err)
	}
	manager := s.antigravityProjectOwner.Load()
	preview, err := manager.Preview(ctx, project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enable(ctx, preview, "synthetic-person", "synthetic-enable", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	got, err := inputs.project(ctx, project.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := manager.Check(ctx, project.Path)
	if err != nil || got.Revision != want.RevisionDigest || got.HookDigest != want.HookDigest || got.HelperDigest != want.HelperDigest {
		t.Fatal("published policy differs from actual consent", err)
	}
	// Real consent does not invent a native compatibility row.
	if _, _, err := inputs.inspect(ctx, project.Path, native.image, "ask"); err == nil {
		t.Fatal("unmeasured production row activated")
	}
	if _, err := inputs.project(ctx, t.TempDir()); err == nil {
		t.Fatal("unknown workspace acquired policy")
	}
	if _, err := manager.Disable(ctx, project.Path, want.Consent.Revision, "synthetic-person", "synthetic-disable", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := inputs.project(ctx, project.Path); err != antigravityproject.NotActive {
		t.Fatal("revocation retained published authority", err)
	}
	cancel()
	if err := s.currentAntigravityPolicy(context.Background(), inputs.launchStore, bridge, inputs); err == nil {
		t.Fatal("cancelled runtime retained authority")
	}
	if _, err := inputs.project(context.Background(), project.Path); err == nil {
		t.Fatal("cancelled closure retained authority")
	}
	stopProject()
	if s.antigravityPolicyInputs.Load() != nil {
		t.Fatal("Project stop retained inputs")
	}
}

func TestAntigravityPolicyPublicationRequiresOwnersAndWithdrawsOnlyItself(t *testing.T) {
	f := newProductionPolicyFixture(t)
	s := f.server
	s.antigravityPolicyInputs.Store(nil)
	bridge, store, manager := s.antigravityBridge.Load(), s.antigravityLaunchStore.Load(), s.antigravityProjectOwner.Load()
	for _, missing := range []string{"project", "store", "bridge", "cancelled"} {
		t.Run(missing, func(t *testing.T) {
			s.antigravityProjectOwner.Store(manager)
			s.antigravityLaunchStore.Store(store)
			s.SetAntigravityBridge(bridge)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch missing {
			case "project":
				s.antigravityProjectOwner.Store(nil)
			case "store":
				s.antigravityLaunchStore.Store(nil)
			case "bridge":
				s.SetAntigravityBridge(nil)
			case "cancelled":
				cancel()
			}
			withdraw := s.publishAntigravityPolicy(ctx)
			defer withdraw()
			if s.antigravityPolicyInputs.Load() != nil {
				t.Fatal("incomplete owners published inputs")
			}
		})
	}
	s.antigravityProjectOwner.Store(manager)
	s.antigravityLaunchStore.Store(store)
	s.SetAntigravityBridge(bridge)
	oldWithdraw := s.publishAntigravityPolicy(t.Context())
	old := s.antigravityPolicyInputs.Load()
	if old == nil {
		t.Fatal("missing first publication")
	}
	oldWithdraw()
	newerWithdraw := s.publishAntigravityPolicy(t.Context())
	defer newerWithdraw()
	newer := s.antigravityPolicyInputs.Load()
	if newer == nil || old == newer {
		t.Fatal("new publication not created")
	}
	oldWithdraw()
	if s.antigravityPolicyInputs.Load() != newer {
		t.Fatal("old stop removed new publication")
	}
	replacement := &antigravityproject.Manager{}
	s.antigravityProjectOwner.Store(replacement)
	if err := s.currentAntigravityPolicy(t.Context(), store, bridge, newer); err == nil {
		t.Fatal("replacement Project owner borrowed old inputs")
	}
	if _, err := newer.project(t.Context(), f.req.ProjectRoot); err == nil {
		t.Fatal("old closure borrowed replacement owner")
	}
	s.antigravityProjectOwner.Store(manager)
}

func TestAntigravityReviewStrategyDoesNotGrantOtherCeilings(t *testing.T) {
	for _, ceiling := range []string{"ask", "edits", "full", ""} {
		for _, strategy := range []antigravityNativeEnforcement{0, antigravityReviewEachTool, 255} {
			decision, err := (antigravityNativeBehavior{Ceiling: ceiling, Enforcement: strategy}).toolDecision()
			if ceiling == "ask" && strategy == antigravityReviewEachTool {
				if err != nil || decision != "force_ask" {
					t.Fatal(decision, err)
				}
			} else if err == nil || decision != "" {
				t.Fatal("unsupported strategy granted", ceiling, strategy)
			}
		}
	}
	// A behavior tuple must match the actual image, platform and architecture.
	for _, field := range []string{"image", "platform", "architecture", "id"} {
		t.Run(field, func(t *testing.T) {
			f := newProductionPolicyFixture(t)
			resolver := f.inputs.behavior
			f.inputs.behavior = func(i antigravity.ImageFingerprint, c string) (antigravityNativeBehavior, error) {
				b, e := resolver(i, c)
				switch field {
				case "image":
					b.ImageDigest = policyHash("other")
				case "platform":
					b.Platform = "other"
				case "architecture":
					b.Architecture = "other"
				case "id":
					b.ID = ""
				}
				return b, e
			}
			if _, err := f.server.prepareProductionAntigravity(t.Context(), f.req, ports.AntigravityLaunchIntent{LaunchID: uuid.NewString(), Ceiling: "ask"}); err == nil {
				t.Fatal("invalid tuple prepared launch")
			}
		})
	}
}

func TestAntigravityPolicyFollowsRuntimeStartupStopAndRestart(t *testing.T) {
	f := projectCloudStandIn(t)
	s := f.server
	template, _ := runtimeServer(t)
	// Use the existing short Unix-listener fixture directory. Go test names can
	// exceed the platform socket path length; SQLite remains the actual store.
	s.cfg.Dir = template.cfg.Dir
	s.antigravityInstallation = template.antigravityInstallation
	stopProject, err := s.StartAntigravityProjectConsent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stopProject()
	stopStore, err := s.StartAntigravityLaunchStore(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stopStore()
	if s.antigravityPolicyInputs.Load() != nil {
		t.Fatal("inputs preceded runtime")
	}
	stopRuntime, err := s.StartAntigravity(t.Context(), s.ManagedAntigravityOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer stopRuntime()
	old := s.antigravityPolicyInputs.Load()
	if old == nil || old.projectOwner != s.antigravityProjectOwner.Load() || old.launchStore != s.antigravityLaunchStore.Load() || old.bridge != s.antigravityBridge.Load() {
		t.Fatal("runtime did not capture actual owners")
	}
	stopRuntime()
	if s.antigravityPolicyInputs.Load() != nil || s.antigravityBridge.Load() != nil {
		t.Fatal("stopped runtime retained authority")
	}
	stopAgain, err := s.StartAntigravity(t.Context(), s.ManagedAntigravityOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer stopAgain()
	newer := s.antigravityPolicyInputs.Load()
	if newer == nil || newer == old {
		t.Fatal("restart reused old inputs")
	}
	stopRuntime()
	if s.antigravityPolicyInputs.Load() != newer {
		t.Fatal("old runtime cleanup withdrew replacement")
	}
	stopStore()
	if s.antigravityPolicyInputs.Load() != nil {
		t.Fatal("closed launch store retained authority")
	}
	stopAgain()
}
