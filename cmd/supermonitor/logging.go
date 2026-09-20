package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const maxLogFileSize int64 = 10 * 1024 * 1024

var (
	bearerPattern  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	secretPattern  = regexp.MustCompile(`(?i)(access[_-]?token|refresh[_-]?token|api[_-]?key|authorization|cookie|secret|auth[_-]?code)(["'=:\s]+)([^\s,;"'}]+)`)
	sessionPattern = regexp.MustCompile(`\bsess_[A-Za-z0-9_-]{8,}`)
	webhookPattern = regexp.MustCompile(`(?i)(/open-apis/bot/v2/hook/)[^\s?"']+`)
)

// newLogger keeps structured logs visible in the process console and, by
// default, appends scrubbed records to a bounded rolling file in the data
// directory. Passing "-" is an explicit opt-out for supervisors that already
// persist stdout.
func newLogger(dataDir, logPath string) (*slog.Logger, io.Closer, error) {
	logPath = strings.TrimSpace(logPath)
	handlerOptions := &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: redactLogAttr}
	if logPath == "-" {
		return slog.New(slog.NewJSONHandler(os.Stdout, handlerOptions)), nil, nil
	}
	if logPath == "" {
		logPath = filepath.Join(dataDir, "supermonitor.log")
	} else if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(dataDir, logPath)
	}
	if err := validateLogPath(dataDir, logPath); err != nil {
		return nil, nil, err
	}
	file, err := openRollingLog(logPath, maxLogFileSize)
	if err != nil {
		return nil, nil, err
	}
	writer := io.MultiWriter(os.Stdout, file)
	return slog.New(slog.NewJSONHandler(writer, handlerOptions)), file, nil
}

func validateLogPath(dataDir, logPath string) error {
	absoluteLog, err := filepath.Abs(logPath)
	if err != nil {
		return fmt.Errorf("resolve log file: %w", err)
	}
	for _, protected := range []string{"supermonitor.db", "supermonitor.db-wal", "supermonitor.db-shm", "credential.key"} {
		absoluteProtected, resolveErr := filepath.Abs(filepath.Join(dataDir, protected))
		if resolveErr != nil {
			return fmt.Errorf("resolve protected data file: %w", resolveErr)
		}
		if strings.EqualFold(filepath.Clean(absoluteLog), filepath.Clean(absoluteProtected)) {
			return fmt.Errorf("SUPMON_LOG_FILE must not overwrite %s", protected)
		}
	}
	return nil
}

type rollingLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

func openRollingLog(path string, maxBytes int64) (*rollingLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}
	return &rollingLog{path: path, maxBytes: maxBytes, file: file, size: info.Size()}, nil
}

func (w *rollingLog) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.maxBytes > 0 && w.size > 0 && w.size+int64(len(payload)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := w.file.Write(payload)
	w.size += int64(written)
	return written, err
}

func (w *rollingLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rollingLog) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close log for rotation: %w", err)
	}
	backup := w.path + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous log backup: %w", err)
	}
	if err := os.Rename(w.path, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate log file: %w", err)
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open rotated log file: %w", err)
	}
	w.file = file
	w.size = 0
	return nil
}

func redactLogAttr(_ []string, attribute slog.Attr) slog.Attr {
	attribute.Value = attribute.Value.Resolve()
	if sensitiveLogKey(attribute.Key) {
		attribute.Value = slog.StringValue("[redacted]")
		return attribute
	}
	switch attribute.Value.Kind() {
	case slog.KindString:
		attribute.Value = slog.StringValue(scrubSensitiveText(attribute.Value.String()))
	case slog.KindAny:
		if err, ok := attribute.Value.Any().(error); ok {
			attribute.Value = slog.StringValue(scrubSensitiveText(err.Error()))
		}
	}
	return attribute
}

func sensitiveLogKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, marker := range []string{"token", "secret", "cookie", "authorization", "credential", "password", "auth_code", "api_key", "webhook"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func scrubSensitiveText(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer [redacted]")
	value = secretPattern.ReplaceAllString(value, "$1$2[redacted]")
	value = sessionPattern.ReplaceAllString(value, "sess_[redacted]")
	return webhookPattern.ReplaceAllString(value, "$1[redacted]")
}
