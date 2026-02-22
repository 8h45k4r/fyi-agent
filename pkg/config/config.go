// Package config provides configuration loading and validation for the FYI Agent.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for the FYI Agent.
type Config struct {
	Agent     AgentConfig     `yaml:"agent"`
	Transport TransportConfig `yaml:"transport"`
	Steering  SteeringConfig  `yaml:"steering"`
	DLP       DLPConfig       `yaml:"dlp"`
	Captive   CaptiveConfig   `yaml:"captive"`
}

// AgentConfig holds agent identity and logging settings.
type AgentConfig struct {
	Name     string `yaml:"name"`
	LogLevel string `yaml:"log_level"`
	TenantID string `yaml:"tenant_id"`
}

// TransportConfig holds zero-trust transport settings.
type TransportConfig struct {
	ControllerURL string        `yaml:"controller_url"`
	IdentityFile  string        `yaml:"identity_file"`
	Timeout       time.Duration `yaml:"timeout"`
}

// SteeringConfig holds traffic steering and PAC settings.
type SteeringConfig struct {
	PACURL      string   `yaml:"pac_url"`
	BypassList  []string `yaml:"bypass_list"`
	SplitTunnel bool     `yaml:"split_tunnel"`
}

// DLPConfig holds data loss prevention settings.
type DLPConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Patterns []string `yaml:"patterns"`
}

// CaptiveConfig holds captive portal detection settings.
type CaptiveConfig struct {
	ProbeURL string        `yaml:"probe_url"`
	Interval time.Duration `yaml:"interval"`
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	return Parse(data)
}

// Parse unmarshals YAML bytes into a Config struct.
func Parse(data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("config data cannot be empty")
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// Validate checks that required fields are set and values are sensible.
func (c *Config) Validate() error {
	if c.Agent.TenantID == "" {
		return fmt.Errorf("agent.tenant_id is required")
	}
	if c.Transport.ControllerURL == "" {
		return fmt.Errorf("transport.controller_url is required")
	}
	return nil
}
