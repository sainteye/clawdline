package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func (s *Server) secretHandoffNote(ctx context.Context, id string) (bool, bool, string, string) {
	note, err := s.store.HumanIntervention(ctx, id)
	if err != nil || note.Kind != "secret" {
		return false, false, "", ""
	}
	return true, note.ResolvedAt != nil, note.TargetConversation, note.TargetSession
}

// The person-facing response contains only a receipt. In particular the
// Note, work receipts, audit stream and Cloud response never hold the value.
func (s *Server) personSubmitSecretHandoff(w http.ResponseWriter, r *http.Request, conversation, id, actor string) {
	note, err := s.store.HumanIntervention(r.Context(), id)
	if err != nil || note.Kind != "secret" || note.TargetConversation != conversation {
		writeRefusal(w, http.StatusNotFound, "secret_handoff_unavailable", "This Secret Handoff is no longer available.")
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	if len(body.Value) == 0 {
		writeRefusal(w, http.StatusUnprocessableEntity, "secret_empty", "Paste a value before submitting.")
		return
	}
	if len(body.Value) > app.MaxSecretHandoffBytes {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "secret_too_large", "This value is too large for Secret Handoff.")
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	// Replay a successful submission before checking the now-completed Note.
	if note.ResolvedAt != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		writeRefusal(w, http.StatusNotFound, "secret_handoff_unavailable", "This Secret Handoff is no longer available.")
		return
	}
	answer := []byte(`{"ok":true,"received":true}`)
	// Completion acknowledges the person's handoff, not the tool's later
	// claim. Commit the Note and receipt together before publishing the value.
	if err := s.secretHandoffs.PutAndCommit(id, conversation, []byte(body.Value), func() error {
		_, _, err := s.humanInterventions().Update(r.Context(), id, conversation, "resolve", "", note.Version, k,
			func(work.HumanIntervention) []byte { return answer })
		return err
	}); err != nil {
		switch {
		case errors.Is(err, app.ErrSecretFull):
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			writeRefusal(w, http.StatusServiceUnavailable, "secret_handoffs_full", "Too many handoffs are pending. Try again later.")
		case errors.Is(err, app.ErrSecretUnavailable):
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			writeRefusal(w, http.StatusConflict, "secret_handoff_unavailable", "This Secret Handoff cannot accept another value.")
		default:
			writeRefusal(w, http.StatusServiceUnavailable, "receipt_unavailable", "The handoff receipt could not be recorded. Check the Session before trying again.")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// This machine-only route is used by the local CLI. It checks that the
// calling conversation still owns the exact target terminal. The value is
// delivered over a one-use loopback connection, never in an HTTP response.
func (s *Server) agentClaimSecretHandoff(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ConversationID string `json:"conversation_id"`
		CallbackPort   int    `json:"callback_port"`
		Nonce          string `json:"nonce"`
	}
	if _, ok := readWorkV2Body(w, r, &body); !ok {
		return
	}
	nonce, nonceErr := base64.RawURLEncoding.DecodeString(body.Nonce)
	if body.CallbackPort < 1 || body.CallbackPort > 65535 || nonceErr != nil || len(nonce) != 32 {
		writeRefusal(w, http.StatusBadRequest, "secret_handoff_callback_invalid", "The local handoff receiver is invalid.")
		return
	}
	valid, _, target, terminal := s.secretHandoffNote(r.Context(), id)
	if !valid || target != body.ConversationID || s.broker == nil {
		writeRefusal(w, http.StatusNotFound, "secret_handoff_unavailable", "This Secret Handoff is not available to this Session.")
		return
	}
	live, err := s.broker.LiveRootSession(r.Context(), body.ConversationID)
	if err != nil || live.ID != terminal {
		writeRefusal(w, http.StatusConflict, "secret_handoff_target_changed", "The target Session is no longer live at this terminal.")
		return
	}
	value, err := s.secretHandoffs.Take(id, target)
	if err != nil {
		writeRefusal(w, http.StatusConflict, "secret_handoff_unavailable", "The value was already claimed, expired, or has not arrived.")
		return
	}
	if note, err := s.store.HumanIntervention(r.Context(), id); err == nil {
		if _, _, err = s.humanInterventions().Update(r.Context(), id, target, "resolve", "Secret Handoff used", note.Version, store.ReceiptKey{}, nil); err == nil {
			s.secretHandoffs.ForgetClaim(id)
		}
	}
	defer func() {
		for i := range value {
			value[i] = 0
		}
	}()
	if err := deliverSecretHandoff(body.CallbackPort, nonce, value); err != nil {
		writeRefusal(w, http.StatusBadGateway, "secret_handoff_delivery_failed", "The one-time value could not reach the local tool receiver. It was consumed.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]bool{"ok": true, "delivered": true})
}

func deliverSecretHandoff(port int, nonce, value []byte) error {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp4", address, time.Duration(app.MaxSecretHandoffDeliverySeconds)*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(app.MaxSecretHandoffDeliverySeconds) * time.Second))
	frame := make([]byte, 4)
	binary.BigEndian.PutUint32(frame, uint32(len(value)))
	for _, part := range [][]byte{nonce, frame, value} {
		if _, err := io.Copy(conn, bytes.NewReader(part)); err != nil {
			return err
		}
	}
	var ack [1]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		return err
	}
	if ack[0] != 1 {
		return errors.New("the local receiver did not acknowledge")
	}
	return nil
}
