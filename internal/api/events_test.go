package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Denght123/SuperMonitor/internal/service"
)

func TestEventStreamSurvivesServerWriteTimeout(t *testing.T) {
	hub := service.NewEventHub()
	handler := New(Dependencies{
		Events:  hub,
		Web:     http.NotFoundHandler(),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version: "test",
	})

	const writeTimeout = 40 * time.Millisecond
	server := httptest.NewUnstartedServer(handler)
	server.Config.WriteTimeout = writeTimeout
	server.Start()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("unexpected content type %q", got)
	}

	reader := bufio.NewReader(response.Body)
	connected, err := readEventStreamFrame(reader)
	if err != nil {
		t.Fatalf("read connected event: %v", err)
	}
	if !strings.Contains(connected, "event: connected") {
		t.Fatalf("unexpected connected frame %q", connected)
	}

	// Let the original server-level write deadline expire before publishing.
	// The update must still arrive because the SSE handler clears that deadline.
	time.Sleep(3 * writeTimeout)
	hub.Publish(service.Event{Type: "quota.updated", Message: "额度已更新", Timestamp: time.Now().UTC()})
	update, err := readEventStreamFrame(reader)
	if err != nil {
		t.Fatalf("read event after server write timeout: %v", err)
	}
	if !strings.Contains(update, "event: update") || !strings.Contains(update, `"type":"quota.updated"`) {
		t.Fatalf("unexpected update frame %q", update)
	}
}

func TestEventStreamHeartbeatAndWriteFailure(t *testing.T) {
	if eventStreamHeartbeatInterval < 15*time.Second || eventStreamHeartbeatInterval > 20*time.Second {
		t.Fatalf("heartbeat interval %s is outside the required 15-20 second range", eventStreamHeartbeatInterval)
	}

	hub := service.NewEventHub()
	writer := newEventStreamTestWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		serveEventStream(writer, request, hub, 10*time.Millisecond)
		close(done)
	}()

	select {
	case deadline := <-writer.deadlines:
		if !deadline.IsZero() {
			t.Fatalf("expected the SSE write deadline to be cleared, got %s", deadline)
		}
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not clear the write deadline")
	}

	deadline := time.After(time.Second)
	for !strings.Contains(writer.bodyString(), ": heartbeat\n\n") {
		select {
		case <-writer.writes:
		case <-deadline:
			t.Fatal("SSE handler did not send a heartbeat comment")
		}
	}

	writer.failWrites()
	hub.Publish(service.Event{Type: "test", Message: "force write", Timestamp: time.Now().UTC()})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not exit after a write failure")
	}
}

func readEventStreamFrame(reader *bufio.Reader) (string, error) {
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if line == "\n" || line == "\r\n" {
			return frame.String(), nil
		}
		frame.WriteString(line)
	}
}

type eventStreamTestWriter struct {
	mu        sync.Mutex
	header    http.Header
	body      strings.Builder
	fail      bool
	writes    chan struct{}
	deadlines chan time.Time
}

func newEventStreamTestWriter() *eventStreamTestWriter {
	return &eventStreamTestWriter{
		header:    make(http.Header),
		writes:    make(chan struct{}, 16),
		deadlines: make(chan time.Time, 1),
	}
}

func (w *eventStreamTestWriter) Header() http.Header {
	return w.header
}

func (w *eventStreamTestWriter) WriteHeader(_ int) {}

func (w *eventStreamTestWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errors.New("forced SSE write failure")
	}
	written, err := w.body.Write(payload)
	select {
	case w.writes <- struct{}{}:
	default:
	}
	return written, err
}

func (w *eventStreamTestWriter) Flush() {}

func (w *eventStreamTestWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines <- deadline
	return nil
}

func (w *eventStreamTestWriter) bodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func (w *eventStreamTestWriter) failWrites() {
	w.mu.Lock()
	w.fail = true
	w.mu.Unlock()
}
