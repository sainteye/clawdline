package http

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestBoardDeliveryUsesAgentLanguageForExistingSessions(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hant", "ja"} {
		t.Run(tag, func(t *testing.T) {
			s, p, item := workV2AssignmentServer(t, session.StateIdle)
			// Product and browser copy must not choose this task's language.
			if _, err := nextconfig.Open(s.broker.Dir).Set(map[string]any{"language": tag, "product_language": "de"}); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			assigned, err := s.assignWorkV2(ctx, item.Item.ID, "local", item.Item.Version, "existing_session", p.s.ID, "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			deliveries := p.done()
			if len(deliveries) != 1 {
				t.Fatalf("assignment deliveries: %v", deliveries)
			}
			text := deliveries[0]
			if !strings.Contains(text, "BCP 47") || !strings.Contains(text, tag) || !strings.Contains(text, item.Item.Title) || !strings.Contains(text, item.Item.AcceptanceCriteria) || !strings.Contains(text, "clawdline item phase "+item.Item.ID+" implementing") {
				t.Fatalf("assignment lost task language, authored data or commands: %s", text)
			}
			phrase := "Clawdline assigned you Board item"
			if tag == "zh-Hant" {
				phrase = "指派給你"
			}
			if !strings.Contains(text, phrase) {
				t.Fatalf("assignment ignored %s: %s", tag, text)
			}
			// A reminder to an already running Session rereads the Agent preference.
			if _, err := nextconfig.Open(s.broker.Dir).Set(map[string]any{"language": "zh-Hant"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.remindWorkV2(ctx, item.Item.ID, assigned.Item.Version); err != nil {
				t.Fatal(err)
			}
			deliveries = p.done()
			if len(deliveries) != 2 || !strings.Contains(deliveries[1], "看板項目提醒") || !strings.Contains(deliveries[1], "zh-Hant") {
				t.Fatalf("reminder did not read the new language: %v", deliveries)
			}
			// Reassign to a different conversation in the same fake terminal.
			p.s.ConversationID = "10000000-0000-4000-8000-000000000099"
			if _, err := s.assignWorkV2(ctx, item.Item.ID, "local", assigned.Item.Version, "existing_session", p.s.ID, "", "", nil); err != nil {
				t.Fatal(err)
			}
			deliveries = p.done()
			if len(deliveries) != 3 || !strings.Contains(deliveries[2], "轉派給你") || !strings.Contains(deliveries[2], "停止處接續") {
				t.Fatalf("reassignment language: %v", deliveries)
			}
		})
	}
}

func TestBoardNewSessionKeepsLanguageAcrossStartupDialog(t *testing.T) {
	s, p, item, mu, shown := dialogServer(t)
	if _, err := nextconfig.Open(s.broker.Dir).Set(map[string]any{"language": "zh-Hant"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	assigned, err := s.assignWorkV2(ctx, item.Item.ID, "local", item.Item.Version, "new_session", "", "codex", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.broker.RootAssignmentByID(ctx, assigned.Assignments[0].RootAssignment)
	if err != nil {
		t.Fatal(err)
	}
	brief, err := os.ReadFile(root.BriefPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brief), "zh-Hant") || !strings.Contains(root.Assignment.Acceptance, "實作、驗證、合併與部署") || !strings.Contains(string(brief), item.Item.AcceptanceCriteria) {
		t.Fatalf("new Root lost language or authored acceptance: %s", brief)
	}
	// The first visible message belongs to the launch already accepted, even if
	// the preference changes while the person answers the startup dialog.
	if _, err := nextconfig.Open(s.broker.Dir).Set(map[string]any{"language": "en"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	*shown = orchestratorScreen(t, "codex-composer")
	mu.Unlock()
	s.broker.Pass(ctx)
	deliveries := p.done()
	if len(deliveries) != 1 || !strings.Contains(deliveries[0], "請先讀取") || !strings.Contains(deliveries[0], root.BriefPath) {
		t.Fatalf("Root startup message changed language: %v", deliveries)
	}
}

func TestChineseBoardBriefKeepsEpicAndFeatureGates(t *testing.T) {
	item := work.ItemV2{ID: "item-1", Kind: work.KindEpic, Cycle: 1, GateSnapshotCycle: 1, PlanningGate: true, VerifyGate: true}
	for _, compose := range []func(work.ItemV2, ...string) string{workV2AssignmentBriefForItem, workV2ReassignmentBriefForItem, workV2RootAssignmentAcceptanceForItem} {
		text := compose(item, "zh-Hant")
		for _, want := range []string{"planning_gate=true", "verify_gate=true", "clawdline dispatch --kind plan_review", "clawdline guide epic", "clawdline item acceptance item-1", "completion_report"} {
			if !strings.Contains(text, want) {
				t.Fatalf("Chinese Epic lost %q: %s", want, text)
			}
		}
	}
	item.Kind = work.KindFeature
	without := workV2AssignmentBriefForItem(item, "zh-Hant")
	if !strings.Contains(without, "未勾選") || strings.Contains(without, "--kind plan_review") {
		t.Fatalf("unreviewed Feature: %s", without)
	}
	item.ReviewRequired = true
	with := workV2AssignmentBriefForItem(item, "zh-Hant")
	if !strings.Contains(with, "已勾選") || !strings.Contains(with, "--role plan") || !strings.Contains(with, "new_risk_boundary=false") {
		t.Fatalf("reviewed Feature: %s", with)
	}
}
