package projectassistantacp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type extensionTestTools struct {
	mcpTestTools
	check func() error
}

func (t extensionTestTools) CheckExtension(context.Context, pa.ExtensionRef) error { return t.check() }

func TestConnectorProxyChecksCurrentAuthorityBeforeEffectAndPublication(t *testing.T) {
	var revoked, late atomic.Bool
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("execution bearer leaked to connector")
		}
		if late.Load() {
			revoked.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"synthetic":true}}`)
	}))
	defer target.Close()
	limits := testLimits()
	client := &Client{l: limits, rpcSlots: make(chan struct{}, limits.RPCPending)}
	ref := pa.ExtensionRef{ID: "connector:fixture", Fingerprint: strings.Repeat("a", 64)}
	tools := extensionTestTools{check: func() error {
		if revoked.Load() {
			return refusal(pa.Revoked, "fixture")
		}
		return nil
	}}
	lease, e := client.openMCP(context.Background(), app.OpenRequest{Binding: pa.ExecutionBinding{Fence: testFence()}, Tools: tools, Extensions: app.NativeExtensions{Connectors: []app.NativeConnector{{Ref: ref, Name: "fixture", URL: target.URL}}}})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := lease.Revoke(ctx); e != nil {
			t.Error(e)
		}
	}()
	call := func(token string, index string) (int, string) {
		request, _ := http.NewRequest("POST", lease.URL+"/extensions/"+index, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		b, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(b)
	}
	if code, _ := call("wrong", "0"); code != 403 || requests.Load() != 0 {
		t.Fatal("unauthenticated connector call", code)
	}
	if code, _ := call(lease.BearerToken, "1"); code != 404 || requests.Load() != 0 {
		t.Fatal("unselected connector reached backend", code)
	}
	if code, b := call(lease.BearerToken, "0"); code != 200 || !strings.Contains(b, "synthetic") {
		t.Fatal(code, b)
	}
	revoked.Store(true)
	if code, _ := call(lease.BearerToken, "0"); code != 403 || requests.Load() != 1 {
		t.Fatal("revoked connector executed", code, requests.Load())
	}
	revoked.Store(false)
	late.Store(true)
	if code, b := call(lease.BearerToken, "0"); code != 409 || strings.Contains(b, "synthetic") {
		t.Fatal("late revoked reply published", code, b)
	}
}
