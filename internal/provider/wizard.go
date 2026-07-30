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

const (
	wzReset  = "\033[0m"
	wzBold   = "\033[1m"
	wzDim    = "\033[2m"
	wzGreen  = "\033[32m"
	wzYellow = "\033[33m"
	wzBlue   = "\033[36m"
	wzRed    = "\033[31m"
)

// RunWizard runs the first-run interactive configuration wizard.
// It guides the user through Ollama URL setup, model selection, and token entry.
func RunWizard(stdin io.Reader, stdout io.Writer, cfg *Config) error {
	scanner := bufio.NewScanner(stdin)

	// Header.
	_, _ = fmt.Fprint(stdout, wzBold+wzGreen+`
   ┌──────────────────────────────────┐
   │     GPU MESH — setup wizard      │
   └──────────────────────────────────┘`+wzReset+`

`+wzDim+`   configure your provider agent`+wzReset+`

`)

	// 1. Ollama check.
	_, _ = fmt.Fprintf(stdout, wzGreen+"⬡"+wzReset+" Checking Ollama at "+wzDim+"%s"+wzReset+"...\n", cfg.OllamaURL)
	models, err := DiscoverModelsWithURL(cfg.OllamaURL)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, wzRed+"✗"+wzReset+" Ollama not reachable at "+wzDim+"%s"+wzReset+"\n", cfg.OllamaURL)
		for range 3 {
			prompt(stdout, "Enter Ollama URL", "http://localhost:11434")
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
			_, _ = fmt.Fprintf(stdout, wzRed+"✗"+wzReset+" Still not reachable: "+wzDim+"%v"+wzReset+"\n", err)
		}
		if err != nil {
			return fmt.Errorf("could not reach Ollama after 3 attempts")
		}
	}

	// 2. Model selection.
	_, _ = fmt.Fprintf(stdout, "\n"+wzGreen+"⬡"+wzReset+" Found "+wzBold+"%d"+wzReset+" model(s):\n", len(models))
	for i, m := range models {
		_, _ = fmt.Fprintf(stdout, "  "+wzDim+"%d."+wzReset+" %s\n", i+1, m)
	}

	prompt(stdout, "Share all models", "Y")
	if !scanner.Scan() {
		return fmt.Errorf("input interrupted")
	}
	shareAll := strings.TrimSpace(scanner.Text())
	if shareAll == "" || strings.EqualFold(shareAll, "y") || strings.EqualFold(shareAll, "yes") {
		cfg.Models = models
	} else {
		prompt(stdout, "Enter numbers to share (e.g. 1,3,5) or 'all'", "all")
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
					_, _ = fmt.Fprintf(stdout, wzYellow+"!"+wzReset+" Invalid selection: "+wzDim+"%s"+wzReset+"\n", s)
					continue
				}
				cfg.Models = append(cfg.Models, models[idx-1])
			}
			if len(cfg.Models) == 0 {
				_, _ = fmt.Fprintf(stdout, wzYellow+"!"+wzReset+" No valid models selected. Sharing all models.\n")
				cfg.Models = models
			}
		}
	}

	// 3. Coordinator URL.
	_, _ = fmt.Fprintf(stdout, "\n"+wzBlue+"⌬"+wzReset+" Coordinator URL ["+wzDim+"%s"+wzReset+"]: ", cfg.CoordinatorURL)
	if !scanner.Scan() {
		return fmt.Errorf("input interrupted")
	}
	if u := strings.TrimSpace(scanner.Text()); u != "" {
		if !strings.HasPrefix(u, "ws://") && !strings.HasPrefix(u, "wss://") {
			u = "wss://" + u
		}
		cfg.CoordinatorURL = u
	}

	// 4. Token.
	if cfg.Token == "" {
		_, _ = fmt.Fprintf(stdout, "\n"+wzYellow+"⚡"+wzReset+" Provider token from /share ["+wzDim+"optional"+wzReset+"]: ")
		if !scanner.Scan() {
			return fmt.Errorf("input interrupted")
		}
		token := strings.TrimSpace(scanner.Text())
		if token != "" {
			cfg.Token = token
		}
	}

	// 5. Summary.
	_, _ = fmt.Fprint(stdout, `
`+wzBold+`--- Configuration Summary ---`+wzReset+`
`+wzBlue+`⌬`+wzReset+` Coordinator: `+wzDim+cfg.CoordinatorURL+wzReset+`
`+wzGreen+`⬡`+wzReset+` Ollama URL:  `+wzDim+cfg.OllamaURL+wzReset+`
`+wzGreen+`⬡`+wzReset+` Models:      `+wzBold+modelsSummary(cfg.Models)+wzReset+`
`+wzYellow+`⚡`+wzReset+` Token:       `+wzDim+maskToken(cfg.Token)+wzReset+`
`+wzYellow+`⚡`+wzReset+` Concurrent:  `+wzDim+fmt.Sprintf("%d", cfg.MaxConcurrent)+wzReset+`
`+wzYellow+`⚡`+wzReset+` Description: `+wzDim+cfg.Description+wzReset+`
`)

	prompt(stdout, "Proceed with this configuration", "Y")
	if !scanner.Scan() {
		return fmt.Errorf("input interrupted")
	}
	proceed := strings.TrimSpace(scanner.Text())
	if proceed != "" && !strings.EqualFold(proceed, "y") && !strings.EqualFold(proceed, "yes") {
		_, _ = fmt.Fprintf(stdout, wzYellow+"!"+wzReset+" Configuration cancelled. Re-run to try again.\n")
		os.Exit(0)
	}

	// Save config.
	configPath := ConfigFilePath()
	if err := SaveConfig(configPath, *cfg); err != nil {
		_, _ = fmt.Fprintf(stdout, wzRed+"✗"+wzReset+" Could not save config: %v\n", err)
	} else {
		_, _ = fmt.Fprintf(stdout, "\n"+wzGreen+"✓"+wzReset+" Configuration saved to "+wzDim+"%s"+wzReset+"\n\n", configPath)
	}

	return nil
}

// prompt prints a styled prompt with a default value hint.
func prompt(w io.Writer, label, def string) {
	_, _ = fmt.Fprintf(w, "\n"+wzBold+"→"+wzReset+" %s ["+wzDim+"%s"+wzReset+"]: ", label, def)
}

// modelsSummary formats the model list for display.
func modelsSummary(models []string) string {
	if len(models) == 0 {
		return "(none)"
	}
	return strings.Join(models, ", ")
}

// DiscoverModelsWithURL fetches available models from a specific Ollama URL.
func DiscoverModelsWithURL(ollamaURL string) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(ollamaURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama /api/tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
