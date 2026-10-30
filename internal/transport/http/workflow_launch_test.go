package http

import (
	"context"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/skills"
	"testing"
)

func TestWorkflowInfoUsesTheLaunchSnapshotAndPreservesKnownOff(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	snap, err := skills.ResolveWorkflow("poteto", false)
	if err != nil {
		t.Fatal(err)
	}
	launch, err := st.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordWorkflowTerminal(ctx, launch.ID, "%1"); err != nil {
		t.Fatal(err)
	}
	if err := st.BindWorkflowConversation(ctx, launch.ID, "%1", "original"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, cfg: config.Config{Dir: t.TempDir()}}
	if _, err := s.settingsFile().Set(map[string]any{"workflow_profile": "clawdline", "graphify_enabled": true}); err != nil {
		t.Fatal(err)
	}
	info := contract.SessionInfo{}
	s.fillWorkflowInfo(ctx, session.Session{ID: "%1", ConversationID: "original"}, &info)
	if info.Workflow == nil || info.Workflow.State != "recorded" || info.Workflow.ProfileID != "poteto" || info.Workflow.Digest != snap.Digest || info.Workflow.GraphifyEnabled == nil || *info.Workflow.GraphifyEnabled {
		t.Fatalf("machine setting substituted for snapshot: %+v", info.Workflow)
	}
	encoded, err := json.Marshal(info.Workflow)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if known, ok := wire["graphify_enabled"]; !ok || known != false {
		t.Fatalf("known off became absent/unknown: %s", encoded)
	}
	s.fillWorkflowInfo(ctx, session.Session{ID: "%old", ConversationID: "legacy"}, &info)
	if info.Workflow.State != "legacy_unsnapshotted" || info.Workflow.ProfileID != "" || info.Workflow.GraphifyEnabled != nil {
		t.Fatalf("legacy session acquired current settings: %+v", info.Workflow)
	}
	st.Close()
	s.fillWorkflowInfo(ctx, session.Session{ID: "%1", ConversationID: "original"}, &info)
	if info.Workflow.State != "unavailable" {
		t.Fatalf("unreadable store became legacy/default: %+v", info.Workflow)
	}
}
