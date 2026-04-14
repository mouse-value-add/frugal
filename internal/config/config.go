package config

import (
	"fmt"
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
	MaxContext           int     `yaml:"max_context"`
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Validate ensures the config has the minimum required structure for routing.
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("providers: at least one provider is required")
	}

	providerHasModel := false
	for providerName, pc := range c.Providers {
		if len(pc.Models) == 0 {
			continue
		}
		for modelName, mc := range pc.Models {
			providerHasModel = true
			if mc.CostPer1KInput < 0 || mc.CostPer1KOutput < 0 {
				return fmt.Errorf("providers.%s.models.%s: costs must be non-negative", providerName, modelName)
			}
			if mc.Capabilities.MaxContext <= 0 {
				return fmt.Errorf("providers.%s.models.%s: max_context must be > 0", providerName, modelName)
			}
			if err := validateUnitInterval(providerName, modelName, "reasoning", mc.Capabilities.Reasoning); err != nil {
				return err
			}
			if err := validateUnitInterval(providerName, modelName, "coding", mc.Capabilities.Coding); err != nil {
				return err
			}
			if err := validateUnitInterval(providerName, modelName, "creative", mc.Capabilities.Creative); err != nil {
				return err
			}
			if err := validateUnitInterval(providerName, modelName, "instruction_following", mc.Capabilities.InstructionFollowing); err != nil {
				return err
			}
		}
	}

	if !providerHasModel {
		return fmt.Errorf("providers: at least one model is required")
	}

	if len(c.QualityThresholds) == 0 {
		return fmt.Errorf("quality_thresholds: at least one threshold is required")
	}
	if _, ok := c.QualityThresholds["balanced"]; !ok {
		return fmt.Errorf("quality_thresholds.balanced: required")
	}

	for thresholdName, tc := range c.QualityThresholds {
		if err := validateThresholdUnitInterval(thresholdName, "min_reasoning", tc.MinReasoning); err != nil {
			return err
		}
		if err := validateThresholdUnitInterval(thresholdName, "min_coding", tc.MinCoding); err != nil {
			return err
		}
		if err := validateThresholdUnitInterval(thresholdName, "min_creative", tc.MinCreative); err != nil {
			return err
		}
		if err := validateThresholdUnitInterval(thresholdName, "min_instruction_following", tc.MinInstructionFollowing); err != nil {
			return err
		}
	}

	return nil
}

func validateUnitInterval(providerName, modelName, capability string, v float64) error {
	if v < 0 || v > 1 {
		return fmt.Errorf("providers.%s.models.%s.capabilities.%s: must be between 0 and 1", providerName, modelName, capability)
	}
	return nil
}

func validateThresholdUnitInterval(thresholdName, field string, v float64) error {
	if v < 0 || v > 1 {
		return fmt.Errorf("quality_thresholds.%s.%s: must be between 0 and 1", thresholdName, field)
	}
	return nil
}
