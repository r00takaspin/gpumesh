package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/r00takaspin/gpumesh/internal/provider"
)

func main() {
	// All CLI flags start empty/zero — precedence resolved explicitly below.
	var (
		coordinatorFlag string
		tokenFlag       string
		ollamaFlag      string
		modelsFlag      string
		descFlag        string
		maxConcFlag     int
		wizardFlag      bool
		noWizardFlag    bool
		configFlag      string
	)
	flag.StringVar(&coordinatorFlag, "coordinator", "", "coordinator WebSocket URL")
	flag.StringVar(&tokenFlag, "token", "", "donor authentication token")
	flag.StringVar(&ollamaFlag, "ollama-url", "", "Ollama base URL")
	flag.StringVar(&modelsFlag, "models", "", "comma-separated model whitelist (default: auto-discover)")
	flag.StringVar(&descFlag, "description", "", "public donor description")
	flag.IntVar(&maxConcFlag, "max-concurrent", 0, "max concurrent requests")
	flag.BoolVar(&wizardFlag, "wizard", false, "force interactive wizard")
	flag.BoolVar(&noWizardFlag, "no-wizard", false, "skip wizard even if config incomplete")
	flag.StringVar(&configFlag, "config", "", "config file path")
	flag.Parse()

	// Track explicitly set flags for Layer 3 override.
	visited := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { visited[f.Name] = true })

	// Layer 0: Load config file.
	configPath := configFlag
	if configPath == "" {
		configPath = provider.ConfigFilePath()
	}
	cfg, err := provider.LoadConfig(configPath)
	if err != nil {
		log.Printf("Warning: could not load config from %s: %v", configPath, err)
	}

	// Layer 1: Apply hardcoded defaults for any remaining zero values.
	applyDefaults(&cfg)

	// Layer 2: Override with environment variables.
	applyEnv(&cfg, modelsFlag)

	// Layer 3: Override with explicitly set CLI flags.
	if visited["coordinator"] {
		cfg.CoordinatorURL = coordinatorFlag
	}
	if visited["token"] {
		cfg.Token = tokenFlag
	}
	if visited["ollama-url"] {
		cfg.OllamaURL = ollamaFlag
	}
	if visited["models"] {
		cfg.Models = parseModels(modelsFlag)
	}
	if visited["max-concurrent"] {
		cfg.MaxConcurrent = maxConcFlag
	}
	if visited["description"] {
		cfg.Description = descFlag
	}

	// Wizard trigger.
	if wizardFlag && !noWizardFlag {
		if err := provider.RunWizard(os.Stdin, os.Stdout, &cfg); err != nil {
			log.Fatalf("wizard: %v", err)
		}
	} else if !noWizardFlag {
		needWizard := cfg.Token == ""
		if !needWizard {
			_, err := provider.DiscoverModelsWithURL(cfg.OllamaURL)
			needWizard = err != nil
		}
		if needWizard {
			if err := provider.RunWizard(os.Stdin, os.Stdout, &cfg); err != nil {
				log.Fatalf("wizard: %v", err)
			}
		}
	}

	if cfg.Token == "" {
		log.Fatal("No token. Get one at https://gpumesh.net/dashboard")
	}

	agent := provider.NewAgent(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	printBanner(cfg)

	log.Printf("starting provider agent, coordinator=%s", cfg.CoordinatorURL)
	if err := agent.Run(ctx); err != nil {
		log.Fatalf("agent: %v", err)
	}
}

func applyDefaults(cfg *provider.Config) {
	if cfg.CoordinatorURL == "" {
		cfg.CoordinatorURL = "wss://gpumesh.net/ws/provider"
	}
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = "http://localhost:11434"
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.Description == "" {
		cfg.Description = hostname()
	}
}

func applyEnv(cfg *provider.Config, modelsFlag string) {
	if v := os.Getenv("MESH_COORDINATOR"); v != "" {
		cfg.CoordinatorURL = v
	}
	if v := os.Getenv("MESH_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("MESH_OLLAMA_URL"); v != "" {
		cfg.OllamaURL = v
	}
	if v := os.Getenv("MESH_MODELS"); v != "" {
		cfg.Models = parseModels(v)
	}
	if v := os.Getenv("MESH_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrent = n
		}
	}
	if v := os.Getenv("MESH_DESCRIPTION"); v != "" {
		cfg.Description = v
	}
}

func parseModels(s string) []string {
	var models []string
	for _, m := range strings.Split(s, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			models = append(models, m)
		}
	}
	return models
}

// --- Display helpers ---

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[36m"
)

func printBanner(cfg provider.Config) {
	host, _ := os.Hostname()
	banner := bold + green + `
   ┌──────────────────────────────────┐
   │     GPU MESH — donor agent       │
   └──────────────────────────────────┘` + reset + `

` + dim + `   peer-to-peer LLM inference mesh` + reset + `

` + yellow + `⚡` + reset + ` agent:    ` + bold + cfg.Description + reset + `
` + blue + `⌬` + reset + ` endpoint: ` + dim + cfg.CoordinatorURL + reset + `
` + green + `⬡` + reset + ` ollama:   ` + dim + cfg.OllamaURL + reset + `
` + yellow + `⚡` + reset + ` host:     ` + dim + host + reset + `
`
	fmt.Print(banner)
}

func hostname() string {
	h, _ := os.Hostname()
	if model := darwinModel(); model != "" {
		return model
	}
	return h
}

func darwinModel() string {
	out, err := exec.Command("sysctl", "-n", "hw.model").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
