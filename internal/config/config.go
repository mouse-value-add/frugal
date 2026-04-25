package config

import (
	"bytes"
	"fmt"
	"math"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Providers         map[string]ProviderConfig  `yaml:"providers"`
	QualityThresholds map[string]ThresholdConfig `yaml:"quality_thresholds"`
}

type ProviderConfig struct {
	APIKeyEnv string                 `yaml:"api_key_env"`
	BaseURL   string                 `yaml:"base_url"`
	Models    map[string]ModelConfig `yaml:"models"`
}

type ModelConfig struct {
	CostPer1KInput  float64          `yaml:"cost_per_1k_input"`
	CostPer1KOutput float64          `yaml:"cost_per_1k_output"`
	Capabilities    CapabilityConfig `yaml:"capabilities"`
}

type CapabilityConfig struct {
	Reasoning            float64 `yaml:"reasoning"`
	Coding               float64 `yaml:"coding"`
	Creative             float64 `yaml:"creative"`
	InstructionFollowing float64 `yaml:"instruction_following"`
	ToolUse              bool    `yaml:"tool_use"`
	JSONMode             bool    `yaml:"json_mode"`
	Vision               bool    `yaml:"vision"`
	MaxContext           int     `yaml:"max_context"`
	// Source names the benchmark suite these scores were derived from
	// (e.g. "livebench+aider"). AsOf is an ISO-8601 date string so
	// operators know when the scores were last refreshed. Routing
	// decisions are only as defensible as these fields — keep them current.
	Source string `yaml:"source,omitempty"`
	AsOf   string `yaml:"as_of,omitempty"`
}

type ThresholdConfig struct {
	MinReasoning            float64 `yaml:"min_reasoning"`
	MinCoding               float64 `yaml:"min_coding"`
	MinCreative             float64 `yaml:"min_creative"`
	MinInstructionFollowing float64 `yaml:"min_instruction_following"`
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
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("providers must contain at least one provider")
	}

	for providerName, provider := range cfg.Providers {
		if provider.APIKeyEnv == "" {
			return fmt.Errorf("providers.%s.api_key_env is required", providerName)
		}
		if len(provider.Models) == 0 {
			return fmt.Errorf("providers.%s.models must contain at least one model", providerName)
		}

		for modelName, model := range provider.Models {
			if !isFiniteNonNegative(model.CostPer1KInput) {
				return fmt.Errorf("providers.%s.models.%s.cost_per_1k_input must be a finite number >= 0", providerName, modelName)
			}
			if !isFiniteNonNegative(model.CostPer1KOutput) {
				return fmt.Errorf("providers.%s.models.%s.cost_per_1k_output must be a finite number >= 0", providerName, modelName)
			}
			if err := validateCapabilityRange(providerName, modelName, "reasoning", model.Capabilities.Reasoning); err != nil {
				return err
			}
			if err := validateCapabilityRange(providerName, modelName, "coding", model.Capabilities.Coding); err != nil {
				return err
			}
			if err := validateCapabilityRange(providerName, modelName, "creative", model.Capabilities.Creative); err != nil {
				return err
			}
			if err := validateCapabilityRange(providerName, modelName, "instruction_following", model.Capabilities.InstructionFollowing); err != nil {
				return err
			}
			if model.Capabilities.MaxContext < 0 {
				return fmt.Errorf("providers.%s.models.%s.capabilities.max_context must be >= 0", providerName, modelName)
			}
		}
	}

	if len(cfg.QualityThresholds) == 0 {
		return fmt.Errorf("quality_thresholds must contain at least one tier")
	}

	for tier, threshold := range cfg.QualityThresholds {
		if err := validateThresholdRange(tier, "min_reasoning", threshold.MinReasoning); err != nil {
			return err
		}
		if err := validateThresholdRange(tier, "min_coding", threshold.MinCoding); err != nil {
			return err
		}
		if err := validateThresholdRange(tier, "min_creative", threshold.MinCreative); err != nil {
			return err
		}
		if err := validateThresholdRange(tier, "min_instruction_following", threshold.MinInstructionFollowing); err != nil {
			return err
		}
	}

	return nil
}

func isFiniteNonNegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

func isFiniteProbability(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}

func validateCapabilityRange(providerName, modelName, field string, value float64) error {
	if !isFiniteProbability(value) {
		return fmt.Errorf("providers.%s.models.%s.capabilities.%s must be between 0 and 1", providerName, modelName, field)
	}
	return nil
}

func validateThresholdRange(tier, field string, value float64) error {
	if !isFiniteProbability(value) {
		return fmt.Errorf("quality_thresholds.%s.%s must be between 0 and 1", tier, field)
	}
	return nil
}
