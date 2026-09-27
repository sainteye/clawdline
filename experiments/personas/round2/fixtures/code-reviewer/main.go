package main

import (
	"database/sql"
	"log"
	"net/http"
	"time"
	_ "time/tzdata" // BusinessLocation must load even on hosts without zoneinfo
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
		store:    NewStore(db),
		notifier: NewNotifier(cfg.WebhookURL, cfg.WebhookTimeout),
		payouts: &PayoutClient{
			baseURL:     cfg.PayoutURL,
			apiKey:      cfg.PayoutAPIKey,
			http:        &http.Client{Timeout: cfg.PayoutTimeout},
			maxAttempts: 3,
			backoff:     250 * time.Millisecond,
		},
		opts: ServerOptions{
			MaxPageSize: cfg.MaxPageSize,
			NotifyWait:  cfg.NotifyWait,
			DailyLimit:  cfg.DailyTransferLimit,
			Location:    cfg.BusinessLocation,
		},
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	log.Printf("walletd listening on %s (business day in %s)", cfg.Addr, cfg.BusinessLocation)
	log.Fatal(httpSrv.ListenAndServe())
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	owned := func(h http.HandlerFunc) http.Handler {
		return s.authenticate(s.requireOwner(h))
	}
	mux.Handle("GET /accounts/{id}", owned(s.getAccount))
	mux.Handle("GET /accounts/{id}/entries", owned(s.listEntries))
	mux.Handle("POST /accounts/{id}/deposits", owned(s.createDeposit))
	mux.Handle("POST /accounts/{id}/transfers", owned(s.createTransfer))
	mux.Handle("POST /accounts/{id}/payouts", owned(s.createPayout))
	mux.Handle("GET /transfers/{id}", owned(s.getTransfer))
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
