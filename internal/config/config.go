package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort   string
	DBPath    string
	UploadDir string
	LogLevel  string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	cfg := Config{
		AppPort:   getEnv("APP_PORT", "8080"),
		DBPath:    getEnv("DB_PATH", "./data/clothing-store.db"),
		UploadDir: strings.TrimSpace(getEnv("UPLOAD_DIR", "./uploads")),
		LogLevel:  strings.ToLower(getEnv("LOG_LEVEL", "info")),
	}
	port, err := strconv.Atoi(cfg.AppPort)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("APP_PORT must be a number from 1 to 65535")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return Config{}, fmt.Errorf("DB_PATH cannot be empty")
	}
	if cfg.UploadDir == "" {
		return Config{}, fmt.Errorf("UPLOAD_DIR cannot be empty")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
