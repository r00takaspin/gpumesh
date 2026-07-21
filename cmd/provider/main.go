package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/gpumesh/gpumesh/internal/provider"
)
func main() {
	var cfg provider.Config

	flag.StringVar(&cfg.CoordinatorURL, "coordinator", envOrDefault("MESH_COORDINATOR", "wss://gpumesh.io/ws/provider"), "coordinator WebSocket URL")
	flag.StringVar(&cfg.Token, "token", os.Getenv("MESH_TOKEN"), "donor authentication token")
	flag.StringVar(&cfg.OllamaURL, "ollama-url", envOrDefault("MESH_OLLAMA_URL", "http://localhost:11434"), "Ollama base URL")
	flag.String("models", os.Getenv("MESH_MODELS"), "comma-separated model whitelist")
	flag.StringVar(&cfg.Description, "description", envOrDefault("MESH_DESCRIPTION", hostname()), "public donor description")
	flag.IntVar(&cfg.MaxConcurrent, "max-concurrent", envOrDefaultInt("MESH_MAX_CONCURRENT", 1), "max concurrent requests")

	flag.Parse()

	// Parse models whitelist.
	if modelsFlag := os.Getenv("MESH_MODELS"); modelsFlag != "" {
		cfg.Models = strings.Split(modelsFlag, ",")
		for i := range cfg.Models {
			cfg.Models[i] = strings.TrimSpace(cfg.Models[i])
		}
	}

	// Also check --models flag (though we registered it above, flag parsing fills it).
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "models" {
			cfg.Models = strings.Split(f.Value.String(), ",")
			for i := range cfg.Models {
				cfg.Models[i] = strings.TrimSpace(cfg.Models[i])
			}
		}
	})

	if cfg.Token == "" {
		log.Fatal("No token. Get one at https://gpumesh.io/dashboard")
	}

	agent := provider.NewAgent(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Printf("starting provider agent, coordinator=%s", cfg.CoordinatorURL)
	if err := agent.Run(ctx); err != nil {
		log.Fatalf("agent: %v", err)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
