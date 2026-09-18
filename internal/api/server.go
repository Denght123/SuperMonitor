package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/integration/genericquota"
	"github.com/Denght123/SuperMonitor/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type dashboardService interface {
	Overview(rctx interface{ Done() <-chan struct{} }) (any, error)
}

type Dependencies struct {
	Dashboard *service.Dashboard
	Accounts  *service.Accounts
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
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "completed", "message": "全部额度已刷新"})
		})
		api.Post("/providers/{providerID}/accounts/import", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			providerID := chi.URLParam(r, "providerID")
			r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
			if err := r.ParseMultipartForm(1024 * 1024); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_upload", "认证文件无效或超过 1 MB")
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				writeError(w, http.StatusBadRequest, "file_required", "请选择认证文件")
				return
			}
			defer file.Close()
			body, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
			if err != nil || len(body) > 1024*1024 {
				writeError(w, http.StatusBadRequest, "invalid_upload", "认证文件读取失败或超过 1 MB")
				return
			}
			var account any
			switch providerID {
			case "codex":
				account, err = deps.Accounts.ImportCodex(r.Context(), r.FormValue("alias"), body)
			case "workbuddy-cn", "workbuddy-global":
				account, err = deps.Accounts.ImportWorkBuddy(r.Context(), providerID, r.FormValue("alias"), body)
			default:
				writeError(w, http.StatusBadRequest, "import_unsupported", "该平台不支持认证文件导入")
				return
			}
			if err != nil {
				writeError(w, providerErrorStatus(err), "credential_import_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, account)
		})
		api.Post("/providers/{providerID}/accounts/secret", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			var payload struct {
				Alias  string `json:"alias"`
				Secret string `json:"secret"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_payload", "请求内容无效")
				return
			}
			var account any
			var err error
			switch chi.URLParam(r, "providerID") {
			case "deepseek":
				account, err = deps.Accounts.ConnectDeepSeek(r.Context(), payload.Alias, payload.Secret)
			case "mimo":
				account, err = deps.Accounts.ConnectMimo(r.Context(), payload.Alias, payload.Secret)
			default:
				writeError(w, http.StatusBadRequest, "secret_unsupported", "该平台不支持密钥方式连接")
				return
			}
			if err != nil {
				writeError(w, providerErrorStatus(err), "secret_connection_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, account)
		})
		api.Post("/providers/{providerID}/accounts/custom", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			var payload struct {
				Alias string `json:"alias"`
				genericquota.Credential
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&payload); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_payload", "请求内容无效")
				return
			}
			account, err := deps.Accounts.ConnectGeneric(r.Context(), chi.URLParam(r, "providerID"), payload.Alias, payload.Credential)
			if err != nil {
				writeError(w, providerErrorStatus(err), "custom_connection_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, account)
		})
		api.Post("/providers/{providerID}/oauth", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			var payload struct {
				Alias string `json:"alias"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&payload)
			}
			providerID := chi.URLParam(r, "providerID")
			if providerID != "workbuddy-cn" && providerID != "workbuddy-global" {
				writeError(w, http.StatusBadRequest, "oauth_unsupported", "该平台暂不支持此 OAuth 流程")
				return
			}
			session, err := deps.Accounts.StartWorkBuddyOAuth(r.Context(), providerID, payload.Alias)
			if err != nil {
				writeError(w, http.StatusBadGateway, "oauth_start_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, session)
		})
		api.Get("/providers/{providerID}/oauth/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
			session, ok := deps.Accounts.DeviceLoginStatus(chi.URLParam(r, "sessionID"))
			if !ok || session.Provider != chi.URLParam(r, "providerID") {
				writeError(w, http.StatusNotFound, "oauth_session_not_found", "登录会话不存在或已过期")
				return
			}
			writeJSON(w, http.StatusOK, session)
		})
		api.Post("/providers/codex/accounts/import", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
			if err := r.ParseMultipartForm(1024 * 1024); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_upload", "OAuth 文件无效或超过 1 MB")
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				writeError(w, http.StatusBadRequest, "file_required", "请选择 OAuth JSON 文件")
				return
			}
			defer file.Close()
			body, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
			if err != nil || len(body) > 1024*1024 {
				writeError(w, http.StatusBadRequest, "invalid_upload", "OAuth 文件读取失败或超过 1 MB")
				return
			}
			account, err := deps.Accounts.ImportCodex(r.Context(), r.FormValue("alias"), body)
			if err != nil {
				writeError(w, http.StatusBadGateway, "codex_import_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, account)
		})
		api.Post("/providers/codex/device-login", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			var payload struct {
				Alias string `json:"alias"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&payload)
			}
			session, err := deps.Accounts.StartCodexDeviceLogin(r.Context(), payload.Alias)
			if err != nil {
				writeError(w, http.StatusBadGateway, "device_login_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, session)
		})
		api.Get("/providers/codex/device-login/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
			session, ok := deps.Accounts.DeviceLoginStatus(chi.URLParam(r, "sessionID"))
			if !ok {
				writeError(w, http.StatusNotFound, "device_session_not_found", "登录会话不存在或已过期")
				return
			}
			writeJSON(w, http.StatusOK, session)
		})
		api.Post("/accounts/{accountID}/refresh", func(w http.ResponseWriter, r *http.Request) {
			if deps.Accounts == nil {
				writeError(w, http.StatusServiceUnavailable, "accounts_unavailable", "账号服务未启用")
				return
			}
			account, err := deps.Accounts.RefreshOne(r.Context(), chi.URLParam(r, "accountID"))
			if err != nil {
				writeError(w, http.StatusBadGateway, "account_refresh_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, account)
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

func providerErrorStatus(err error) int {
	message := err.Error()
	for _, marker := range []string{"为空", "无效", "缺少", "不是有效", "已失效", "不支持", "不符", "找不到", "无法解析"} {
		if strings.Contains(message, marker) {
			return http.StatusBadRequest
		}
	}
	return http.StatusBadGateway
}
