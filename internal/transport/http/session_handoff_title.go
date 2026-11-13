package http

import (
	"context"
	"log"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// automaticNamingEnabled is the one opt-in every automatic title reads:
// `codex_auto_name`, or, where it was never written, any naming assistant
// other than "off".
func automaticNamingEnabled(values nextconfig.Values) bool {
	enabled, ok := values.Bool("codex_auto_name")
	if !ok {
		if asst, hasAsst := values.String("auto_name_assistant"); hasAsst && asst != "off" {
			enabled = true
		}
	}
	return enabled
}

// nameUntitledHandoff names a milestone handoff its sender did not title,
// from the summary's Goal and Next step. The title becomes the handoff's own,
// so it outranks the name the receiver's assistant derives from the generic
// handoff line. It runs after the response; a failure leaves the tab as the
// assistant names it.
func (s *Server) nameUntitledHandoff(h orchestrator.Handoff) {
	if h.Title != "" || !h.Milestone || h.Opened == nil {
		return
	}
	asked, ok := orchestrator.MilestoneNamingText(h)
	if !ok {
		return
	}
	values, err := s.settingsFile().Read()
	if err != nil || !automaticNamingEnabled(values) {
		return
	}
	assistant, err := s.namingAssistant()
	if err != nil {
		return
	}
	if assistant == "session" {
		assistant = h.Assistant
	}
	if !s.enterIntent() {
		log.Printf("audit handoff.auto_title assistant=%s ok=0 why=busy", assistant)
		return
	}
	defer s.leaveIntent()
	s.intentRun.Lock()
	defer s.intentRun.Unlock()

	budget := time.Duration(2*CapacityLimit(capacity.IntentPlannerSeconds)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	title, _, tried, err := s.nameWith(ctx, asked, assistant)
	if err != nil {
		log.Printf("audit handoff.auto_title assistant=%s tried=%s ok=0 why=failed", assistant, tried)
		return
	}
	// Turning the feature off while the turn ran also prevents publication.
	if values, err = s.settingsFile().Read(); err != nil || !automaticNamingEnabled(values) {
		return
	}
	if _, set, err := s.broker.NameHandoff(ctx, h.ID, normalizedSessionTitle(title)); err != nil || !set {
		log.Printf("audit handoff.auto_title assistant=%s tried=%s ok=0 why=not_saved", assistant, tried)
		return
	}
	log.Printf("audit handoff.auto_title assistant=%s tried=%s ok=1", assistant, tried)
}
