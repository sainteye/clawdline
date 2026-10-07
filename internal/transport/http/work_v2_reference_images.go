package http

import (
	"context"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"net/http"
)

func referenceImageFileRefusal(err error) (*app.WorkError, bool) {
	switch {
	case errors.Is(err, store.ErrReferenceImageMissing):
		return &app.WorkError{Status: http.StatusGone, Code: "image_file_missing",
			Message: "That reference image's file is gone from this machine."}, true
	case errors.Is(err, store.ErrReferenceImageMismatch):
		return &app.WorkError{Status: http.StatusGone, Code: "image_file_mismatch",
			Message: "That reference image's file is not the picture that was stored."}, true
	}
	return nil, false
}

func (s *Server) workV2ReferenceImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is read with GET.")
		return
	}
	// `?size=thumb` is the picture a card or a to-do row draws: long edge at
	// most artifacts.MaxThumbnailEdge, as JPEG. No query is the full stored
	// PNG, which is what the full-size viewer asks for. Anything else is a
	// spelling this route does not know, refused rather than read as "full".
	query := r.URL.Query()
	thumb := false
	for key, values := range query {
		if key != "size" || len(values) != 1 || values[0] != "thumb" {
			writeRefusal(w, http.StatusBadRequest, "bad_query", "A reference image is read whole, or with size=thumb.")
			return
		}
		thumb = true
	}
	data, mediaType, ok, err := s.store.WorkV2ImageBytes(r.Context(), id)
	if refusal, named := referenceImageFileRefusal(err); named {
		writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		return
	}
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The reference image could not be read.")
		return
	}
	if !ok {
		writeRefusal(w, http.StatusNotFound, "image_not_found", "No reference image has that id.")
		return
	}
	if thumb {
		// The stored image is read first either way, so a deleted image is a
		// 404 and never a thumbnail the cache still holds.
		t, err := s.thumbs.Get(id, data)
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The reference image could not be drawn small.")
			return
		}
		data, mediaType = t.JPEG, "image/jpeg"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

func (s *Server) workV2AddImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is added with POST.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int64  `json:"expected_version"`
		Title           string `json:"title"`
		DataURL         string `json:"data_url"`
		Position        int64  `json:"position"`
	}
	raw, ok := readWorkV2BodyAtMost(w, r, &body, workV2ImageBodyLimit, "A reference-image request is at most 18 MiB.")
	if !ok {
		return
	}
	bytes, ok := artifacts.DecodeDataURL(body.DataURL)
	if !ok {
		writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
		return
	}
	policy := artifacts.ProductionPolicy
	policy.MaxEncodedBytes = workV2ImageByteLimit
	normalized, err := artifacts.Normalize(r.Context(), bytes, policy)
	if err != nil {
		var refusal artifacts.Refusal
		if errors.As(err, &refusal) {
			writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
		} else {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
		}
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	_, err = s.workV2().AddImage(r.Context(), id, app.AddImageV2{ExpectedVersion: body.ExpectedVersion,
		Title: body.Title, Data: normalized.Data, MediaType: normalized.MediaType, Width: normalized.Width, Height: normalized.Height,
		Position: body.Position, Actor: actor}, func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
		answer = workV2Answer(itemOf(v))
		return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
	})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(answer)
}

func (s *Server) workV2DeleteImage(w http.ResponseWriter, r *http.Request, id, imageID string) {
	if r.Method != http.MethodDelete {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A reference image is removed with DELETE.")
		return
	}
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	raw, ok := readWorkV2Body(w, r, &body)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	itemOf := s.workV2ItemProjector(r.Context())
	var answer []byte
	_, err := s.workV2().DeleteImage(r.Context(), id, imageID, body.ExpectedVersion, actor,
		func(v app.WorkV2View) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = workV2Answer(itemOf(v))
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}
