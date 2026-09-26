package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// keyBodyLimit is what one key weighs. `{"key":"shift+tab"}` is twenty bytes;
// four kilobytes is room for a body somebody wrote by hand, and for the
// question's sixty-four-digit fingerprint beside it, and nothing else.
//
// Every other body on this daemon is bounded — `/send` at the Swift server's
// twenty megabytes (actions.go), `/voice` at its own (voice.go), `/v1/settings`
// at 64 KiB — and this one was not, so `{"key":"<a few hundred megabytes>"}`
// was read into memory before the allowlist below ever looked at it. The
// server sets no `ReadTimeout` either, so it was read slowly if the caller
// liked. It needs send, which is exactly what a phone answering a menu has.
const keyBodyLimit = 4 << 10

// sessionKey answers POST /v1/sessions/{id}/key: one key for the menu on a
// session's screen. The caller has already passed the send permission.
//
// **The key is parsed before the session is looked up.** Not for secrecy — a
// well-formed key still tells you whether a session exists — but because the
// allowlist is what this route is for, and a check that runs after two other
// steps is a check somebody will later move.
//
// **`expect` is the question the answer was chosen for** (app.Actions.Key):
// given, the key is typed only at that question, and refused with nothing
// typed at any other.
func (s *Server) sessionKey(ctx context.Context, w http.ResponseWriter, id string, raw []byte) {
	var body contract.KeyRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a key")
		return
	}
	// Enter on a send that was typed and never submitted: pressed only at the
	// words it names (app.SubmitTyped), never at a menu.
	if body.Key == app.KeyEnter {
		if body.Expect != "" {
			writeRefusal(w, http.StatusBadRequest, "bad_request",
				"enter submits typed words and answers no question; it names no expect.")
			return
		}
		if _, err := s.actions().SubmitTyped(ctx, id, body.Typed); err != nil {
			writeActionRefusal(w, err)
			return
		}
		writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "keyed"})
		return
	}
	if _, _, ok := app.KeyName(body.Key); !ok {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			`key must be "1"…"9", "tab", "shift+tab" or "submit".`)
		return
	}
	if body.Expect != "" && !session.ValidFingerprint(body.Expect) {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"expect must be the fingerprint of the question being answered.")
		return
	}
	if _, err := s.actions().Key(ctx, id, body.Key, body.Expect); err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "keyed"})
}

// menuWire is contract.SessionMenu as it is sent. It exists for one key the
// generator cannot express: `checked` is optional and `false` is a real
// answer (an unticked row of a multi-select), so it must be a pointer rather
// than the generated bool, whose omitempty would drop it. `selected` is a
// pointer for the same reason.
type menuWire struct {
	Options  []menuOptionWire            `json:"options"`
	Question string                      `json:"question,omitempty"`
	Selected *int                        `json:"selected,omitempty"`
	Steps    []contract.SessionMenuStep  `json:"steps,omitempty"`
	Submit   *contract.SessionMenuSubmit `json:"submit,omitempty"`
}

type menuOptionWire struct {
	Can      bool   `json:"can"`
	Checked  *bool  `json:"checked,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Label    string `json:"label"`
	N        int    `json:"n"`
	Selected bool   `json:"selected"`
}

// wireMenu is RemoteServer.menuObject. Only a waiting session carries one.
func wireMenu(item session.Session) *menuWire {
	if item.Menu == nil || item.State != session.StateWaiting {
		return nil
	}
	m := item.Menu
	out := &menuWire{Options: make([]menuOptionWire, 0, len(m.Options)), Question: m.Question, Selected: m.Selected}
	for _, o := range m.Options {
		out.Options = append(out.Options, menuOptionWire{
			Can: o.Answerable(), Checked: o.Checked, Detail: o.Detail,
			Label: o.Label, N: o.Number, Selected: o.Selected,
		})
	}
	if m.Submit != nil {
		out.Submit = &contract.SessionMenuSubmit{Label: m.Submit.Label, Selected: m.Submit.Selected}
	}
	for _, step := range m.Steps {
		out.Steps = append(out.Steps, contract.SessionMenuStep{Label: step.Label, Done: step.Answered, Answer: step.Answer})
	}
	return out
}
