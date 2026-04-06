package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Server.Port != 7437 {
		t.Errorf("expected default port 7437, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Decay.HalfLifeDays != 30 {
		t.Errorf("expected half life 30, got %f", cfg.Decay.HalfLifeDays)
	}
	if cfg.Decay.MinImportance != 0.1 {
		t.Errorf("expected min importance 0.1, got %f", cfg.Decay.MinImportance)
	}
	if cfg.Decay.PruneInterval != "1h" {
		t.Errorf("expected prune interval 1h, got %s", cfg.Decay.PruneInterval)
	}
	if cfg.Turso.SyncInterval != "5m" {
		t.Errorf("expected sync interval 5m, got %s", cfg.Turso.SyncInterval)
	}
	if cfg.Extractor.Model != "glm-4.7" {
		t.Errorf("expected extractor model glm-4.7, got %s", cfg.Extractor.Model)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	data := map[string]interface{}{
		"server": map[string]interface{}{
			"host": "0.0.0.0",
			"port": 8080,
		},
	}

	b, _ := json.Marshal(data)
	if err := os.WriteFile(cfgPath, b, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	// Other fields should use defaults
	if cfg.Decay.HalfLifeDays != 30 {
		t.Errorf("expected default half life, got %f", cfg.Decay.HalfLifeDays)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("Load with missing file should not error, got: %v", err)
	}
	// Should return defaults
	if cfg.Server.Port != 7437 {
		t.Errorf("expected default port 7437, got %d", cfg.Server.Port)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with empty path should not error, got: %v", err)
	}
	if cfg.Server.Port != 7437 {
		t.Errorf("expected default port 7437, got %d", cfg.Server.Port)
	}
}

func TestEnvOverrides(t *testing.T) {
	// Save and restore environment
	restore := func(key, val string) {
		if val == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, val)
		}
	}

	origHost := os.Getenv("CLAWMEMORY_HOST")
	origPort := os.Getenv("CLAWMEMORY_PORT")
	origToken := os.Getenv("TURSO_AUTH_TOKEN")
	defer func() {
		restore("CLAWMEMORY_HOST", origHost)
		restore("CLAWMEMORY_PORT", origPort)
		restore("TURSO_AUTH_TOKEN", origToken)
	}()

	os.Setenv("CLAWMEMORY_HOST", "0.0.0.0")
	os.Setenv("CLAWMEMORY_PORT", "9000")
	os.Setenv("TURSO_AUTH_TOKEN", "test-token-123")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected env host override 0.0.0.0, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("expected env port override 9000, got %d", cfg.Server.Port)
	}
	if cfg.Turso.AuthToken != "test-token-123" {
		t.Errorf("expected env token override, got %s", cfg.Turso.AuthToken)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default config",
			mutate:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "invalid port 0",
			mutate:  func(c *Config) { c.Server.Port = 0 },
			wantErr: true,
		},
		{
			name:    "invalid port negative",
			mutate:  func(c *Config) { c.Server.Port = -1 },
			wantErr: true,
		},
		{
			name:    "invalid port too high",
			mutate:  func(c *Config) { c.Server.Port = 70000 },
			wantErr: true,
		},
		{
			name:    "empty host",
			mutate:  func(c *Config) { c.Server.Host = "" },
			wantErr: true,
		},
		{
			name:    "invalid half life days",
			mutate:  func(c *Config) { c.Decay.HalfLifeDays = -1 },
			wantErr: true,
		},
		{
			name:    "invalid min importance above 1",
			mutate:  func(c *Config) { c.Decay.MinImportance = 1.5 },
			wantErr: true,
		},
		{
			name:    "invalid min importance below 0",
			mutate:  func(c *Config) { c.Decay.MinImportance = -0.1 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(cfg)
			err := validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	if err := os.WriteFile(cfgPath, []byte("not-json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestEnvOverrides_AllFields(t *testing.T) {
	restore := func(key, val string) {
		if val == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, val)
		}
	}

	savedVars := map[string]string{}
	envVars := []string{
		"CLAWMEMORY_HOST", "CLAWMEMORY_PORT", "CLAWMEMORY_DB_PATH",
		"EXTRACTOR_BASE_URL",
		"EXTRACTOR_API_KEY", "EXTRACTOR_MODEL", "TURSO_URL", "TURSO_AUTH_TOKEN",
	}
	for _, k := range envVars {
		savedVars[k] = os.Getenv(k)
	}
	defer func() {
		for k, v := range savedVars {
			restore(k, v)
		}
	}()

	os.Setenv("CLAWMEMORY_HOST", "10.0.0.1")
	os.Setenv("CLAWMEMORY_PORT", "8888")
	os.Setenv("CLAWMEMORY_DB_PATH", "/tmp/test.db")
	os.Setenv("EXTRACTOR_BASE_URL", "http://proxy:8080")
	os.Setenv("EXTRACTOR_API_KEY", "secret")
	os.Setenv("EXTRACTOR_MODEL", "gpt-4")
	os.Setenv("TURSO_URL", "libsql://test.turso.io")
	os.Setenv("TURSO_AUTH_TOKEN", "mytoken")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Host != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 8888 {
		t.Errorf("expected 8888, got %d", cfg.Server.Port)
	}
	if cfg.Store.DBPath != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %s", cfg.Store.DBPath)
	}
	if cfg.Extractor.BaseURL != "http://proxy:8080" {
		t.Errorf("expected extractor URL, got %s", cfg.Extractor.BaseURL)
	}
	if cfg.Extractor.APIKey != "secret" {
		t.Errorf("expected secret, got %s", cfg.Extractor.APIKey)
	}
	if cfg.Extractor.Model != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", cfg.Extractor.Model)
	}
	if cfg.Turso.URL != "libsql://test.turso.io" {
		t.Errorf("expected turso URL, got %s", cfg.Turso.URL)
	}
	if cfg.Turso.AuthToken != "mytoken" {
		t.Errorf("expected mytoken, got %s", cfg.Turso.AuthToken)
	}
}

func TestLoadValidationError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	// Write config with invalid port
	data := map[string]interface{}{
		"server": map[string]interface{}{
			"port": -1,
		},
	}
	b, _ := json.Marshal(data)
	os.WriteFile(cfgPath, b, 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Error("expected validation error for invalid port")
	}
}
