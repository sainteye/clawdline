package main

import (
	"log"
	"net/http"
	"os"
)

func NewServer(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/report", reportHandler(s))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	return mux
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, NewServer(NewStore(300, 1000))))
}
