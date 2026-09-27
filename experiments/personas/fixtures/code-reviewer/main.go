package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr            string
	DSN             string
	AdminToken      string
	CacheTTLSeconds int
}

func loadConfig() Config {
	return Config{
		Addr:            getenv("ADDR", ":8080"),
		DSN:             os.Getenv("DATABASE_URL"),
		AdminToken:      os.Getenv("ADMIN_TOKEN"),
		CacheTTLSeconds: atoiDefault(os.Getenv("CACHE_TTL_SECONDS"), 30),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func main() {
	cfg := loadConfig()
	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store := NewStore(db)
	cache := NewOrderCache(time.Duration(cfg.CacheTTLSeconds))
	h := &Handler{store: store, cache: cache}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders", requireUser(h.listOrders))
	mux.HandleFunc("GET /orders/{id}", requireUser(h.getOrder))
	mux.HandleFunc("POST /orders", requireUser(h.createOrder))
	mux.HandleFunc("POST /orders/batch", requireUser(h.batchGetOrders))
	mux.HandleFunc("POST /admin/orders/{id}/refund", requireAdmin(cfg.AdminToken, h.refundOrder))
	mux.HandleFunc("DELETE /admin/orders/{id}", h.deleteOrder)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("listening on %s", cfg.Addr)
	log.Fatal(srv.ListenAndServe())
}
