package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)
// BackendType indicates which API the backend server speaks.
type BackendType int

const (
	BackendOllama BackendType = iota
	BackendOpenAI
)

// discoverTimeout is the timeout for model discovery requests.
const discoverTimeout = 5 * time.Second
var modelDiscoveryClient = &http.Client{Timeout: discoverTimeout}

// ollamaTagsResponse is the response from Ollama's /api/tags endpoint.
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// openaiModelsResponse is the response from OpenAI-compatible /v1/models endpoint.
type openaiModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// FetchModels discovers available models from a backend server.
// FetchModels discovers available models and detects the backend API type.
// Tries Ollama's /api/tags first, falls back to OpenAI-compatible /v1/models.
func FetchModels(baseURL string) ([]string, BackendType, error) {
	models, err := fetchOllamaTags(baseURL)
	if err == nil {
		return models, BackendOllama, nil
	}

	models, openaiErr := fetchOpenAIModels(baseURL)
	if openaiErr != nil {
		return nil, BackendOllama, fmt.Errorf("model discovery failed: ollama /api/tags: %w; openai /v1/models: %w", err, openaiErr)
	}
	return models, BackendOpenAI, nil
}

func fetchOllamaTags(baseURL string) ([]string, error) {
	resp, err := modelDiscoveryClient.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama /api/tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama /api/tags: HTTP %d", resp.StatusCode)
	}

	var result ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse /api/tags: %w", err)
	}

	var models []string
	for _, m := range result.Models {
		models = append(models, m.Name)
	}
	return models, nil
}

func fetchOpenAIModels(baseURL string) ([]string, error) {
	resp, err := modelDiscoveryClient.Get(baseURL + "/v1/models")
	if err != nil {
		return nil, fmt.Errorf("openai /v1/models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai /v1/models: HTTP %d", resp.StatusCode)
	}

	var result openaiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse /v1/models: %w", err)
	}

	var models []string
	for _, m := range result.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

