package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ConfigFilePath returns the path to the provider config file.
// Checks MESH_CONFIG env var, then ~/.gpumesh.json, falls back to ./gpumesh.json.
func ConfigFilePath() string {
	if p := os.Getenv("MESH_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".gpumesh.json")
	}
	return "./gpumesh.json"
}

// LoadConfig loads configuration from a JSON file.
// Returns zero Config and nil error if the file does not exist.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SaveConfig persists configuration to a JSON file atomically.
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
