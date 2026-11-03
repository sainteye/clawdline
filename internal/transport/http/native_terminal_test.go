package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type nativeHTTPPane struct{ *pane }

func (h *nativeHTTPPane) NativeTerminalIdentity(context.Context, session.Session) (string, error) {
	return "verified-cli:birth", nil
}

func TestNativeTerminalUsesRealHTTPAdmissionAndIdempotentReceipts(t *testing.T) {
	f := newLocalAuthorityStandIn(t)
	p := waitingPane("%4", "Select a model\n> current\n  next")
	h := &nativeHTTPPane{p}
	hosts := []ports.TerminalHost{h}
	f.server.terminals = hosts
	f.server.inventory = app.Inventory{Terminals: hosts, Screen: p}
	path := "/v1/sessions/%254/native-terminal"
	unauth := call{method: http.MethodGet, path: path}.do(f.handler)
	if unauth.Code == 200 {
		t.Fatal("unauthenticated terminal read")
	}
	headers := map[string]string{"Authorization": "Bearer " + f.lan, "Content-Type": "application/json"}
	read := call{method: http.MethodGet, path: path, headers: headers}.do(f.handler)
	if read.Code != 200 {
		t.Fatalf("%d %s", read.Code, read.Body)
	}
	var frame app.NativeTerminalFrame
	if err := json.Unmarshal(read.Body.Bytes(), &frame); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(app.NativeTerminalInput{Expect: frame.Expect, Key: "down"})
	headers["Idempotency-Key"] = "native-key-1"
	for i := 0; i < 2; i++ {
		sent := call{method: http.MethodPost, path: path, headers: headers, body: string(raw)}.do(f.handler)
		if sent.Code != 200 {
			t.Fatalf("%d %s", sent.Code, sent.Body)
		}
	}
	if got := p.done(); len(got) != 1 || got[0] != "key:\x1b[B" {
		t.Fatalf("%q", got)
	}
	forbidden := call{method: http.MethodPost, path: path, body: string(raw), headers: map[string]string{"Content-Type": "application/json"}}.do(f.handler)
	if forbidden.Code == 200 || len(p.done()) != 1 {
		t.Fatal("unauthenticated input reached the CLI")
	}
}
