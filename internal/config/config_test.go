package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAllowsLoopbackWithoutAdministratorPassword(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		t.Run(listen, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv("SUPMON_LISTEN", listen)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Listen != listen {
				t.Fatalf("Listen = %q, want %q", cfg.Listen, listen)
			}
			if cfg.AdminPassword != "" {
				t.Fatalf("AdminPassword = %q, want empty", cfg.AdminPassword)
			}
		})
	}
}

func TestLoadRejectsRemoteListenWithoutAdministratorPassword(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:8080", "192.168.1.20:8080"} {
		t.Run(listen, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv("SUPMON_LISTEN", listen)

			_, err := Load()
			assertErrorContains(t, err, "SUPMON_ADMIN_PASSWORD is required")
		})
	}
}

func TestLoadAllowsProtectedOrExplicitlyIsolatedRemoteListen(t *testing.T) {
	tests := []struct {
		name          string
		listen        string
		adminPassword string
		allowInsecure string
	}{
		{
			name:          "strong administrator password",
			listen:        "0.0.0.0:8080",
			adminPassword: strings.Repeat("强", 12),
		},
		{
			name:          "explicit insecure remote opt-in",
			listen:        "192.168.1.20:8080",
			allowInsecure: "true",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv("SUPMON_LISTEN", test.listen)
			t.Setenv("SUPMON_ADMIN_PASSWORD", test.adminPassword)
			t.Setenv("SUPMON_ALLOW_INSECURE_REMOTE", test.allowInsecure)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.AllowInsecureRemote != (test.allowInsecure == "true") {
				t.Fatalf("AllowInsecureRemote = %t", cfg.AllowInsecureRemote)
			}
		})
	}
}

func TestLoadRejectsInvalidSecurityAndTimeoutSettings(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       string
		wantMessage string
	}{
		{
			name:        "weak administrator password",
			key:         "SUPMON_ADMIN_PASSWORD",
			value:       strings.Repeat("a", 11),
			wantMessage: "at least 12 Unicode characters",
		},
		{
			name:        "invalid insecure remote boolean",
			key:         "SUPMON_ALLOW_INSECURE_REMOTE",
			value:       "sometimes",
			wantMessage: "must be true or false",
		},
		{
			name:        "invalid listen address",
			key:         "SUPMON_LISTEN",
			value:       "127.0.0.1",
			wantMessage: "valid host:port address",
		},
		{
			name:        "invalid listen port",
			key:         "SUPMON_LISTEN",
			value:       "127.0.0.1:not-a-port",
			wantMessage: "valid host:port address",
		},
		{
			name:        "out of range listen port",
			key:         "SUPMON_LISTEN",
			value:       "127.0.0.1:70000",
			wantMessage: "valid host:port address",
		},
		{
			name:        "invalid synchronization timeout",
			key:         "SUPMON_SYNC_TIMEOUT",
			value:       "not-a-duration",
			wantMessage: "duration of at least 1m",
		},
		{
			name:        "too short synchronization timeout",
			key:         "SUPMON_SYNC_TIMEOUT",
			value:       "59s",
			wantMessage: "duration of at least 1m",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(test.key, test.value)

			_, err := Load()
			assertErrorContains(t, err, test.wantMessage)
		})
	}
}

func TestLoadUsesSynchronizationTimeout(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("SUPMON_SYNC_TIMEOUT", "7m30s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SyncTimeout != 7*time.Minute+30*time.Second {
		t.Fatalf("SyncTimeout = %s, want 7m30s", cfg.SyncTimeout)
	}
}

func setValidEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("SUPMON_LISTEN", "127.0.0.1:8080")
	t.Setenv("SUPMON_DATA_DIR", `G:\SuperMonitor\.tmp\config-test-data`)
	t.Setenv("SUPMON_ENVIRONMENT", "test")
	t.Setenv("SUPMON_LOG_FILE", "")
	t.Setenv("SUPMON_ADMIN_PASSWORD", "")
	t.Setenv("SUPMON_ADMIN_TOKEN", "")
	t.Setenv("SUPMON_SYNC_TIMEOUT", "10m")
	t.Setenv("SUPMON_ALLOW_INSECURE_REMOTE", "false")
}

func TestLoadUsesLegacyAdministratorTokenWhenPasswordIsUnset(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("SUPMON_ADMIN_TOKEN", "legacy-administrator-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AdminPassword != "legacy-administrator-secret" {
		t.Fatalf("AdminPassword = %q", cfg.AdminPassword)
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Load() error = nil, want message containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Load() error = %q, want message containing %q", err, want)
	}
}
