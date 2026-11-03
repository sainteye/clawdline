package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
)

func (s *Server) nativeTerminalScreen(w http.ResponseWriter, r *http.Request, id string) {
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	frame, err := s.actions().NativeTerminal(r.Context(), id, nil)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	writeJSON(w, frame)
}

func (s *Server) nativeTerminalInput(ctx context.Context, w http.ResponseWriter, id string, raw []byte) {
	var input app.NativeTerminalInput
	if json.Unmarshal(raw, &input) != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "bad_request")
		return
	}
	if _, err := s.actions().NativeTerminal(ctx, id, &input); err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "keyed"})
}
