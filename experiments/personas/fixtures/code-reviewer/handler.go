package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
)

type Handler struct {
	store *Store
	cache *OrderCache
}

const (
	maxBatchIDs = 100
	batchChunk  = 20
)

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
	if o, ok := h.cache.Get(id); ok && o.UserID == userID(r) {
		writeJSON(w, http.StatusOK, o)
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
	h.cache.Set(*o)
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
	h.cache.Delete(id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 1)
	size := atoiDefault(q.Get("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	orders, total, err := h.store.ListOrders(r.Context(), ListParams{
		UserID:   userID(r),
		Status:   q.Get("status"),
		Sort:     q.Get("sort"),
		Page:     page,
		PageSize: size,
	})
	if errors.Is(err, ErrInvalidStatus) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Printf("list orders: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"orders":    orders,
		"total":     total,
		"page":      page,
		"page_size": size,
	})
}

// batchGetOrders returns the caller's orders among the requested ids, serving what it can
// from the cache and loading the rest from the database in parallel chunks.
func (h *Handler) batchGetOrders(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.IDs) == 0 || len(in.IDs) > maxBatchIDs {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var found []Order
	var misses []int64
	for _, id := range in.IDs {
		if o, ok := h.cache.Get(id); ok {
			found = append(found, o)
		} else {
			misses = append(misses, id)
		}
	}

	var chunks [][]int64
	for start := 0; start < len(misses); start += batchChunk {
		chunks = append(chunks, misses[start:min(start+batchChunk, len(misses))])
	}
	results := make([][]Order, len(chunks))
	errs := make([]error, len(chunks))
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = h.store.GetOrdersByIDs(r.Context(), chunk)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			log.Printf("batch get orders: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	for _, rs := range results {
		for _, o := range rs {
			h.cache.Set(o)
			found = append(found, o)
		}
	}

	uid := userID(r)
	out := make([]Order, 0, len(found))
	for _, o := range found {
		if o.UserID == uid {
			out = append(out, o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

func (h *Handler) deleteOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteOrder(r.Context(), id); err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("delete order %d: %v", id, err)
	}
	h.cache.Delete(id)
	w.WriteHeader(http.StatusNoContent)
}
