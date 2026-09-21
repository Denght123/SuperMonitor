package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testAdminPassword = "a-strong-administrator-password"

func TestAdminAuthProtectsAPIAndCreatesRandomHttpOnlySession(t *testing.T) {
	auth := newAdminAuth(testAdminPassword)
	protected := auth.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	protected.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://console.example/api/v1/overview", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	cookie := loginCookie(t, auth, testAdminPassword, "192.0.2.10:41000")
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Value == testAdminPassword {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	if cookie.MaxAge != int(adminSessionTTL.Seconds()) || cookie.Expires.IsZero() {
		t.Fatalf("session cookie lifetime not set: %+v", cookie)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "http://console.example/api/v1/overview", nil)
	authorizedRequest.AddCookie(cookie)
	authorized := httptest.NewRecorder()
	protected.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", authorized.Code)
	}
}

func TestAdminSessionsAreUniqueExpireAndDoNotSurviveAuthRestart(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	auth := newAdminAuth(testAdminPassword)
	auth.now = func() time.Time { return now }
	first := loginCookie(t, auth, testAdminPassword, "192.0.2.10:41000")
	second := loginCookie(t, auth, testAdminPassword, "192.0.2.10:41001")
	if first.Value == second.Value {
		t.Fatal("separate logins reused a session identifier")
	}

	request := httptest.NewRequest(http.MethodGet, "http://console.example/api/v1/overview", nil)
	request.AddCookie(first)
	if got := auth.authenticateRequest(request); got != authenticationSession {
		t.Fatalf("live session authentication = %v", got)
	}

	restarted := newAdminAuth(testAdminPassword)
	if got := restarted.authenticateRequest(request); got != authenticationDenied {
		t.Fatalf("session survived auth restart: %v", got)
	}
	rotated := newAdminAuth("a-different-random-administrator-token")
	if got := rotated.authenticateRequest(request); got != authenticationDenied {
		t.Fatalf("session survived password rotation: %v", got)
	}

	now = now.Add(adminSessionTTL)
	if got := auth.authenticateRequest(request); got != authenticationDenied {
		t.Fatalf("expired session authentication = %v", got)
	}
}

func TestAdminLogoutRevokesCurrentSession(t *testing.T) {
	auth := newAdminAuth(testAdminPassword)
	cookie := loginCookie(t, auth, testAdminPassword, "192.0.2.10:41000")
	handler := auth.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/session" {
			auth.logout(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	logout := httptest.NewRequest(http.MethodDelete, "http://console.example/api/v1/auth/session", nil)
	logout.Header.Set("Origin", "http://console.example")
	logout.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, logout)
	if response.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", response.Code, response.Body.String())
	}
	cleared := response.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear cookie: %+v", cleared)
	}

	request := httptest.NewRequest(http.MethodGet, "http://console.example/api/v1/overview", nil)
	request.AddCookie(cookie)
	afterLogout := httptest.NewRecorder()
	handler.ServeHTTP(afterLogout, request)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d", afterLogout.Code)
	}
}

func TestCookieWritesRequireSameOriginButBearerDoesNot(t *testing.T) {
	auth := newAdminAuth(testAdminPassword)
	cookie := loginCookie(t, auth, testAdminPassword, "192.0.2.10:41000")
	handler := auth.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for name, origin := range map[string]string{
		"missing": "",
		"foreign": "https://attacker.example",
		"null":    "null",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://console.example/api/v1/refresh", nil)
			request.Header.Set("Origin", origin)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}

	sameOrigin := httptest.NewRequest(http.MethodPost, "http://internal-proxy/api/v1/refresh", nil)
	sameOrigin.Host = "internal-proxy"
	sameOrigin.Header.Set("Origin", "https://monitor.example")
	sameOrigin.Header.Set("X-Forwarded-Proto", "https")
	sameOrigin.Header.Set("X-Forwarded-Host", "monitor.example:443")
	sameOrigin.AddCookie(cookie)
	sameOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(sameOriginResponse, sameOrigin)
	if sameOriginResponse.Code != http.StatusNoContent {
		t.Fatalf("same-origin status = %d body=%s", sameOriginResponse.Code, sameOriginResponse.Body.String())
	}

	bearer := httptest.NewRequest(http.MethodDelete, "http://console.example/api/v1/accounts/example", nil)
	bearer.Header.Set("Authorization", "Bearer "+testAdminPassword)
	bearerResponse := httptest.NewRecorder()
	handler.ServeHTTP(bearerResponse, bearer)
	if bearerResponse.Code != http.StatusNoContent {
		t.Fatalf("bearer status = %d body=%s", bearerResponse.Code, bearerResponse.Body.String())
	}
}

func TestAdminAuthOnlyExemptsExactPublicRoutes(t *testing.T) {
	handler := newAdminAuth(testAdminPassword).middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/healthz", http.StatusNoContent},
		{http.MethodGet, "/", http.StatusNoContent},
		{http.MethodGet, "/api/v1/auth/status", http.StatusNoContent},
		{http.MethodPost, "/api/v1/auth/session", http.StatusNoContent},
		{http.MethodPost, "/api/v1/auth/connect", http.StatusNoContent},
		{http.MethodGet, "/api/v1/auth/connect", http.StatusUnauthorized},
		{http.MethodGet, kiroOAuthCallbackPath, http.StatusNoContent},
		{http.MethodPost, kiroOAuthCallbackPath, http.StatusUnauthorized},
		{http.MethodGet, kiroOAuthCallbackPath + "/extra", http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/auth/status/extra", http.StatusUnauthorized},
		{http.MethodDelete, "/api/v1/auth/session", http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, "http://console.example"+test.path, nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}

	disabled := newAdminAuth("").middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	disabled.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "http://console.example/api/v1/accounts/example", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("disabled auth status = %d", response.Code)
	}
}

func TestAdminLoginRateLimitIsScopedBySourceAndExpires(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	auth := newAdminAuth(testAdminPassword)
	auth.now = func() time.Time { return now }

	for attempt := 0; attempt < loginMaxFailures; attempt++ {
		response := performLogin(auth, "wrong-token", "192.0.2.10:41000")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", attempt+1, response.Code)
		}
	}
	limited := performLogin(auth, testAdminPassword, "192.0.2.10:41001")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("rate-limited response = %d retry-after=%q", limited.Code, limited.Header().Get("Retry-After"))
	}

	otherSource := performLogin(auth, testAdminPassword, "192.0.2.11:41000")
	if otherSource.Code != http.StatusOK {
		t.Fatalf("different source status = %d", otherSource.Code)
	}

	now = now.Add(loginLockoutDuration + time.Second)
	recovered := performLogin(auth, testAdminPassword, "192.0.2.10:41002")
	if recovered.Code != http.StatusOK {
		t.Fatalf("post-lockout status = %d body=%s", recovered.Code, recovered.Body.String())
	}
}

func TestAdminConnectAcceptsAddressAndPasswordWithoutPersistingPassword(t *testing.T) {
	auth := newAdminAuth(testAdminPassword)
	form := url.Values{
		"address":  {"https://console.example"},
		"password": {testAdminPassword},
		"remember": {"1"},
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example/api/v1/auth/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.0.2.10:41000"
	response := httptest.NewRecorder()
	auth.connect(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/?auth=connected" {
		t.Fatalf("connect response = %d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("connect cookies = %d", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe connect cookie: %+v", cookie)
	}
	if cookie.Value == testAdminPassword || cookie.MaxAge != int(persistentSessionTTL.Seconds()) {
		t.Fatalf("connect cookie leaked password or has wrong lifetime: %+v", cookie)
	}
}

func TestAdminConnectRejectsMismatchedAddressWithoutForwardingPassword(t *testing.T) {
	auth := newAdminAuth(testAdminPassword)
	form := url.Values{
		"address":  {"https://attacker.example"},
		"password": {testAdminPassword},
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example/api/v1/auth/connect", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	auth.connect(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/?auth_error=invalid_address" {
		t.Fatalf("connect response = %d location=%q", response.Code, response.Header().Get("Location"))
	}
}

func TestNormalizeConnectionAddressRequiresHTTPSOutsideTrustedNetworks(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://monitor.example/", "https://monitor.example", true},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", true},
		{"http://192.168.1.20:8080", "http://192.168.1.20:8080", true},
		{"http://monitor.local:8080", "http://monitor.local:8080", true},
		{"http://monitor.example", "", false},
		{"https://monitor.example/subpath", "", false},
		{"https://user@monitor.example", "", false},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			got, ok := normalizeConnectionAddress(test.raw)
			if ok != test.ok || got != test.want {
				t.Fatalf("normalizeConnectionAddress(%q) = %q, %t; want %q, %t", test.raw, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestAdminAuthAcceptsBearerPasswordForAutomation(t *testing.T) {
	handler := newAdminAuth(testAdminPassword).middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://console.example/api/v1/sync/status", nil)
	request.Header.Set("Authorization", "bearer "+testAdminPassword)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("bearer status = %d", response.Code)
	}
}

func TestAPIResponsesDisableCachingAndVaryByCredentials(t *testing.T) {
	handler := apiPrivacyHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "http://console.example/api/v1/overview", nil))
	if got := apiResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := apiResponse.Header().Get("Vary"); got != "Authorization, Cookie, Origin" {
		t.Fatalf("Vary = %q", got)
	}

	staticResponse := httptest.NewRecorder()
	handler.ServeHTTP(staticResponse, httptest.NewRequest(http.MethodGet, "http://console.example/assets/app.js", nil))
	if got := staticResponse.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("static Cache-Control = %q", got)
	}
}

func loginCookie(t *testing.T, auth *adminAuth, password, remoteAddr string) *http.Cookie {
	t.Helper()
	response := performLogin(auth, password, remoteAddr)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("login status/cookies = %d/%d body=%s", response.Code, len(response.Result().Cookies()), response.Body.String())
	}
	return response.Result().Cookies()[0]
}

func performLogin(auth *adminAuth, password, remoteAddr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://console.example/api/v1/auth/session", bytes.NewBufferString(`{"password":"`+password+`"}`))
	request.RemoteAddr = remoteAddr
	response := httptest.NewRecorder()
	auth.login(response, request)
	return response
}

func TestCanonicalOriginRejectsMalformedHosts(t *testing.T) {
	for _, value := range []string{"", "example.com/path", "user@example.com", "example.com?x=1"} {
		if origin, ok := canonicalOrigin("https", value); ok || strings.TrimSpace(origin) != "" {
			t.Fatalf("canonicalOrigin accepted %q as %q", value, origin)
		}
	}
}
