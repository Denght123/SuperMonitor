package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
	"github.com/google/uuid"
)

const (
	authPortalURL = "https://app.kiro.dev/signin"
	tokenURL      = "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/token"
	refreshURL    = "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
)

type Client struct {
	http    *http.Client
	mu      sync.Mutex
	pending map[string]Challenge
}
type Challenge struct {
	ID          string
	State       string
	Verifier    string
	CallbackURL string
	VerifyURL   string
	ExpiresAt   time.Time
}
type Credential struct {
	AccessToken  string         `json:"accessToken"`
	RefreshToken string         `json:"refreshToken,omitempty"`
	ProfileARN   string         `json:"profileArn"`
	Email        string         `json:"email,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}
type Usage struct {
	Plan       string
	Tier       string
	Total      float64
	Used       float64
	BonusTotal float64
	BonusUsed  float64
	ResetAt    *time.Time
}

func NewClient() *Client {
	return &Client{http: netutil.NewHTTPClient(30 * time.Second), pending: make(map[string]Challenge)}
}

func (c *Client) StartLogin(callbackURL string) (Challenge, error) {
	stateBytes, verifierBytes := make([]byte, 24), make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return Challenge{}, err
	}
	if _, err := rand.Read(verifierBytes); err != nil {
		return Challenge{}, err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	digest := sha256.Sum256([]byte(verifier))
	values := url.Values{
		"state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method": {"S256"}, "redirect_uri": {callbackURL}, "redirect_from": {"KiroIDE"},
	}
	challenge := Challenge{ID: uuid.NewString(), State: state, Verifier: verifier, CallbackURL: callbackURL, VerifyURL: authPortalURL + "?" + values.Encode(), ExpiresAt: time.Now().Add(10 * time.Minute)}
	c.mu.Lock()
	c.pending[state] = challenge
	c.mu.Unlock()
	return challenge, nil
}

func (c *Client) CompleteLogin(ctx context.Context, values url.Values) (Challenge, Credential, error) {
	state := values.Get("state")
	c.mu.Lock()
	challenge, ok := c.pending[state]
	if ok {
		delete(c.pending, state)
	}
	c.mu.Unlock()
	if !ok || state == "" {
		return Challenge{}, Credential{}, fmt.Errorf("Kiro OAuth state 无效或登录已过期")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return challenge, Credential{}, fmt.Errorf("Kiro 登录已超时，请重新发起")
	}
	if oauthErr := values.Get("error"); oauthErr != "" {
		return challenge, Credential{}, fmt.Errorf("Kiro 授权失败: %s", oauthErr)
	}
	code := values.Get("code")
	if code == "" {
		return challenge, Credential{}, fmt.Errorf("Kiro 回调缺少授权 code")
	}
	loginOption := first(values.Get("login_option"), values.Get("loginOption"))
	redirectURI := challenge.CallbackURL
	if loginOption != "" {
		redirectURI += "?login_option=" + url.QueryEscape(loginOption)
	}
	body, _ := json.Marshal(map[string]string{"code": code, "code_verifier": challenge.Verifier, "redirect_uri": redirectURI})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return challenge, Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return challenge, Credential{}, fmt.Errorf("连接 Kiro OAuth token 接口失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return challenge, Credential{}, fmt.Errorf("Kiro OAuth token 接口返回 HTTP %d", resp.StatusCode)
	}
	credential, err := parseCredentialJSON(raw)
	if err != nil {
		return challenge, Credential{}, err
	}
	credential.Provider = loginOption
	return challenge, credential, nil
}

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 2*1024*1024 {
		return Credential{}, fmt.Errorf("Kiro 认证文件为空或超过 2 MB")
	}
	return parseCredentialJSON(raw)
}

func parseCredentialJSON(raw []byte) (Credential, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return Credential{}, fmt.Errorf("Kiro 认证文件不是有效 JSON: %w", err)
	}
	var credential Credential
	walk(root, func(m map[string]any) bool {
		credential.AccessToken = stringAt(m, "accessToken", "access_token", "token", "idToken", "id_token", "accessTokenJwt")
		if credential.AccessToken == "" {
			return false
		}
		credential.RefreshToken = stringAt(m, "refreshToken", "refresh_token", "refreshTokenJwt")
		credential.ProfileARN = stringAt(m, "profileArn", "profile_arn", "arn")
		credential.Email = stringAt(m, "email", "login_hint", "loginHint")
		credential.Provider = stringAt(m, "provider", "loginProvider", "login_option")
		credential.Raw = m
		return true
	})
	if credential.AccessToken == "" {
		return Credential{}, fmt.Errorf("Kiro 认证信息缺少 access token")
	}
	if credential.ProfileARN == "" {
		credential.ProfileARN = profileARNFromJWT(credential.AccessToken)
	}
	if credential.ProfileARN == "" {
		return Credential{}, fmt.Errorf("Kiro 认证信息缺少 profileArn；请导入 ~/.aws/sso/cache/kiro-auth-token.json 与包含 profileArn 的完整导出文件")
	}
	return credential, nil
}

func (c *Client) FetchUsage(ctx context.Context, credential *Credential) (Usage, bool, error) {
	usage, status, err := c.fetchUsage(ctx, credential)
	if err == nil {
		return usage, false, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return Usage{}, false, err
	}
	if credential.RefreshToken == "" {
		return Usage{}, false, fmt.Errorf("Kiro 登录已过期且没有 refreshToken，请重新授权")
	}
	refreshed, refreshErr := c.Refresh(ctx, credential.RefreshToken)
	if refreshErr != nil {
		return Usage{}, false, refreshErr
	}
	credential.AccessToken = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		credential.RefreshToken = refreshed.RefreshToken
	}
	usage, _, err = c.fetchUsage(ctx, credential)
	return usage, true, err
}

func (c *Client) fetchUsage(ctx context.Context, credential *Credential) (Usage, int, error) {
	region := regionFromARN(credential.ProfileARN)
	endpoint := "https://q.us-east-1.amazonaws.com"
	if region == "eu-central-1" {
		endpoint = "https://q.eu-central-1.amazonaws.com"
	}
	values := url.Values{"origin": {"AI_EDITOR"}, "profileArn": {credential.ProfileARN}, "resourceType": {"AGENTIC_REQUEST"}, "isEmailRequired": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/getUsageLimits?"+values.Encode(), nil)
	if err != nil {
		return Usage{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, 0, fmt.Errorf("连接 Kiro 官方用量接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, resp.StatusCode, fmt.Errorf("Kiro 用量接口返回 HTTP %d", resp.StatusCode)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return Usage{}, resp.StatusCode, fmt.Errorf("解析 Kiro 用量响应失败: %w", err)
	}
	state := findUsageState(root)
	breakdown := firstBreakdown(state)
	if breakdown == nil {
		return Usage{}, resp.StatusCode, fmt.Errorf("Kiro 用量响应缺少 usageBreakdownList")
	}
	usage := Usage{Plan: pickString(state, "planName", "currentPlanName"), Tier: pickString(state, "planTier", "tier")}
	usage.Total = pickNumber(breakdown, "usageLimitWithPrecision", "usageLimit", "limit", "total")
	usage.Used = pickNumber(breakdown, "currentUsageWithPrecision", "currentUsage", "used")
	if trial, ok := breakdown["freeTrialInfo"].(map[string]any); ok {
		usage.BonusTotal = pickNumber(trial, "usageLimitWithPrecision", "usageLimit", "limit", "total")
		usage.BonusUsed = pickNumber(trial, "currentUsageWithPrecision", "currentUsage", "used")
	}
	if reset := pickTime(breakdown, "resetDate", "resetAt"); reset != nil {
		usage.ResetAt = reset
	}
	if usage.Total <= 0 && usage.BonusTotal <= 0 {
		return Usage{}, resp.StatusCode, fmt.Errorf("Kiro 用量响应没有可显示的额度总量")
	}
	return usage, resp.StatusCode, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (Credential, error) {
	body, _ := json.Marshal(map[string]string{"refreshToken": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(body))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("Kiro refreshToken 接口返回 HTTP %d", resp.StatusCode)
	}
	credential, err := parseCredentialJSON(raw)
	if err != nil {
		return Credential{}, err
	}
	if credential.RefreshToken == "" {
		credential.RefreshToken = refreshToken
	}
	return credential, nil
}

func regionFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) > 3 {
		return parts[3]
	}
	return "us-east-1"
}
func profileARNFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return stringAt(m, "profileArn", "profile_arn", "arn")
}
func findUsageState(root map[string]any) map[string]any {
	if value, ok := root["usageState"].(map[string]any); ok {
		return value
	}
	if value, ok := root["kiro.resourceNotifications.usageState"].(map[string]any); ok {
		return value
	}
	return root
}
func firstBreakdown(root map[string]any) map[string]any {
	for _, key := range []string{"usageBreakdownList", "usageBreakdowns"} {
		if list, ok := root[key].([]any); ok {
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					return m
				}
			}
		}
	}
	return nil
}
func pickString(m map[string]any, keys ...string) string { return stringAt(m, keys...) }
func pickNumber(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch value := m[key].(type) {
		case float64:
			return value
		case json.Number:
			n, _ := value.Float64()
			return n
		case string:
			var n float64
			if _, err := fmt.Sscan(value, &n); err == nil {
				return n
			}
		}
	}
	return 0
}
func pickTime(m map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		switch v := m[key].(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				parsed = parsed.UTC()
				return &parsed
			}
		case float64:
			seconds := int64(v)
			if seconds > 1e12 {
				seconds /= 1000
			}
			if seconds > 0 {
				parsed := time.Unix(seconds, 0).UTC()
				return &parsed
			}
		}
	}
	return nil
}
func stringAt(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func walk(value any, visit func(map[string]any) bool) bool {
	switch current := value.(type) {
	case map[string]any:
		if visit(current) {
			return true
		}
		for _, child := range current {
			if walk(child, visit) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if walk(child, visit) {
				return true
			}
		}
	}
	return false
}
