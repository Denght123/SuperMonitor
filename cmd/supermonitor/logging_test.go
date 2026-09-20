package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLoggerPersistsJSONLogInDataDirectory(t *testing.T) {
	dataDir := t.TempDir()
	logger, closer, err := newLogger(dataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if closer == nil {
		t.Fatal("expected persistent log closer")
	}
	logger.Info("test persisted log", "component", "test")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dataDir, "supermonitor.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"msg":"test persisted log"`) || !strings.Contains(text, `"component":"test"`) {
		t.Fatalf("unexpected log contents: %s", text)
	}
}

func TestNewLoggerAllowsExplicitStdoutOnlyMode(t *testing.T) {
	_, closer, err := newLogger(t.TempDir(), "-")
	if err != nil {
		t.Fatal(err)
	}
	if closer != nil {
		t.Fatal("stdout-only logging must not return a file closer")
	}
}

func TestRollingLogRotatesAndRetainsOneBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rolling.log")
	writer, err := openRollingLog(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("first-record-is-long-enough\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("second-record-rotates\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "second-record") || !strings.Contains(string(backup), "first-record") {
		t.Fatalf("unexpected rotated logs: current=%q backup=%q", current, backup)
	}
}

func TestNewLoggerRejectsCredentialAndDatabasePaths(t *testing.T) {
	dataDir := t.TempDir()
	for _, path := range []string{"credential.key", "supermonitor.db"} {
		if _, _, err := newLogger(dataDir, path); err == nil {
			t.Fatalf("expected protected path %q to be rejected", path)
		}
	}
}

func TestLogRedactionScrubsCommonCredentialShapes(t *testing.T) {
	attribute := redactLogAttr(nil, slog.Any("error", errors.New("Authorization: Bearer abc.def access_token=secret sess_123456789 https://open.feishu.cn/open-apis/bot/v2/hook/private-token")))
	text := attribute.Value.String()
	for _, secret := range []string{"abc.def", "=secret", "sess_123456789", "private-token"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log text leaked %q: %s", secret, text)
		}
	}
	if got := redactLogAttr(nil, slog.String("api_key", "plain-secret")).Value.String(); got != "[redacted]" {
		t.Fatalf("sensitive log attribute was not redacted: %q", got)
	}
}
