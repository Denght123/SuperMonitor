package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Denght123/SuperMonitor/internal/service"
)

const eventStreamHeartbeatInterval = 15 * time.Second

func serveEventStream(w http.ResponseWriter, r *http.Request, events *service.EventHub, heartbeatInterval time.Duration) {
	if _, ok := w.(http.Flusher); !ok {
		writeError(w, http.StatusInternalServerError, "stream_unsupported", "服务器不支持事件流")
		return
	}

	controller := http.NewResponseController(w)
	// An SSE response is intentionally long-lived. Clear the per-response write
	// deadline inherited from http.Server.WriteTimeout so later events are not
	// discarded when the connection has been open longer than that timeout.
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "无法建立事件流")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	channel, unsubscribe := events.Subscribe()
	defer unsubscribe()

	if err := writeEventStreamFrame(w, controller, "event: connected\ndata: {\"status\":\"live\"}\n\n"); err != nil {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload, ok := <-channel:
			if !ok {
				return
			}
			if err := writeEventStreamFrame(w, controller, fmt.Sprintf("event: update\ndata: %s\n\n", payload)); err != nil {
				return
			}
		case <-heartbeat.C:
			// A comment is ignored by EventSource clients while keeping idle
			// connections alive through browsers and reverse proxies.
			if err := writeEventStreamFrame(w, controller, ": heartbeat\n\n"); err != nil {
				return
			}
		}
	}
}

func writeEventStreamFrame(w io.Writer, controller *http.ResponseController, frame string) error {
	if _, err := io.WriteString(w, frame); err != nil {
		return err
	}
	return controller.Flush()
}
