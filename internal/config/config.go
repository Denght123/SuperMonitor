package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Listen      string
	DataDir     string
	Environment string
}

func Load() (Config, error) {
	cfg := Config{
		Listen:      envOr("SUPMON_LISTEN", "127.0.0.1:8080"),
		DataDir:     envOr("SUPMON_DATA_DIR", "./data"),
		Environment: envOr("SUPMON_ENVIRONMENT", "local"),
	}
	if strings.TrimSpace(cfg.Listen) == "" {
		return Config{}, fmt.Errorf("SUPMON_LISTEN must not be empty")
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return Config{}, fmt.Errorf("SUPMON_DATA_DIR must not be empty")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
