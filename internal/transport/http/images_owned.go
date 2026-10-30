package http

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func ownedGeminiImages(row session.Session) bool {
	return row.Backend == session.BackendOwned && row.Binding == session.BindingProtocol && row.Assistant == session.AssistantAntigravity
}

// Keep artifact liveness dynamic at the shared HTTP projection boundary. Stored
// native text and its generation stay intact; markers inside fences stay text.
func (s *Server) showOwnedPictures(ctx context.Context, h *antigravitysession.Host, item session.Session, rows []contract.TranscriptEntry, now time.Time) {
	for i := range rows {
		if rows[i].Role != "assistant" {
			continue
		}
		text, ids := transcript.AssistantImageMarkers(rows[i].Text)
		if len(ids) > 0 {
			rows[i].Text = strings.TrimSpace(text)
			for _, image := range s.pictures.wireArtifacts(transcript.Entry{ArtifactIDs: ids}, now) {
				if !slices.ContainsFunc(rows[i].Artifacts, func(a contract.ImageArtifact) bool { return a.ID == image.ID }) {
					rows[i].Artifacts = append(rows[i].Artifacts, image)
				}
			}
		}
	}
	if s.pictures.store == nil {
		return
	}
	record, _, err := h.ImageAddress(ctx, item)
	if err != nil {
		return
	}
	generation := record[strings.LastIndex(record, ":")+1:]
	for _, picture := range s.pictures.store.ShownIn(record, now) {
		anchor := picture.Anchor
		if anchor < 0 {
			anchor, err = h.ImageReplyOrdinal(ctx, item, record, picture.Offset)
			if err != nil || anchor < 0 {
				continue
			}
			s.pictures.store.SetAnchor(picture.ID, anchor)
		}
		for i := range rows {
			row := &rows[i]
			if row.Role != "assistant" {
				continue
			}
			if row.ID != "acp2:"+generation+":"+strconv.FormatInt(anchor, 10) {
				continue
			}
			if !slices.ContainsFunc(row.Artifacts, func(a contract.ImageArtifact) bool { return a.ID == picture.ID }) {
				row.Artifacts = append(row.Artifacts, s.pictures.resolve(picture.ID, now))
			}
			break
		}
	}
}
