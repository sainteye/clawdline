package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
)

type Handler struct {
	store *Store
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	o, err := h.store.GetOrder(r.Context(), id)
	if errors.Is(err, ErrNotFound) || (err == nil && o.UserID != userID(r)) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("get order %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AmountCents int64 `json:"amount_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AmountCents <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	o := &Order{UserID: userID(r), Status: "pending", AmountCents: in.AmountCents}
	if err := h.store.CreateOrder(r.Context(), o); err != nil {
		log.Printf("create order: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (h *Handler) refundOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := h.store.Refund(r.Context(), id); errors.Is(err, ErrNotFound) {
		http.Error(w, "not found or not refundable", http.StatusNotFound)
		return
	} else if err != nil {
		log.Printf("refund %d: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
