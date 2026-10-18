package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_PORT", "8080")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("UPLOAD_DIR", "./uploads")
	t.Setenv("LOG_LEVEL", "info")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.AppPort != "8080" || cfg.DBPath != "./data/clothing-store.db" || cfg.UploadDir != "./uploads" || cfg.LogLevel != "info" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("APP_PORT", "invalid")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("UPLOAD_DIR", "./uploads")
	t.Setenv("LOG_LEVEL", "info")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for invalid port")
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("APP_PORT", "8080")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("UPLOAD_DIR", "./uploads")
	t.Setenv("LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for invalid log level")
	}
}

func TestLoadRejectsEmptyUploadDirectory(t *testing.T) {
	t.Setenv("APP_PORT", "8080")
	t.Setenv("DB_PATH", "./data/clothing-store.db")
	t.Setenv("UPLOAD_DIR", "  ")
	t.Setenv("LOG_LEVEL", "info")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected an error for empty UPLOAD_DIR")
	}
}
