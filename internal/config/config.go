// Package config loads Frugal's runtime configuration from models.yaml.
//
// v1.0 ships only the routed search-tool layer; chat-model pricing /
// capability scores moved out of the binary when the recipe layer was
// cut. They'll come back in Phase 2 with the frugal__chat MCP tool.
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk model.yaml decoded.
type Config struct {
	SearchProviders map[string]SearchProviderConfig `yaml:"search_providers,omitempty"`
}

// SearchProviderConfig describes a routed search backend (Tavily, Serper,
// SearXNG, …). The frugal__search MCP tool registers one entry per
// provider whose credentials/endpoint are present at startup; the
// auto-router picks the lowest CostPerCall among those.
//
// Providers split into two shapes:
//
//   - Hosted APIs (Tavily, Serper): need an API key. APIKeyEnv is set; the
//     driver registers only if that env var is non-empty.
//   - Self-hosted backends (SearXNG): no API key. APIKeyEnv is empty;
//     BaseURLEnv (or BaseURL) supplies the endpoint the operator stood up.
type SearchProviderConfig struct {
	APIKeyEnv   string  `yaml:"api_key_env,omitempty"`
	BaseURL     string  `yaml:"base_url,omitempty"`
	BaseURLEnv  string  `yaml:"base_url_env,omitempty"`
	CostPerCall float64 `yaml:"cost_per_call"`
}

// Load reads the config from the given path, or from FRUGAL_CONFIG env var.
func Load(path string) (*Config, error) {
	if envPath := os.Getenv("FRUGAL_CONFIG"); envPath != "" {
		path = envPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	for name, sp := range cfg.SearchProviders {
		if sp.CostPerCall < 0 {
			return fmt.Errorf("search_providers.%s.cost_per_call must be non-negative", name)
		}
		// Either an API key env (hosted API) or a base URL (self-hosted) is
		// required — without one we have no way to dispatch a call.
		if sp.APIKeyEnv == "" && sp.BaseURL == "" && sp.BaseURLEnv == "" {
			return fmt.Errorf("search_providers.%s: set api_key_env (hosted) or base_url / base_url_env (self-hosted)", name)
		}
	}
	return nil
}
