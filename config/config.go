package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Vault  VaultConfig  `yaml:"vault"`
	Git    GitConfig    `yaml:"git"`
	Scan   ScanConfig   `yaml:"scan"`
	Ignore []string     `yaml:"ignore"`
	Auth   AuthConfig   `yaml:"auth"`
	Server ServerConfig `yaml:"server"`
}

type VaultConfig struct {
	Path string `yaml:"path"`
}

type GitConfig struct {
	AutocommitMinutes int    `yaml:"autocommit_minutes"`
	AccessToken       string `yaml:"access_token"`
	RepositoryURL     string `yaml:"repository_url"`
	CommitMessage     string `yaml:"commit_message"`
}

type ScanConfig struct {
	OnStartup        bool `yaml:"on_startup"`
	API              bool `yaml:"api"`
	OnChange         bool `yaml:"on_change"`
	PeriodicInterval int  `yaml:"periodic_interval"`
}

type AuthConfig struct {
	Token string `yaml:"token"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Bind string `yaml:"bind"`
}

// Load reads and parses the config file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if cfg.Vault.Path == "" {
		cfg.Vault.Path = "./vault"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Bind == "" {
		cfg.Server.Bind = "0.0.0.0"
	}
	// if cfg.Scan.PeriodicInterval == 0 {
	// 	cfg.Scan.PeriodicInterval = 300
	// }
	if cfg.Git.CommitMessage == "" {
		cfg.Git.CommitMessage = "Automatic SmartSyncServer commit"
	}

	return &cfg, nil
}

// IsAuthEnabled returns true if authentication is required
func (c *Config) IsAuthEnabled() bool {
	return c.Auth.Token != ""
}

// IsGitEnabled returns true if git integration is configured
func (c *Config) IsGitEnabled() bool {
	return c.Git.RepositoryURL != ""
}

// GetDefaultIgnorePatterns returns default ignore patterns if none specified
func (c *Config) GetDefaultIgnorePatterns() []string {
	// if len(c.Ignore) == 0 {
	// 	return []string{
	// 		".obsidian/workspace*",
	// 		".obsidian/cache",
	// 		".DS_Store",
	// 		".trash/**",
	// 		"*.tmp",
	// 		"*.swp",
	// 	}

	// }
	return c.Ignore
}
