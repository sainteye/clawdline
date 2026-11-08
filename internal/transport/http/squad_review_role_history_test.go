package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// These are the released role versions, not hashes derived from the new catalog.
var earlierReviewRoles = map[string]string{
	"code-reviewer":      "sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead",
	"reality-checker":    "sha256:bcb0a355ecdbd66040d1054705d7f85c481caa21403b7fde030b904c102d799d",
	"evidence-collector": "sha256:3ad3ff4aa4f43487e08f62e240fd91bcb1af24c2489af51f3f785815adac36fe",
}

func TestEarlierReviewRoleVersionsRemainReadable(t *testing.T) {
	f := newSquadFixture(t)
	for id, version := range earlierReviewRoles {
		t.Run(id, func(t *testing.T) {
			status, raw := f.ask("GET", "/v1/squad/definitions/"+id+"?version="+version, f.reader, "", "")
			var got squad.Definition
			if status != 200 || json.Unmarshal([]byte(raw), &got) != nil || got.Version != version {
				t.Fatalf("previous %s role = %d %s", id, status, raw)
			}
			// Prove the exact old body and links, rather than relabeling new text.
			digest := squad.Digest(struct {
				Body   string
				Source string
				Teams  []squad.Reference
				Skills []squad.SkillReference
			}{got.Body, got.Source, got.Teams, got.Skills})
			if "sha256:"+digest != version {
				t.Fatalf("immutable version %s returned different content", version)
			}
			variants := []squad.Definition{squad.PreSkillBuiltinVersion(got)}
			if previous, ok := squad.HistoricalBuiltinDefinition(got); ok {
				variants = append(variants, previous)
			}
			for _, old := range variants {
				status, raw = f.ask("GET", "/v1/squad/definitions/"+id+"?version="+old.Version, f.reader, "", "")
				var restored squad.Definition
				if status != 200 || json.Unmarshal([]byte(raw), &restored) != nil || restored.Body != got.Body || restored.Version != old.Version {
					t.Fatalf("earlier skill %s role = %d %s", id, status, raw)
				}
			}
			if status, _ := f.ask("GET", "/v1/squad/definitions/"+id+"?version=sha256:unknown", f.reader, "", ""); status != 404 {
				t.Fatalf("unknown role version = %d", status)
			}
		})
	}
}

func TestReviewRoleLaunchPublishesNewBodyAndKeepsTheHandbook(t *testing.T) {
	f := newSquadFixture(t)
	for id, oldVersion := range earlierReviewRoles {
		t.Run(id, func(t *testing.T) {
			binding, _ := json.Marshal(map[string]any{
				"definition_id": id, "expected_version": 0,
				"overrides": map[string]any{"handbook": map[string]any{"present": true, "value": "Keep the team's release checklist."}},
			})
			if status, raw := f.ask("PUT", "/v1/squad/settings", f.local, string(binding), "review-handbook-"+id); status != 200 {
				t.Fatalf("handbook = %d %s", status, raw)
			}
			snapshot, err := f.s.ResolveSquadLaunch(context.Background(), id, "")
			if err != nil || snapshot.Definition.Version == oldVersion || snapshot.Handbook.Text != "Keep the team's release checklist." ||
				len(snapshot.Skills) == 0 || len(snapshot.Skills) != len(snapshot.Definition.Skills) {
				t.Fatalf("new review launch = %+v, %v", snapshot, err)
			}
			document, _ := json.Marshal(snapshot)
			hash := sha256.Sum256(document)
			files, err := squadfiles.Publish(t.TempDir(), "ABCDEFGHIJKLMNOPQRSTUV", hex.EncodeToString(hash[:]), "test-capability", document)
			if err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(files.PromptPath)
			if err != nil || !strings.Contains(string(prompt), strings.TrimPrefix(snapshot.Definition.Body, persona.Preamble)) || !strings.Contains(string(prompt), snapshot.Handbook.Text) {
				t.Fatalf("published role or handbook differs from its snapshot: %v", err)
			}
			stored, err := os.ReadFile(files.SnapshotPath)
			if err != nil || !bytes.Equal(stored, document) {
				t.Fatalf("published snapshot bytes changed: %v", err)
			}
		})
	}
}
