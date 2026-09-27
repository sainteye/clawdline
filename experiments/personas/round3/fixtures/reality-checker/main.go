package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("ADDR", "127.0.0.1:8080")
	data := env("DATA", "data/tasks.json")
	loc, err := time.LoadLocation(env("TZ_NAME", "Asia/Taipei"))
	if err != nil {
		log.Fatal(err)
	}
	migrated, err := Migrate(data)
	if err != nil {
		log.Fatal(err)
	}
	if migrated {
		log.Printf("migrated %s to v2 (backup at %s.v1.bak)", data, data)
	}
	store, err := OpenStore(data)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, NewServer(store, loc).Routes()))
}
