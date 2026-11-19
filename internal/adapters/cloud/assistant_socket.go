package cloud

import (
	"encoding/json"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SetAssistantRail installs one enqueue-only callback for the Cloud assistant.
func (t *Transport) SetAssistantRail(fn func(uint64, []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.assistant, t.assistantReady, t.assistantWanted = fn, 0, 0
}

func (t *Transport) AssistantRailState() (uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.generation, t.conn != nil && t.assistant != nil && t.assistantReady == t.generation
}

func (t *Transport) NegotiateAssistantRail() error {
	t.mu.Lock()
	conn, gen := t.conn, t.generation
	if conn == nil || t.assistant == nil {
		t.mu.Unlock()
		return botFail(pa.Offline, "assistant_socket")
	}
	if t.assistantWanted == gen {
		t.mu.Unlock()
		return nil
	}
	t.assistantWanted = gen
	t.mu.Unlock()
	if err := conn.WriteText([]byte(`{"type":"bot_assistant_ready","version":1}`), t.opts.Now().Add(OpeningTimeout)); err != nil {
		// Fail this lane closed. The existing socket's own read/write lifecycle
		// owns connection recovery for its other traffic.
		return botFail(pa.OutcomeUnknown, "assistant_negotiation")
	}
	return nil
}

func (t *Transport) handleAssistantFrame(conn *Conn, kind string, raw []byte) {
	t.mu.Lock()
	if conn != t.conn || t.assistant == nil || t.assistantWanted != t.generation {
		t.mu.Unlock()
		return
	}
	if kind == "bot_assistant_ready" {
		var response struct {
			Type    string `json:"type"`
			Version int    `json:"version"`
		}
		if botDecode(raw, &response) == nil && response.Version == 1 {
			t.assistantReady = t.generation
		}
	}
	fn, gen, ready := t.assistant, t.generation, t.assistantReady == t.generation
	t.mu.Unlock()
	if kind == "bot_assistant_call" && ready && len(raw) <= PeerFrameBytesLimit {
		fn(gen, append([]byte(nil), raw...))
	}
}

func (t *Transport) ReplyAssistant(gen uint64, id, sealed string) error {
	raw, err := json.Marshal(map[string]string{"type": "bot_assistant_reply", "delivery_id": id, "sealed": sealed})
	if err != nil || len(raw) > PeerFrameBytesLimit || !botSegment(id) {
		return botFail(pa.Invalid, "assistant_reply")
	}
	t.mu.Lock()
	conn, current := t.conn, gen == t.generation && t.assistantReady == gen && t.assistant != nil
	t.mu.Unlock()
	if conn == nil || !current {
		return botFail(pa.Offline, "assistant_socket")
	}
	if err = conn.WriteText(raw, t.opts.Now().Add(OpeningTimeout)); err != nil {
		return botFail(pa.OutcomeUnknown, "assistant_reply")
	}
	return nil
}
