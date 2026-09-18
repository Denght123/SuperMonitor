package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Denght123/SuperMonitor/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type dashboardService interface {
	Overview(rctx interface{ Done() <-chan struct{} }) (any, error)
}

type Dependencies struct {
	Dashboard *service.Dashboard
	Events    *service.EventHub
	Web       http.Handler
	Logger    *slog.Logger
	Version   string
}

func New(deps Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(securityHeaders)
	router.Use(recoverer(deps.Logger))
	router.Use(requestLogger(deps.Logger))

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": deps.Version})
	})

	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/overview", func(w http.ResponseWriter, r *http.Request) {
			overview, err := deps.Dashboard.Overview(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "overview_unavailable", "无法加载监控总览")
				return
			}
			writeJSON(w, http.StatusOK, overview)
		})
		api.Get("/providers", func(w http.ResponseWriter, r *http.Request) {
			providers, err := deps.Dashboard.Providers(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "providers_unavailable", "无法加载适配器状态")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": providers})
		})
		api.Post("/refresh", func(w http.ResponseWriter, r *http.Request) {
			if err := deps.Dashboard.Refresh(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "refresh_failed", "刷新任务执行失败")
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "completed", "message": "模拟额度已刷新"})
		})
		api.Get("/events", func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				writeError(w, http.StatusInternalServerError, "stream_unsupported", "服务器不支持事件流")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			channel, unsubscribe := deps.Events.Subscribe()
			defer unsubscribe()
			fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"live\"}\n\n")
			flusher.Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case payload := <-channel:
					fmt.Fprintf(w, "event: update\ndata: %s\n\n", payload)
					flusher.Flush()
				}
			}
		})
	})

	router.Handle("/*", deps.Web)
	return router
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
		})
	}
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered", "panic", recovered, "stack", string(debug.Stack()))
					writeError(w, http.StatusInternalServerError, "internal_error", "服务器发生内部错误")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
