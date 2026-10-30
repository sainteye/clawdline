package http

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A producer callback, never a list/read side effect. It reuses the existing
// opt-in, selected namer, context scrubber, deadlines and small-turn capacity.
func (s *Server) automaticGeminiTitle(ctx context.Context, item session.Session) {
	values, err := s.settingsFile().Read()
	if err != nil || ctx.Err() != nil {
		return
	}
	enabled, _ := values.Bool("codex_auto_name")
	if !enabled || item.Backend != session.BackendOwned || item.Assistant != session.AssistantAntigravity || item.Binding != session.BindingProtocol || item.ConversationID == "" {
		return
	}
	// The host has already persisted its native turn. Its naming job must not
	// queue behind manual actions or delay the next native input.
	if !s.enterIntent() {
		return
	}
	defer s.leaveIntent()
	if !s.intentRun.TryLock() {
		return
	}
	defer s.intentRun.Unlock()
	live := liveOf(item)
	view := s.withOwnSessionTitles(ownrecords.Snapshot{}, time.Now())
	if s.store != nil {
		own, readErr := s.ownOverlay(ctx, []ownrecords.Live{live})
		if readErr != nil {
			return
		}
		view = s.withOwnSessionTitles(ownrecords.Snapshot{}.With(own), time.Now())
	}
	titles := view.TitleOf(live, item.CustomTitle, []ownrecords.Live{live})
	if titles.Manual != "" || titles.Orchestrator != "" || item.CustomTitle != "" || item.Rungs.Thread != "" {
		return
	}
	first, err := s.firstRequest(item)
	if err != nil || strings.TrimSpace(scrub(first)) == "" {
		return
	}
	assistant, err := s.namingAssistant()
	if err != nil {
		return
	}
	h := s.antigravitySessions.Load()
	if h == nil {
		return
	}
	claimed, err := h.ClaimAutomaticName(ctx, item)
	if err != nil || !claimed {
		return
	}
	asked := namingText(s.gatherNamingContext(ctx, item, first))
	title, _, tried, err := s.nameWith(ctx, asked, assistant)
	if err != nil {
		log.Printf("audit session.auto_title assistant=%s tried=%s ok=0 why=failed", assistant, tried)
		return
	}
	// Turning the feature off while a turn runs also prevents its publication.
	values, err = s.settingsFile().Read()
	if err != nil {
		return
	}
	enabled, _ = values.Bool("codex_auto_name")
	if !enabled {
		return
	}
	if err = h.SaveAutomaticName(ctx, item, normalizedSessionTitle(title)); err != nil {
		log.Printf("audit session.auto_title assistant=%s tried=%s ok=0 why=not_saved", assistant, tried)
		return
	}
	log.Printf("audit session.auto_title assistant=%s tried=%s ok=1", assistant, tried)
}
