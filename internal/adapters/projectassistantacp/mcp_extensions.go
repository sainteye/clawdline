package projectassistantacp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Selected HTTP connectors share the owned MCP listener, process/RPC budgets
// and cancellation. Native configuration receives only a per-execution loopback
// capability, never the source connector endpoint or a daemon/human bearer.
func (s *ownedMCPServer) extension(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || (r.Method != "POST" && r.Method != "GET" && r.Method != "DELETE") {
		http.Error(w, string(pa.Invalid), 400)
		return
	}
	text := strings.TrimPrefix(r.URL.Path, "/mcp/extensions/")
	index, e := strconv.Atoi(text)
	if e != nil || strconv.Itoa(index) != text || index < 0 || index >= len(s.extensions) {
		http.Error(w, string(pa.NotFound), 404)
		return
	}
	connector := s.extensions[index]
	guard, ok := s.tools.(app.ExtensionGuard)
	if !ok {
		http.Error(w, string(pa.Unsupported), 403)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.limits.RPCTimeout)
	defer cancel()
	if e = guard.CheckExtension(ctx, connector.Ref); e != nil {
		http.Error(w, string(pa.Code(e)), 403)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(s.limits.FrameBytes)))
	if e != nil {
		http.Error(w, string(pa.CapacityFull), 413)
		return
	}
	if connector.Credential != nil {
		value, err := connector.Credential(ctx)
		if err != nil {
			http.Error(w, string(pa.Code(err)), 403)
			return
		}
		defer func() {
			for i := range value {
				value[i] = 0
			}
		}()
		if connector.Executable == "" {
			// The source-owned closure checks the captured revision as well as
			// returning the value. Header injection happens only at the backend.
			ctx = context.WithValue(ctx, connectorSecretKey{}, value)
		}
	}
	if index < len(s.stdio) && s.stdio[index] != nil {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		data, err := s.stdio[index].exchange(ctx, body)
		if err != nil {
			http.Error(w, string(pa.Code(err)), 502)
			return
		}
		if err = guard.CheckExtension(ctx, connector.Ref); err != nil {
			http.Error(w, string(pa.OutcomeUnknown), 409)
			return
		}
		if connector.Credential != nil {
			v, err := connector.Credential(ctx)
			for i := range v {
				v[i] = 0
			}
			if err != nil {
				http.Error(w, string(pa.OutcomeUnknown), 409)
				return
			}
		}
		if data == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
		return
	}
	request, e := http.NewRequestWithContext(ctx, r.Method, connector.URL, bytes.NewReader(body))
	if e != nil {
		http.Error(w, string(pa.Invalid), 400)
		return
	}
	for _, name := range []string{"Content-Type", "Accept", "Mcp-Session-Id", "MCP-Protocol-Version"} {
		if v := r.Header.Get(name); v != "" {
			request.Header.Set(name, v)
		}
	}
	if value, ok := ctx.Value(connectorSecretKey{}).([]byte); ok {
		request.Header.Set(connector.CredentialHeader, string(value))
	}
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: s.limits.RPCTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// No redirects, inherited proxy settings, automatic retries or credentials.
	response, e := client.Do(request)
	if e != nil {
		http.Error(w, string(pa.OutcomeUnknown), 502)
		return
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, int64(s.limits.FrameBytes)+1))
	if e != nil || len(data) > s.limits.FrameBytes {
		http.Error(w, string(pa.CapacityFull), 502)
		return
	}
	if e = guard.CheckExtension(ctx, connector.Ref); e != nil {
		http.Error(w, string(pa.OutcomeUnknown), 409)
		return
	}
	if connector.Credential != nil {
		v, err := connector.Credential(ctx)
		for i := range v {
			v[i] = 0
		}
		if err != nil {
			http.Error(w, string(pa.OutcomeUnknown), 409)
			return
		}
	}
	for _, name := range []string{"Content-Type", "Mcp-Session-Id"} {
		if v := response.Header.Get(name); v != "" {
			w.Header().Set(name, v)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(data)
}

type connectorSecretKey struct{}
