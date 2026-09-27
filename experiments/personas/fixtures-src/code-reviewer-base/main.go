package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"
)

type Config struct {
	Addr       string
	DSN        string
	AdminToken string
}

func loadConfig() Config {
	return Config{
		Addr:       getenv("ADDR", ":8080"),
		DSN:        os.Getenv("DATABASE_URL"),
		AdminToken: os.Getenv("ADMIN_TOKEN"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()
	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store := NewStore(db)
	h := &Handler{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", requireUser(h.getOrder))
	mux.HandleFunc("POST /orders", requireUser(h.createOrder))
	mux.HandleFunc("POST /admin/orders/{id}/refund", requireAdmin(cfg.AdminToken, h.refundOrder))

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("listening on %s", cfg.Addr)
	log.Fatal(srv.ListenAndServe())
}
