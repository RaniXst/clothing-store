package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_PORT", "8080")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("LOG_LEVEL", "info")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.AppPort != "8080" {
		t.Errorf("AppPort = %q, want %q", cfg.AppPort, "8080")
	}

	if cfg.DBPath != "./data/clothing-store.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "./data/clothing-store.db")
	}

	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("APP_PORT", "invalid")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("LOG_LEVEL", "info")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected an error for invalid port")
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("APP_PORT", "8080")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected an error for invalid log level")
	}
}
