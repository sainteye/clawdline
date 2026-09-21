package transcript

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// OpenQuestions is the questions a Claude session's open picker asked, whole,
// out of its own transcript (Transcript.openQuestions).
//
// The screen is a lossy copy: Claude Code fits a dialog to the window and cuts
// the paragraph under each option to whatever height is left. The call is on
// disk in full while the picker is still open, and **open is "nothing came
// after it"** — the moment it is answered a result lands behind it. So the
// newest entry decides, and anything else answers false and leaves the screen
// as the only source. All of the call's questions come back; which one is on
// screen is only the screen's to say (session.ShowingQuestion).
func (h *Host) OpenQuestions(ctx context.Context, s session.Session) ([]session.AskedQuestion, bool) {
	if s.Assistant != session.AssistantClaude || s.ConversationID == "" {
		return nil, false
	}
	cwd := s.CWD
	if r, ok := h.claude[s.PID]; ok && r.CWD != "" {
		cwd = r.CWD
	}
	if cwd == "" {
		return nil, false
	}
	page, err := ReadClaude(ClaudePath(h.Home, cwd, s.ConversationID), 1)
	if err != nil || len(page.Entries) == 0 {
		return nil, false
	}
	last := page.Entries[len(page.Entries)-1]
	if last.Kind != KindTool || last.Tool != AskTool {
		return nil, false
	}
	return askedQuestions(last.Text)
}

// askedQuestions is the other end of askPayload.
func askedQuestions(text string) ([]session.AskedQuestion, bool) {
	if !strings.HasPrefix(text, AskMarker) {
		return nil, false
	}
	var rows []struct {
		Q string `json:"q"`
		O []struct {
			L string `json:"l"`
			D string `json:"d"`
		} `json:"o"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(text, AskMarker)), &rows) != nil || len(rows) == 0 {
		return nil, false
	}
	out := make([]session.AskedQuestion, 0, len(rows))
	for _, row := range rows {
		q := session.AskedQuestion{Text: row.Q}
		for _, o := range row.O {
			if o.L == "" {
				continue
			}
			q.Options = append(q.Options, session.AskedOption{Label: o.L, Note: o.D})
		}
		out = append(out, q)
	}
	return out, true
}
