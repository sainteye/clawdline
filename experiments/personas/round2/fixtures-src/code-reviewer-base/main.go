package main

import (
	"database/sql"
	"log"
	"net/http"
	"time"
)

// The database/sql driver named by WALLET_DB_DRIVER is linked in by the
// deployment build; this package only depends on database/sql.

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := sql.Open(cfg.DBDriver, cfg.DSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	srv := &Server{
		store:       NewStore(db),
		notifier:    NewNotifier(cfg.WebhookURL, cfg.WebhookTimeout),
		maxPageSize: cfg.MaxPageSize,
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	log.Printf("walletd listening on %s", cfg.Addr)
	log.Fatal(httpSrv.ListenAndServe())
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("GET /accounts/{id}", s.authenticate(s.requireOwner(s.getAccount)))
	mux.Handle("GET /accounts/{id}/entries", s.authenticate(s.requireOwner(s.listEntries)))
	mux.Handle("POST /accounts/{id}/deposits", s.authenticate(s.requireOwner(s.createDeposit)))
	return logRequests(mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
