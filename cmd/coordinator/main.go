package main

import (
	"log"
	"os"

	"github.com/gpumesh/gpumesh/internal/coord"
)

func main() {
	cfg := coord.ConfigFromEnv()

	// Ensure data directory exists.
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	srv, err := coord.NewServer(cfg)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
