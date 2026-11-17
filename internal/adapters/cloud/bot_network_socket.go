package cloud

import (
	"encoding/json"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SetBotNetwork installs one enqueue-only callback. The owner must stop and
// detach the old rail before replacement. No peer or viewer frame uses it.
func (t *Transport) SetBotNetwork(fn func(uint64, []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.botCallback = fn
	t.botReady = 0
	t.botWanted = 0
}
func (t *Transport) BotNetworkState() (uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.generation, t.conn != nil && t.botReady == t.generation && t.botCallback != nil
}
func (t *Transport) NegotiateBotNetwork() error {
	t.mu.Lock()
	conn, gen := t.conn, t.generation
	if conn == nil || t.botCallback == nil {
		t.mu.Unlock()
		return botFail(pa.Unsupported, "bot_cloud_socket")
	}
	if t.botWanted == gen {
		t.mu.Unlock()
		return nil
	}
	t.botWanted = gen
	t.mu.Unlock()
	if e := conn.WriteText([]byte(`{"type":"bot_network_ready","version":1}`), t.opts.Now().Add(OpeningTimeout)); e != nil {
		_ = conn.Close()
		return botFail(pa.OutcomeUnknown, "bot_cloud_negotiation")
	}
	return nil
}
func (t *Transport) handleBotFrame(conn *Conn, kind string, raw []byte) {
	t.mu.Lock()
	if conn != t.conn || t.botCallback == nil || t.botWanted != t.generation {
		t.mu.Unlock()
		return
	}
	if kind == "bot_network_ready" {
		var f struct {
			Type    string `json:"type"`
			Version int    `json:"version"`
		}
		if botDecode(raw, &f) == nil && f.Version == 1 {
			t.botReady = t.generation
		}
		t.mu.Unlock()
		return
	}
	fn, gen, ready := t.botCallback, t.generation, t.botReady == t.generation
	t.mu.Unlock()
	if ready && len(raw) <= PeerFrameBytesLimit {
		fn(gen, append([]byte(nil), raw...))
	}
}
func (t *Transport) SendBotNetwork(gen uint64, raw []byte) error {
	if len(raw) > PeerFrameBytesLimit || !json.Valid(raw) {
		return botFail(pa.Invalid, "bot_cloud_frame")
	}
	var f frameHeader
	if json.Unmarshal(raw, &f) != nil || f.Type != "bot_network_publish" && f.Type != "bot_network_delivery_ack" {
		return botFail(pa.Invalid, "bot_cloud_frame")
	}
	t.mu.Lock()
	conn := t.conn
	if conn == nil || gen != t.generation || t.botReady != gen || t.botCallback == nil {
		t.mu.Unlock()
		return botFail(pa.Offline, "bot_cloud_socket")
	}
	t.mu.Unlock()
	if e := conn.WriteText(raw, t.opts.Now().Add(OpeningTimeout)); e != nil {
		_ = conn.Close()
		return botFail(pa.ExecutionEffectUnknown, "bot_cloud_write")
	}
	return nil
}
