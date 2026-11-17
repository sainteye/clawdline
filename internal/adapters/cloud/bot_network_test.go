package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestBotNetworkMachineAPIUsesExactLiveAuthority(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	h := wire.BotHeader{Version: 1, RequestID: "request", Kind: "message", PairID: "pair", AuthorityID: "consent", AuthorityEpoch: 2, Source: wire.BotEndpoint{AccountID: "account", MachineID: "source"}, Target: wire.BotEndpoint{AccountID: "account", MachineID: "target"}}
	var calls atomic.Int32
	var response atomic.Value
	response.Store(`{"authorized":true,"account_id":"account","pair_id":"pair","authority_id":"consent","authority_epoch":2,"expires_at":` + jsonNumber(now.Add(time.Minute).Unix()) + `}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/bot-network/authorize" || r.Header.Get("Authorization") != "Bearer machine-only" {
			t.Error("wrong machine endpoint or authority")
			w.WriteHeader(401)
			return
		}
		var got struct {
			Header wire.BotHeader `json:"header"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&got) != nil || got.Header != h {
			t.Error("header changed")
		}
		w.Header().Set("Cache-Control", "no-store")
		body := response.Load().(string)
		if strings.Contains(body, `"authorized":false`) {
			w.WriteHeader(403)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	c := NewBotNetworkClient(Identity{AccountID: "account", MachineID: "source", MachineCredential: "machine-only", APIBase: server.URL})
	c.Clock = func() time.Time { return now }
	if e := c.CheckBotAuthority(context.Background(), h); e != nil {
		t.Fatal(e)
	}
	response.Store(`{"authorized":false,"reason":"consent_revoked"}`)
	if e := c.CheckBotAuthority(context.Background(), h); pa.Code(e) != pa.Revoked {
		t.Fatalf("revocation hidden: %v", e)
	}
	if calls.Load() != 2 {
		t.Fatal("positive result cached")
	}
	for _, raw := range []string{
		`{"authorized":true,"account_id":"other","pair_id":"pair","authority_id":"consent","authority_epoch":2,"expires_at":9999999999}`,
		`{"authorized":true,"account_id":"account","pair_id":"pair","authority_id":"consent","authority_epoch":1,"expires_at":9999999999}`,
		`{"authorized":true,"authorized":true,"account_id":"account","pair_id":"pair","authority_id":"consent","authority_epoch":2,"expires_at":9999999999}`,
		`{"authorized":true,"account_id":"account","pair_id":"pair","authority_id":"consent","authority_epoch":2,"expires_at":9999999999,"viewer":true}`,
		`{"authorized":true,"account_id":"account","pair_id":"pair","authority_id":"consent","authority_epoch":2}`,
		`{"authorized":true,"account_id":"account","pair_id":"pair","authority_id":"consent","authority_epoch":2,"expires_at":1}`,
	} {
		response.Store(raw)
		if e := c.CheckBotAuthority(context.Background(), h); pa.Code(e) != pa.Unsupported {
			t.Fatalf("bad response admitted: %s %v", raw, e)
		}
	}
	before := calls.Load()
	wrong := h
	wrong.Source.AccountID = "other"
	if e := c.CheckBotAuthority(context.Background(), wrong); pa.Code(e) != pa.ScopeDenied || calls.Load() != before {
		t.Fatal("foreign account reached private API")
	}
}
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }
func TestBotNetworkAPICapabilitiesAreClosedCompleteCaptures(t *testing.T) {
	ep := wire.BotEndpoint{AccountID: "account", MachineID: "source", ProjectID: "project", BotID: "bot", ConversationID: "conversation", Generation: 0}
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer srv.Close()
	c := NewBotNetworkClient(Identity{AccountID: "account", MachineID: "source", MachineCredential: "machine", APIBase: srv.URL})
	view := BotCapabilities{MachineID: "source", Revision: 1, WireVersion: 1, State: "ready", Endpoints: []wire.BotEndpoint{ep}, ExpiresAt: time.Now().Unix() + 60}
	b, _ := json.Marshal(view)
	raw = string(b)
	if _, e := c.Capabilities(context.Background(), "source"); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(raw, `"generation":0`, `"generation":null`, 1), strings.Replace(raw, `,"generation":0`, ``, 1), strings.Replace(raw, `"state":"ready"`, `"state":"unavailable"`, 1), strings.Replace(raw, `"machine_id":"source"`, `"machine_id":"other"`, 1)} {
		raw = bad
		if _, e := c.Capabilities(context.Background(), "source"); e == nil {
			t.Fatal("incomplete or replaced capture accepted", bad)
		}
	}
}
func TestBotNetworkNegotiationIsIndependentAndGenerationBound(t *testing.T) {
	conn, other := &Conn{}, &Conn{}
	var seen int
	tr := &Transport{conn: conn, generation: 7, botWanted: 7, botCallback: func(g uint64, b []byte) {
		if g != 7 {
			t.Error("changed connection")
		}
		seen++
	}}
	tr.handleBotFrame(conn, "bot_network_envelope", []byte(`{"type":"bot_network_envelope"}`))
	if seen != 0 {
		t.Fatal("unnegotiated ingress")
	}
	tr.handleBotFrame(other, "bot_network_ready", []byte(`{"type":"bot_network_ready","version":1}`))
	if _, ready := tr.BotNetworkState(); ready {
		t.Fatal("old socket negotiated")
	}
	for _, raw := range []string{`{"type":"bot_network_ready","version":2}`, `{"type":"bot_network_ready","version":1,"viewer":true}`, `{"type":"bot_network_ready","version":1,"version":1}`} {
		tr.handleBotFrame(conn, "bot_network_ready", []byte(raw))
		if _, ready := tr.BotNetworkState(); ready {
			t.Fatal("invalid ready accepted", raw)
		}
	}
	tr.handleBotFrame(conn, "bot_network_ready", []byte(`{"type":"bot_network_ready","version":1}`))
	tr.handleBotFrame(conn, "bot_network_envelope", []byte(`{"type":"bot_network_envelope"}`))
	if seen != 1 {
		t.Fatal("dedicated callback not used")
	}
	tr.generation++
	if _, ready := tr.BotNetworkState(); ready {
		t.Fatal("readiness survived connection replacement")
	}
}
