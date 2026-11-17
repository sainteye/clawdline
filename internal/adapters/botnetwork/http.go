package botnetwork

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Handler accepts only signed encrypted Bot wire, never viewer bearer auth.
type Handler struct{ Service *app.Service }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != "/v1/bot-network/exchange" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if h.Service == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(&pa.Failure{Code: pa.Unsupported, Field: "bot_network_unconfigured"})
		return
	}
	// Wire framing reuses the existing peer body budget including base64 overhead.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(wire.MaxBodyBytes*2)))
	dec.DisallowUnknownFields()
	var e wire.BotEnvelope
	if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	out, err := h.Service.Receive(r.Context(), e)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(&pa.Failure{Code: pa.Code(err), Field: "bot_network_admission"})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

type HTTPTransport struct {
	URL    string
	Client *http.Client
}

func (t HTTPTransport) Exchange(ctx context.Context, e wire.BotEnvelope) (wire.BotEnvelope, error) {
	var out wire.BotEnvelope
	if t.Client == nil {
		return out, refuse(pa.Unsupported, "network_http_client")
	}
	b, err := json.Marshal(e)
	if err != nil {
		return out, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL+"/v1/bot-network/exchange", bytes.NewReader(b))
	if err != nil {
		return out, err
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := t.Client.Do(r)
	if err != nil {
		return out, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var refusal pa.Failure
		if json.NewDecoder(io.LimitReader(response.Body, int64(wire.MaxBodyBytes))).Decode(&refusal) == nil && refusal.Code != "" {
			return out, &refusal
		}
		return out, refuse(pa.OutcomeUnknown, "network_http_response")
	}
	dec := json.NewDecoder(io.LimitReader(response.Body, int64(wire.MaxBodyBytes*2)))
	dec.DisallowUnknownFields()
	err = dec.Decode(&out)
	return out, err
}
