//go:build darwin || linux

package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestGeminiSessionMessagesUseSharedTranscriptProjection(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	wire := `<clawdline-message>{"protocol":"clawdline.message","version":1,"kind":"session_message","source":{"id":"%1","assistant":"codex","label":"Synthetic coordinator"},"body":"Ready for **review**."}</clawdline-message>`
	sendAutomaticFixture(t, s, row, wire)
	// A later input puts the message on an older page as well as the full view.
	sendAutomaticFixture(t, s, row, "Next synthetic request")
	check := func() {
		t.Helper()
		h := s.antigravitySessions.Load()
		raw, err := h.Transcript(row, 200)
		if err != nil || len(raw.Entries) == 0 || raw.Entries[0].Role != "user" || raw.Entries[0].Text != wire {
			t.Fatal("native history was rewritten", err)
		}
		page := s.transcriptPageBeforeProjection(row.ID, row, 200, 0, true, true)
		assertMessage := func(e contract.TranscriptEntry) {
			t.Helper()
			if e.Role != "message" || e.Text != "Ready for **review**." || e.Source != "Synthetic coordinator" || e.SourceAssistant != "codex" || e.SourceMode != "clawdline" {
				t.Fatalf("session envelope did not reach the shared message card: %+v", e)
			}
			if e.ID != raw.Entries[0].ID || e.At != raw.Entries[0].At {
				t.Fatal("projection changed native identity or timestamp")
			}
		}
		assertMessage(page.Entries[0])
		newest := s.transcriptPageBeforeProjection(row.ID, row, 1, 0, true, true)
		older := s.transcriptPageBeforeProjection(row.ID, row, 200, newest.NextBefore, true, true, newest.Signature)
		if newest.NextBefore == 0 || len(older.Entries) == 0 {
			t.Fatal("older page missing")
		}
		assertMessage(older.Entries[0])
	}
	check()
	if err := s.antigravitySessions.Load().Shutdown(); err != nil {
		t.Fatal(err)
	}
	s.antigravitySessions.Store(nil)
	stop, err := s.StartAntigravitySessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	check()
}

func TestGeminiMessageProjectionPreservesOrdinaryTextAndImageReferences(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	wire := `<clawdline-message>{"protocol":"clawdline.message","version":1,"kind":"session_message","source":{"id":"%1","assistant":"claude","label":"Synthetic sender"},"body":"Review the sample."}</clawdline-message>`
	inputs := []string{"Ordinary request", "```\n" + wire + "\n```", strings.Replace(wire, `"version":1`, `"version":99`, 1), "Quoted: " + wire, wire}
	image := contract.ImageArtifact{ID: "11111111-1111-4111-8111-111111111111", MediaType: "image/png", ByteCount: 12, Width: 4, Height: 3, ExpiresAt: 1900000000}
	ref, _ := json.Marshal(map[string]any{"id": image.ID, "media_type": image.MediaType, "byte_count": image.ByteCount, "width": image.Width, "height": image.Height, "expires_at": image.ExpiresAt})
	withImage := strings.Replace(wire, `"version":1`, `"version":2,"artifacts":[`+string(ref)+`]`, 1)
	inputs = append(inputs, withImage)
	for _, text := range inputs {
		sendAutomaticFixture(t, s, row, text)
	}
	page := s.transcriptPageBeforeProjection(row.ID, row, 200, 0, true, true)
	index := 0
	for _, e := range page.Entries {
		if e.Role == "assistant" {
			continue
		}
		if index < 4 {
			if e.Role != "user" || e.Text != inputs[index] {
				t.Fatal("ordinary or invalid input became a session message", e)
			}
		} else if e.Role != "message" || e.SourceAssistant != "claude" || e.Text != "Review the sample." {
			t.Fatal("valid message was not projected", e)
		}
		if index == 5 && (len(e.Artifacts) != 1 || e.Artifacts[0] != image) {
			t.Fatal("version 2 lost its shared artifact projection", e.Artifacts)
		}
		index++
	}
	if index != len(inputs) {
		t.Fatalf("read %d inputs, want %d", index, len(inputs))
	}
}
