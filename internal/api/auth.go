package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	adminSessionCookie     = "supermonitor_admin"
	adminSessionTTL        = 12 * time.Hour
	loginFailureWindow     = 5 * time.Minute
	loginLockoutDuration   = 5 * time.Minute
	loginMaxFailures       = 5
	maxTrackedLoginSources = 4096
	kiroOAuthCallbackPath  = "/api/v1/providers/kiro/oauth/callback"
)

type authenticationKind uint8

const (
	authenticationDenied authenticationKind = iota
	authenticationBearer
	authenticationSession
	authenticationOriginRejected
)

type adminSession struct {
	expiresAt time.Time
}

type loginFailureState struct {
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
	failures     int
}

type adminAuth struct {
	required    bool
	tokenDigest [sha256.Size]byte

	mu            sync.Mutex
	sessions      map[[sha256.Size]byte]adminSession
	loginFailures map[string]loginFailureState
	now           func() time.Time
	random        io.Reader
}

func newAdminAuth(token string) *adminAuth {
	token = strings.TrimSpace(token)
	auth := &adminAuth{
		required:      token != "",
		sessions:      make(map[[sha256.Size]byte]adminSession),
		loginFailures: make(map[string]loginFailureState),
		now:           time.Now,
		random:        rand.Reader,
	}
	if auth.required {
		auth.tokenDigest = sha256.Sum256([]byte(token))
	}
	return auth
}

func (a *adminAuth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.requiresAuthentication(r) {
			next.ServeHTTP(w, r)
			return
		}

		switch a.authenticateRequest(r) {
		case authenticationBearer, authenticationSession:
			next.ServeHTTP(w, r)
		case authenticationOriginRejected:
			writeError(w, http.StatusForbidden, "invalid_request_origin", "Cookie 会话仅允许来自当前控制台的同源写入请求")
		default:
			writeError(w, http.StatusUnauthorized, "authentication_required", "需要管理员令牌才能访问此控制台")
		}
	})
}

func (a *adminAuth) requiresAuthentication(r *http.Request) bool {
	if !a.required || !isAPIPath(r.URL.Path) {
		return false
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/status" {
		return false
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/session" {
		return false
	}
	// The Kiro authorization server cannot send a SameSite=Strict session cookie
	// on its cross-site redirect. This exact callback remains safe to expose
	// because completion requires the one-time high-entropy state and PKCE
	// verifier retained by the server-side Kiro client.
	return !(r.Method == http.MethodGet && r.URL.Path == kiroOAuthCallbackPath)
}

func isAPIPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func (a *adminAuth) authenticateRequest(r *http.Request) authenticationKind {
	if !a.required {
		return authenticationSession
	}
	if a.validBearer(r.Header.Get("Authorization")) {
		return authenticationBearer
	}
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || !a.validSession(cookie.Value) {
		return authenticationDenied
	}
	if !isSafeHTTPMethod(r.Method) && !hasSameOrigin(r) {
		return authenticationOriginRejected
	}
	return authenticationSession
}

func (a *adminAuth) validBearer(header string) bool {
	header = strings.TrimSpace(header)
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return subtle.ConstantTimeCompare(digest[:], a.tokenDigest[:]) == 1
}

func (a *adminAuth) validSession(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	digest := sha256.Sum256([]byte(value))
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	session, ok := a.sessions[digest]
	if !ok {
		return false
	}
	if !now.Before(session.expiresAt) {
		delete(a.sessions, digest)
		return false
	}
	return true
}

func (a *adminAuth) authenticated(r *http.Request) bool {
	kind := a.authenticateRequest(r)
	return kind == authenticationBearer || kind == authenticationSession
}

func (a *adminAuth) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"required":      a.required,
		"authenticated": a.authenticated(r),
	})
}

func (a *adminAuth) login(w http.ResponseWriter, r *http.Request) {
	if !a.required {
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
		return
	}

	source := loginSource(r)
	if allowed, retryAfter := a.loginAllowed(source); !allowed {
		w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds(retryAfter), 10))
		writeError(w, http.StatusTooManyRequests, "login_rate_limited", "管理员令牌连续验证失败次数过多，请稍后再试")
		return
	}

	var payload struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024))
	if err := decoder.Decode(&payload); err != nil {
		a.recordLoginFailure(source)
		writeError(w, http.StatusBadRequest, "invalid_payload", "请输入管理员令牌")
		return
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(payload.Token)))
	if subtle.ConstantTimeCompare(digest[:], a.tokenDigest[:]) != 1 {
		a.recordLoginFailure(source)
		writeError(w, http.StatusUnauthorized, "invalid_admin_token", "管理员令牌不正确")
		return
	}

	sessionID, expiresAt, err := a.createSession()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_creation_failed", "无法创建管理员会话，请重试")
		return
	}
	a.clearLoginFailures(source)
	http.SetCookie(w, &http.Cookie{
		Name: adminSessionCookie, Value: sessionID, Path: "/api/v1", HttpOnly: true,
		Secure: requestIsSecure(r), SameSite: http.SameSiteStrictMode,
		MaxAge: int(adminSessionTTL.Seconds()), Expires: expiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *adminAuth) createSession() (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(a.random, raw); err != nil {
		return "", time.Time{}, err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(value))
	now := a.now()
	expiresAt := now.Add(adminSessionTTL)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneSessionsLocked(now)
	a.sessions[digest] = adminSession{expiresAt: expiresAt}
	return value, expiresAt, nil
}

func (a *adminAuth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminSessionCookie); err == nil && cookie.Value != "" {
		digest := sha256.Sum256([]byte(cookie.Value))
		a.mu.Lock()
		delete(a.sessions, digest)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminSessionCookie, Value: "", Path: "/api/v1", HttpOnly: true,
		Secure: requestIsSecure(r), SameSite: http.SameSiteStrictMode,
		MaxAge: -1, Expires: time.Unix(1, 0).UTC(),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (a *adminAuth) pruneSessionsLocked(now time.Time) {
	for digest, session := range a.sessions {
		if !now.Before(session.expiresAt) {
			delete(a.sessions, digest)
		}
	}
}

func (a *adminAuth) loginAllowed(source string) (bool, time.Duration) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.loginFailures[source]
	if !ok {
		return true, 0
	}
	if now.Before(state.blockedUntil) {
		return false, state.blockedUntil.Sub(now)
	}
	if !state.blockedUntil.IsZero() || now.Before(state.windowStart) || now.Sub(state.windowStart) >= loginFailureWindow {
		delete(a.loginFailures, source)
	}
	return true, 0
}

func (a *adminAuth) recordLoginFailure(source string) {
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.loginFailures[source]
	if !ok || now.Before(state.windowStart) || now.Sub(state.windowStart) >= loginFailureWindow {
		state = loginFailureState{windowStart: now}
	}
	state.failures++
	state.lastSeen = now
	if state.failures >= loginMaxFailures {
		state.blockedUntil = now.Add(loginLockoutDuration)
	}
	a.loginFailures[source] = state
	a.limitLoginFailureEntriesLocked(now)
}

func (a *adminAuth) clearLoginFailures(source string) {
	a.mu.Lock()
	delete(a.loginFailures, source)
	a.mu.Unlock()
}

func (a *adminAuth) limitLoginFailureEntriesLocked(now time.Time) {
	if len(a.loginFailures) <= maxTrackedLoginSources {
		return
	}
	for source, state := range a.loginFailures {
		if (!state.blockedUntil.IsZero() && !now.Before(state.blockedUntil)) ||
			(state.blockedUntil.IsZero() && now.Sub(state.lastSeen) >= loginFailureWindow) {
			delete(a.loginFailures, source)
		}
	}
	for len(a.loginFailures) > maxTrackedLoginSources {
		var oldestSource string
		var oldest time.Time
		for source, state := range a.loginFailures {
			if oldestSource == "" || state.lastSeen.Before(oldest) {
				oldestSource, oldest = source, state.lastSeen
			}
		}
		delete(a.loginFailures, oldestSource)
	}
}

func loginSource(r *http.Request) string {
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if ip := net.ParseIP(remote); ip != nil {
		return ip.String()
	}
	if remote == "" {
		return "unknown"
	}
	return strings.ToLower(remote)
}

func retryAfterSeconds(duration time.Duration) int64 {
	seconds := int64((duration + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func isSafeHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func hasSameOrigin(r *http.Request) bool {
	rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if rawOrigin == "" || strings.EqualFold(rawOrigin, "null") {
		return false
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.User != nil || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	actual, ok := canonicalOrigin(origin.Scheme, origin.Host)
	if !ok {
		return false
	}
	expected, ok := canonicalOrigin(requestScheme(r), requestHost(r))
	return ok && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func canonicalOrigin(scheme, hostPort string) (string, bool) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	parsed, err := url.Parse(scheme + "://" + hostPort)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + net.JoinHostPort(host, port), true
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, true
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if strings.EqualFold(forwarded, "https") {
		return "https"
	}
	return "http"
}

func requestHost(r *http.Request) string {
	forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if forwarded != "" {
		return forwarded
	}
	return r.Host
}

func requestIsSecure(r *http.Request) bool {
	return requestScheme(r) == "https"
}
