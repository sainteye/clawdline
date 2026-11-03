//go:build darwin || linux

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func geminiImageFile(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic.png")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func geminiImageReply(t *testing.T, s *Server, row session.Session, text string) contract.TranscriptPage {
	t.Helper()
	if err := os.WriteFile(filepath.Join(row.CWD, "reply-text"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	sendAutomaticFixture(t, s, row, "Synthetic request")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		page := s.transcriptPageBeforeProjection(row.ID, row, 200, 0, true, true)
		for _, e := range page.Entries {
			if e.Role == "assistant" {
				return page
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("assistant reply missing")
	return contract.TranscriptPage{}
}
func TestGeminiImageMarkersUseExistingArtifactProjection(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	s.pictures.store = artifacts.NewStore(t.TempDir())
	stored, err := s.pictures.store.ImportPaths(context.Background(), []string{geminiImageFile(t)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := stored[0].Artifact.ID
	page := geminiImageReply(t, s, row, "Screenshot\n"+artifacts.Marker(id))
	for _, e := range page.Entries {
		if e.Role != "assistant" {
			continue
		}
		if strings.Contains(e.Text, "clawdline-image") || len(e.Artifacts) != 1 || e.Artifacts[0].ID != id || e.Artifacts[0].State != "" {
			t.Fatal("native marker remained prose or lost its live artifact")
		}
		return
	}
	t.Fatal("assistant missing")
}
func TestGeminiImageSendBindsNextReplyWithoutAMarker(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	s.pictures.store = artifacts.NewStore(t.TempDir())
	body, _ := json.Marshal(map[string]any{"terminal": row.ID, "images": []map[string]string{{"path": geminiImageFile(t)}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts/images", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true}))
	rec := httptest.NewRecorder()
	s.imagesRoute(rec, req)
	var stored contract.ImageStoreResult
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &stored) != nil || len(stored.Artifacts) != 1 {
		t.Fatalf("owned image send refused: %d %s", rec.Code, rec.Body)
	}
	page := geminiImageReply(t, s, row, "Here is the screenshot without a marker")
	found := false
	for _, e := range page.Entries {
		if e.Role == "assistant" && len(e.Artifacts) == 1 && e.Artifacts[0].ID == stored.Artifacts[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatal("image did not attach to exact native reply")
	}
	if err := os.WriteFile(filepath.Join(row.CWD, "reply-text"), []byte("A later reply"), 0600); err != nil {
		t.Fatal(err)
	}
	sendAutomaticFixture(t, s, row, "A later request")
	h := s.antigravitySessions.Load()
	deadline := time.Now().Add(time.Second)
	var newest contract.TranscriptPage
	for time.Now().Before(deadline) {
		newest = s.transcriptPageBeforeProjection(row.ID, row, 1, 0, true, true)
		if len(newest.Entries) == 1 && newest.Entries[0].Text == "A later reply" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(newest.Entries) != 1 || len(newest.Entries[0].Artifacts) != 0 {
		t.Fatal("image moved to a later reply")
	}
	before := newest.NextBefore
	older := s.transcriptPageBeforeProjection(row.ID, row, 200, before, true, true, newest.Signature)
	count := 0
	for _, e := range older.Entries {
		count += len(e.Artifacts)
	}
	if count != 1 {
		t.Fatal("paging duplicated or lost the image", count)
	}
	if err := h.Shutdown(); err != nil {
		t.Fatal(err)
	}
	s.antigravitySessions.Store(nil)
	stop, err := s.StartAntigravitySessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	page = s.transcriptPageBeforeProjection(row.ID, row, 200, 0, true, true)
	count = 0
	for _, e := range page.Entries {
		count += len(e.Artifacts)
	}
	if count != 1 {
		t.Fatal("restart lost the image anchor", count)
	}

}

func TestGeminiMarkersPreserveFencesAndExplainMissingImages(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	s.pictures.store = artifacts.NewStore(t.TempDir())
	stored, err := s.pictures.store.ImportPaths(t.Context(), []string{geminiImageFile(t)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	live := stored[0].Artifact
	for _, tc := range []struct {
		name, text string
		now        time.Time
		state      contract.ImageAbsence
		count      int
	}{
		{"fenced", "```text\n" + artifacts.Marker(live.ID) + "\n```", time.Now(), "", 0},
		{"malformed", `<clawdline-image id="../file">`, time.Now(), "", 0},
		{"unknown", artifacts.Marker("11111111-1111-4111-8111-111111111111"), time.Now(), contract.ImageAbsenceUnknown, 1},
		{"expired", artifacts.Marker(live.ID), time.Unix(live.ExpiresAt+1, 0), contract.ImageAbsenceExpired, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []contract.TranscriptEntry{{Role: "assistant", Text: tc.text}, {Role: "user", Text: tc.text}}
			s.showOwnedPictures(t.Context(), s.antigravitySessions.Load(), row, rows, tc.now)
			if len(rows[0].Artifacts) != tc.count || len(rows[1].Artifacts) != 0 || rows[1].Text != tc.text {
				t.Fatal("image scope changed")
			}
			if tc.count == 0 && rows[0].Text != tc.text {
				t.Fatal("unrecognized marker disappeared")
			}
			if tc.count == 1 && rows[0].Artifacts[0].State != tc.state {
				t.Fatal("missing image state lost")
			}
		})
	}
}
