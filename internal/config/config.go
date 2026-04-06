// Package config provides configuration loading for ClawMemory.
// It supports JSON/YAML config files and environment variable overrides.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all ClawMemory configuration.
type Config struct {
	Server    ServerConfig    `json:"server"`
	Store     StoreConfig     `json:"store"`
	Extractor ExtractorConfig `json:"extractor"`
	Decay     DecayConfig     `json:"decay"`
	Turso     TursoConfig     `json:"turso"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Host string `json:"host"` // default "127.0.0.1"
	Port int    `json:"port"` // default 7437
}

// StoreConfig holds SQLite store configuration.
type StoreConfig struct {
	DBPath string `json:"db_path"` // default "~/.clawmemory/memory.db"
}

// ExtractorConfig holds LLM fact extraction configuration.
type ExtractorConfig struct {
	BaseURL   string `json:"base_url"`   // LLM proxy base URL
	Model     string `json:"model"`      // e.g. "glm-4.7"
	APIKey    string `json:"api_key"`    // proxy API key
	APIFormat string `json:"api_format"` // "openai" (default) or "anthropic" — auto-detected from base_url if empty
}

// DecayConfig holds importance decay configuration.
type DecayConfig struct {
	HalfLifeDays  float64 `json:"half_life_days"`  // default 30
	MinImportance float64 `json:"min_importance"`  // default 0.1 — below this, auto-prune
	PruneInterval string  `json:"prune_interval"`  // default "1h"
}

// TursoConfig holds Turso cloud sync configuration.
type TursoConfig struct {
	URL          string `json:"url"`           // "libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io"
	AuthToken    string `json:"auth_token"`    // from env: TURSO_AUTH_TOKEN
	SyncInterval string `json:"sync_interval"` // default "5m"
}

// Default returns a Config with all defaults filled in.
func Default() *Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dbPath := filepath.Join(home, ".clawmemory", "memory.db")

	return &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 7437,
		},
		Store: StoreConfig{
			DBPath: dbPath,
		},
		Extractor: ExtractorConfig{
			Model: "glm-4.7",
		},
		Decay: DecayConfig{
			HalfLifeDays:  30,
			MinImportance: 0.1,
			PruneInterval: "1h",
		},
		Turso: TursoConfig{
			URL:          "libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io",
			SyncInterval: "5m",
		},
	}
}

// Load reads config from path, falls back to defaults, then applies env overrides.
// If path is empty or the file doesn't exist, defaults are used.
func Load(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config file %s: %w", path, err)
			}
			// File doesn't exist — use defaults
		} else {
			if err := json.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parsing config file %s: %w", path, err)
			}
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Validate
	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CLAWMEMORY_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("CLAWMEMORY_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("CLAWMEMORY_DB_PATH"); v != "" {
		cfg.Store.DBPath = v
	}
	if v := os.Getenv("EXTRACTOR_BASE_URL"); v != "" {
		cfg.Extractor.BaseURL = v
	}
	if v := os.Getenv("EXTRACTOR_API_KEY"); v != "" {
		cfg.Extractor.APIKey = v
	}
	if v := os.Getenv("EXTRACTOR_MODEL"); v != "" {
		cfg.Extractor.Model = v
	}
	if v := os.Getenv("TURSO_URL"); v != "" {
		cfg.Turso.URL = v
	}
	if v := os.Getenv("TURSO_AUTH_TOKEN"); v != "" {
		cfg.Turso.AuthToken = v
	}
}

// validate checks that the config has valid values.
func validate(cfg *Config) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", cfg.Server.Port)
	}
	if strings.TrimSpace(cfg.Server.Host) == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if cfg.Decay.HalfLifeDays <= 0 {
		return fmt.Errorf("decay.half_life_days must be positive, got %f", cfg.Decay.HalfLifeDays)
	}
	if cfg.Decay.MinImportance < 0 || cfg.Decay.MinImportance > 1 {
		return fmt.Errorf("decay.min_importance must be between 0 and 1, got %f", cfg.Decay.MinImportance)
	}
	return nil
}
