package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Config struct {
	Listen              string
	DataDir             string
	Environment         string
	LogFile             string
	AdminToken          string
	SyncTimeout         time.Duration
	AllowInsecureRemote bool
}

func Load() (Config, error) {
	syncTimeout, err := time.ParseDuration(strings.TrimSpace(envOr("SUPMON_SYNC_TIMEOUT", "10m")))
	if err != nil || syncTimeout < time.Minute {
		return Config{}, fmt.Errorf("SUPMON_SYNC_TIMEOUT must be a duration of at least 1m")
	}
	allowInsecureRemote := false
	if raw := strings.TrimSpace(envOr("SUPMON_ALLOW_INSECURE_REMOTE", "false")); raw != "" {
		allowInsecureRemote, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SUPMON_ALLOW_INSECURE_REMOTE must be true or false")
		}
	}
	cfg := Config{
		Listen:              envOr("SUPMON_LISTEN", "127.0.0.1:8080"),
		DataDir:             envOr("SUPMON_DATA_DIR", "./data"),
		Environment:         envOr("SUPMON_ENVIRONMENT", "local"),
		LogFile:             envOr("SUPMON_LOG_FILE", ""),
		AdminToken:          envOr("SUPMON_ADMIN_TOKEN", ""),
		SyncTimeout:         syncTimeout,
		AllowInsecureRemote: allowInsecureRemote,
	}
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	if cfg.Listen == "" {
		return Config{}, fmt.Errorf("SUPMON_LISTEN must not be empty")
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return Config{}, fmt.Errorf("SUPMON_DATA_DIR must not be empty")
	}
	cfg.AdminToken = strings.TrimSpace(cfg.AdminToken)
	cfg.LogFile = strings.TrimSpace(cfg.LogFile)
	if cfg.AdminToken != "" && (!utf8.ValidString(cfg.AdminToken) || utf8.RuneCountInString(cfg.AdminToken) < 24) {
		return Config{}, fmt.Errorf("SUPMON_ADMIN_TOKEN must contain at least 24 Unicode characters")
	}
	loopback, err := isLoopbackListenAddress(cfg.Listen)
	if err != nil {
		return Config{}, err
	}
	if !loopback && cfg.AdminToken == "" && !cfg.AllowInsecureRemote {
		return Config{}, fmt.Errorf("SUPMON_ADMIN_TOKEN is required for non-loopback SUPMON_LISTEN; set SUPMON_ALLOW_INSECURE_REMOTE=true only for an explicitly isolated deployment")
	}
	return cfg, nil
}

func isLoopbackListenAddress(address string) (bool, error) {
	host, port, err := net.SplitHostPort(address)
	portNumber, portErr := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return false, fmt.Errorf("SUPMON_LISTEN must be a valid host:port address")
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback(), nil
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
