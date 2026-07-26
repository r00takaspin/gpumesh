package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunWizard runs the first-run interactive configuration wizard.
// It guides the user through Ollama URL setup, model selection, and token entry.
func RunWizard(stdin io.Reader, stdout io.Writer, cfg *Config) error {
	scanner := bufio.NewScanner(stdin)

	// 1. Ollama check.
	fmt.Fprintf(stdout, "Checking Ollama at %s...\n", cfg.OllamaURL)
	models, err := DiscoverModelsWithURL(cfg.OllamaURL)
	if err != nil {
		fmt.Fprintf(stdout, "Ollama not reachable at %s\n", cfg.OllamaURL)
		for range 3 {
			fmt.Fprintf(stdout, "Enter Ollama URL [http://localhost:11434]: ")
			if !scanner.Scan() {
				return fmt.Errorf("input interrupted")
			}
			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				input = "http://localhost:11434"
			}
			models, err = DiscoverModelsWithURL(input)
			if err == nil {
				cfg.OllamaURL = input
				break
			}
			fmt.Fprintf(stdout, "Still not reachable: %v\n", err)
		}
		if err != nil {
			return fmt.Errorf("could not reach Ollama after 3 attempts")
		}
	}

	// 2. Model selection.
	fmt.Fprintf(stdout, "\nFound %d model(s):\n", len(models))
	for i, m := range models {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, m)
	}

	fmt.Fprintf(stdout, "\nShare all models? [Y/n]: ")
	if !scanner.Scan() {
		return fmt.Errorf("input interrupted")
	}
	shareAll := strings.TrimSpace(scanner.Text())
	if shareAll == "" || strings.EqualFold(shareAll, "y") || strings.EqualFold(shareAll, "yes") {
		cfg.Models = models
	} else {
		fmt.Fprintf(stdout, "Enter numbers to share (e.g. 1,3,5) or 'all': ")
		if !scanner.Scan() {
			return fmt.Errorf("input interrupted")
		}
		selection := strings.TrimSpace(scanner.Text())
		if strings.EqualFold(selection, "all") {
			cfg.Models = models
		} else {
			for _, s := range strings.Split(selection, ",") {
				s = strings.TrimSpace(s)
				idx, err := strconv.Atoi(s)
				if err != nil || idx < 1 || idx > len(models) {
					fmt.Fprintf(stdout, "Invalid selection: %s\n", s)
					continue
				}
				cfg.Models = append(cfg.Models, models[idx-1])
			}
			if len(cfg.Models) == 0 {
				fmt.Fprintf(stdout, "No valid models selected. Sharing all models.\n")
				cfg.Models = models
			}
		}
	}

	// 3. Token check.
	if cfg.Token == "" {
		fmt.Fprintf(stdout, "\nNo token configured. Enter token [or press Enter to skip]: ")
		if !scanner.Scan() {
			return fmt.Errorf("input interrupted")
		}
		token := strings.TrimSpace(scanner.Text())
		if token != "" {
			cfg.Token = token
		}
	}

	// 3.5. Coordinator URL.
	fmt.Fprintf(stdout, "\nCoordinator URL [%s]: ", cfg.CoordinatorURL)
	if !scanner.Scan() {
		return fmt.Errorf("input interrupted")
	}
	if u := strings.TrimSpace(scanner.Text()); u != "" {
		// Auto-detect scheme if missing.
		if !strings.HasPrefix(u, "ws://") && !strings.HasPrefix(u, "wss://") {
			u = "wss://" + u
		}
		cfg.CoordinatorURL = u
	}

	// 4. Summary and confirmation.
	fmt.Fprintf(stdout, "\n--- Configuration Summary ---\n")
	fmt.Fprintf(stdout, "Coordinator: %s\n", cfg.CoordinatorURL)
	fmt.Fprintf(stdout, "Ollama URL:  %s\n", cfg.OllamaURL)
	fmt.Fprintf(stdout, "Models:      %v\n", cfg.Models)
	fmt.Fprintf(stdout, "Token:       %s\n", maskToken(cfg.Token))
	fmt.Fprintf(stdout, "Max concurrent: %d\n", cfg.MaxConcurrent)
	fmt.Fprintf(stdout, "Description: %s\n", cfg.Description)
	fmt.Fprintf(stdout, "-----------------------------\n")
	fmt.Fprintf(stdout, "\nProceed with this configuration? [Y/n]: ")
	if !scanner.Scan() {
		return fmt.Errorf("input interrupted")
	}
	proceed := strings.TrimSpace(scanner.Text())
	if proceed != "" && !strings.EqualFold(proceed, "y") && !strings.EqualFold(proceed, "yes") {
		fmt.Fprintf(stdout, "Configuration cancelled. Re-run to try again.\n")
		os.Exit(0)
	}

	// Save config.
	configPath := ConfigFilePath()
	if err := SaveConfig(configPath, *cfg); err != nil {
		fmt.Fprintf(stdout, "Warning: could not save config: %v\n", err)
	} else {
		fmt.Fprintf(stdout, "\nConfiguration saved to %s\n", configPath)
	}

	return nil
}

// DiscoverModelsWithURL fetches available models from a specific Ollama URL.
func DiscoverModelsWithURL(ollamaURL string) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(ollamaURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama /api/tags: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse /api/tags: %w", err)
	}

	var models []string
	for _, m := range result.Models {
		models = append(models, m.Name)
	}
	return models, nil
}

// maskToken returns a masked version of the token for display.
func maskToken(token string) string {
	if token == "" {
		return "(none)"
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}
