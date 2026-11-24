package http

import (
	"net/http"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// viewing is a console saying a Bot chat is on screen and in focus, or no
// longer is. It changes nothing stored: the report lives in memory for
// orchestrator.BotViewingSeconds and only holds back that chat's decision push.
func (h *projectAssistantHTTP) viewing(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	q, err := paDecodeHTTP[struct {
		Conversation string `json:"conversation"`
		Viewing      *bool  `json:"viewing"`
	}](w, r)
	if err != nil {
		return nil, err
	}
	if q.Conversation == "" || q.Viewing == nil {
		return nil, paHTTPFailure(pa.Invalid, "viewing")
	}
	if h.options.Viewing != nil {
		h.options.Viewing(route.bot, q.Conversation, *q.Viewing)
	}
	return map[string]bool{"ok": true}, nil
}

func (s *Server) reportBotViewing(bot, conversation string, viewing bool) {
	if s.broker != nil {
		s.broker.BotPresence.Report(bot, conversation, viewing, time.Now())
	}
}
