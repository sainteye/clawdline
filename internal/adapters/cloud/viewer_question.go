package cloud

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type ViewerQuestionOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type ViewerQuestion struct {
	Text        string                 `json:"text,omitempty"`
	Fingerprint string                 `json:"fingerprint"`
	Options     []ViewerQuestionOption `json:"options"`
	ObservedAt  int64                  `json:"observed_at"`
}

type viewerRich struct {
	seq  uint64
	body json.RawMessage
}

func viewerRichKey(destination ViewerDestination) string {
	return destination.MachineID + "\x00" + destination.SessionID
}

func (c *ViewerClient) openRich(destination ViewerDestination) {
	key := viewerRichKey(destination)
	c.richMu.Lock()
	c.richActive[key] = true
	delete(c.rich, key)
	c.richMu.Unlock()
}

func (c *ViewerClient) closeRich(destination ViewerDestination) {
	key := viewerRichKey(destination)
	c.richMu.Lock()
	delete(c.richActive, key)
	delete(c.rich, key)
	c.richMu.Unlock()
}

// acceptRich holds decrypted content only while the exact detail is open.
func (c *ViewerClient) acceptRich(envelope domain.Envelope, plaintext []byte) {
	parts := strings.Split(envelope.Ch, "/")
	if len(parts) != 3 || parts[0] != "s" || !json.Valid(plaintext) {
		return
	}
	machineID, machineErr := url.PathUnescape(parts[1])
	sessionID, sessionErr := url.PathUnescape(parts[2])
	if machineErr != nil || sessionErr != nil || machineID != envelope.Sender || sessionID == "" {
		return
	}
	key := machineID + "\x00" + sessionID
	c.richMu.Lock()
	if c.richActive[key] && envelope.Seq > c.rich[key].seq {
		c.rich[key] = viewerRich{seq: envelope.Seq, body: append(json.RawMessage(nil), plaintext...)}
		select {
		case c.richUpdate <- struct{}{}:
		default:
		}
	}
	c.richMu.Unlock()
}

// currentQuestion applies the hosted console's pinned menu checks to the
// signed rich row. A question without fresh source evidence cannot be answered.
func (c *ViewerClient) currentQuestion(destination ViewerDestination, now time.Time) *ViewerQuestion {
	c.richMu.Lock()
	row := append(json.RawMessage(nil), c.rich[viewerRichKey(destination)].body...)
	c.richMu.Unlock()
	if len(row) == 0 {
		return nil
	}
	var payload struct {
		Session struct {
			ID                  string `json:"id"`
			ExecutionGeneration string `json:"execution_generation"`
			Source              struct {
				Freshness  string `json:"freshness"`
				ObservedAt int64  `json:"observed_at"`
			} `json:"source"`
			Menu *struct {
				Question string `json:"question"`
				Options  []struct {
					N     int    `json:"n"`
					Label string `json:"label"`
					Can   bool   `json:"can"`
				} `json:"options"`
				Steps []struct {
					Done  bool   `json:"done"`
					Label string `json:"label"`
				} `json:"steps"`
			} `json:"menu"`
		} `json:"session"`
	}
	if json.Unmarshal(row, &payload) != nil || payload.Session.ID != destination.SessionID ||
		payload.Session.ExecutionGeneration != destination.ExecutionGeneration ||
		payload.Session.Source.Freshness != "current" || payload.Session.Source.ObservedAt <= 0 ||
		viewerOutsideFreshWindow(payload.Session.Source.ObservedAt, now) || payload.Session.Menu == nil ||
		len(payload.Session.Menu.Options) == 0 {
		return nil
	}
	menu := session.Menu{Question: payload.Session.Menu.Question}
	seen := make(map[int]bool)
	answerable := make([]ViewerQuestionOption, 0, len(payload.Session.Menu.Options))
	for _, option := range payload.Session.Menu.Options {
		if option.N < 1 || option.N > 9 || seen[option.N] || option.Label == "" {
			return nil
		}
		seen[option.N] = true
		menu.Options = append(menu.Options, session.MenuOption{Number: option.N, Label: option.Label})
		if option.Can {
			answerable = append(answerable, ViewerQuestionOption{Key: string(rune('0' + option.N)), Label: option.Label})
		}
	}
	if len(answerable) == 0 {
		return nil
	}
	for _, step := range payload.Session.Menu.Steps {
		menu.Steps = append(menu.Steps, session.MenuStep{Answered: step.Done, Label: step.Label})
	}
	return &ViewerQuestion{Text: menu.Question, Fingerprint: session.MenuFingerprint(menu),
		Options: answerable, ObservedAt: payload.Session.Source.ObservedAt * 1000}
}
