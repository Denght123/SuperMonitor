package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	clientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	tokenURL              = "https://auth.openai.com/oauth/token"
	deviceUserCodeURL     = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	deviceTokenURL        = "https://auth.openai.com/api/accounts/deviceauth/token"
	deviceVerificationURL = "https://auth.openai.com/codex/device"
	deviceRedirectURI     = "https://auth.openai.com/deviceauth/callback"
	usageURL              = "https://chatgpt.com/backend-api/wham/usage"
	userAgent             = "codex_cli_rs/0.154.0 (Windows; x86_64) SuperMonitor/0.3.0"
)

type Client struct {
	http *http.Client
}

type Credential struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	IDToken      string    `json:"idToken,omitempty"`
	AccountID    string    `json:"accountId,omitempty"`
	Email        string    `json:"email,omitempty"`
	Plan         string    `json:"plan,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

type QuotaWindow struct {
	Label            string
	UsedPercent      float64
	RemainingPercent float64
	WindowSeconds    int64
	ResetAt          *time.Time
}

type Usage struct {
	Plan    string
	Windows []QuotaWindow
	Credits *float64
}

type DeviceChallenge struct {
	DeviceAuthID string
	UserCode     string
	Interval     time.Duration
	VerifyURL    string
	ExpiresAt    time.Time
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	CodeChallenge     string `json:"code_challenge"`
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}}
}

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("OAuth 文件为空或超过 1 MB")
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Credential{}, fmt.Errorf("OAuth 文件不是有效 JSON: %w", err)
	}
	tokens := objectAt(root, "tokens")
	credential := Credential{
		AccessToken:  firstString(tokens, root, "access_token", "accessToken"),
		RefreshToken: firstString(tokens, root, "refresh_token", "refreshToken"),
		IDToken:      firstString(tokens, root, "id_token", "idToken"),
		AccountID:    firstString(tokens, root, "account_id", "accountId", "chatgpt_account_id"),
		Email:        firstString(tokens, root, "email"),
		Plan:         firstString(tokens, root, "plan_type", "plan"),
	}
	if credential.AccessToken == "" {
		return Credential{}, fmt.Errorf("OAuth 文件缺少 access_token")
	}
	credential.enrichFromJWT()
	return credential, nil
}

func (c *Credential) enrichFromJWT() {
	for _, token := range []string{c.IDToken, c.AccessToken} {
		claims := parseJWTClaims(token)
		if claims == nil {
			continue
		}
		if c.Email == "" {
			c.Email = stringClaim(claims, "email")
			if c.Email == "" {
				c.Email = nestedStringClaim(claims, "https://api.openai.com/profile", "email")
			}
		}
		if c.AccountID == "" {
			c.AccountID = nestedStringClaim(claims, "https://api.openai.com/auth", "chatgpt_account_id")
			if c.AccountID == "" {
				c.AccountID = stringClaim(claims, "chatgpt_account_id")
			}
		}
		if c.Plan == "" {
			c.Plan = nestedStringClaim(claims, "https://api.openai.com/auth", "chatgpt_plan_type")
			if c.Plan == "" {
				c.Plan = stringClaim(claims, "chatgpt_plan_type")
			}
		}
		if c.ExpiresAt.IsZero() {
			if exp, ok := numberClaim(claims, "exp"); ok {
				c.ExpiresAt = time.Unix(exp, 0).UTC()
			}
		}
	}
}

func (c *Client) FetchUsage(ctx context.Context, credential *Credential) (Usage, bool, error) {
	usage, status, err := c.fetchUsage(ctx, credential)
	if err == nil {
		return usage, false, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return Usage{}, false, err
	}
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return Usage{}, false, fmt.Errorf("Codex 授权已失效，文件中没有 refresh_token，请重新登录")
	}
	refreshed, refreshErr := c.Refresh(ctx, credential.RefreshToken)
	if refreshErr != nil {
		return Usage{}, false, refreshErr
	}
	if refreshed.Email == "" {
		refreshed.Email = credential.Email
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = credential.AccountID
	}
	if refreshed.Plan == "" {
		refreshed.Plan = credential.Plan
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = credential.IDToken
	}
	*credential = refreshed
	usage, _, err = c.fetchUsage(ctx, credential)
	return usage, true, err
}

func (c *Client) fetchUsage(ctx context.Context, credential *Credential) (Usage, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return Usage{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("originator", "codex_cli_rs")
	if credential.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", credential.AccountID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, 0, fmt.Errorf("连接 Codex 额度接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Usage{}, resp.StatusCode, fmt.Errorf("读取 Codex 额度响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, resp.StatusCode, fmt.Errorf("Codex 额度接口返回 HTTP %d", resp.StatusCode)
	}
	usage, err := parseUsage(body)
	if err != nil {
		return Usage{}, resp.StatusCode, err
	}
	if usage.Plan != "" {
		credential.Plan = usage.Plan
	}
	return usage, resp.StatusCode, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (Credential, error) {
	form := url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {"openid profile email"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.readTokenResponse(req, refreshToken)
}

func (c *Client) StartDeviceLogin(ctx context.Context) (DeviceChallenge, error) {
	payload, _ := json.Marshal(map[string]string{"client_id": clientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, bytes.NewReader(payload))
	if err != nil {
		return DeviceChallenge{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return DeviceChallenge{}, fmt.Errorf("启动 Codex 设备登录失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeviceChallenge{}, fmt.Errorf("Codex 设备登录接口返回 HTTP %d", resp.StatusCode)
	}
	var value struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return DeviceChallenge{}, fmt.Errorf("解析 Codex 设备登录响应失败: %w", err)
	}
	if value.UserCode == "" {
		value.UserCode = value.UserCodeAlt
	}
	if value.DeviceAuthID == "" || value.UserCode == "" {
		return DeviceChallenge{}, fmt.Errorf("Codex 设备登录响应缺少验证码")
	}
	interval := 5 * time.Second
	var seconds int
	if json.Unmarshal(value.Interval, &seconds) == nil && seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	return DeviceChallenge{DeviceAuthID: value.DeviceAuthID, UserCode: value.UserCode, Interval: interval, VerifyURL: deviceVerificationURL, ExpiresAt: time.Now().UTC().Add(15 * time.Minute)}, nil
}

func (c *Client) CompleteDeviceLogin(ctx context.Context, challenge DeviceChallenge) (Credential, error) {
	ticker := time.NewTicker(challenge.Interval)
	defer ticker.Stop()
	for {
		result, pending, err := c.pollDevice(ctx, challenge)
		if err != nil {
			return Credential{}, err
		}
		if !pending {
			return c.exchangeDeviceCode(ctx, result)
		}
		select {
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) pollDevice(ctx context.Context, challenge DeviceChallenge) (deviceTokenResponse, bool, error) {
	payload, _ := json.Marshal(map[string]string{"device_auth_id": challenge.DeviceAuthID, "user_code": challenge.UserCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, bytes.NewReader(payload))
	if err != nil {
		return deviceTokenResponse{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return deviceTokenResponse{}, false, fmt.Errorf("轮询 Codex 设备登录失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return deviceTokenResponse{}, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceTokenResponse{}, false, fmt.Errorf("Codex 设备登录轮询返回 HTTP %d", resp.StatusCode)
	}
	var result deviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, false, fmt.Errorf("解析 Codex 设备授权失败: %w", err)
	}
	if result.AuthorizationCode == "" || result.CodeVerifier == "" {
		return result, false, fmt.Errorf("Codex 设备授权响应不完整")
	}
	return result, false, nil
}

func (c *Client) exchangeDeviceCode(ctx context.Context, result deviceTokenResponse) (Credential, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {result.AuthorizationCode},
		"redirect_uri":  {deviceRedirectURI},
		"code_verifier": {result.CodeVerifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.readTokenResponse(req, "")
}

func (c *Client) readTokenResponse(req *http.Request, fallbackRefresh string) (Credential, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return Credential{}, fmt.Errorf("刷新 Codex 授权失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return Credential{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("Codex 授权刷新失败 HTTP %d", resp.StatusCode)
	}
	var value struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return Credential{}, fmt.Errorf("解析 Codex 授权响应失败: %w", err)
	}
	if value.AccessToken == "" {
		return Credential{}, fmt.Errorf("Codex 授权响应缺少 access_token")
	}
	if value.RefreshToken == "" {
		value.RefreshToken = fallbackRefresh
	}
	credential := Credential{AccessToken: value.AccessToken, RefreshToken: value.RefreshToken, IDToken: value.IDToken}
	if value.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().UTC().Add(time.Duration(value.ExpiresIn) * time.Second)
	}
	credential.enrichFromJWT()
	return credential, nil
}

func parseUsage(body []byte) (Usage, error) {
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Usage{}, fmt.Errorf("Codex 额度响应不是有效 JSON: %w", err)
	}
	usage := Usage{Plan: stringValue(root["plan_type"])}
	rateLimit, _ := root["rate_limit"].(map[string]any)
	for _, entry := range []struct {
		key      string
		fallback string
	}{
		{"primary_window", "主额度窗口"},
		{"secondary_window", "次额度窗口"},
	} {
		window, _ := rateLimit[entry.key].(map[string]any)
		if window == nil {
			continue
		}
		used, ok := floatValue(window["used_percent"])
		if !ok {
			continue
		}
		seconds, _ := intValue(window["limit_window_seconds"])
		resetAt := parseEpoch(window["reset_at"])
		if resetAt == nil {
			if after, ok := intValue(window["reset_after_seconds"]); ok && after > 0 {
				value := time.Now().UTC().Add(time.Duration(after) * time.Second)
				resetAt = &value
			}
		}
		usage.Windows = append(usage.Windows, QuotaWindow{Label: windowLabel(seconds, entry.fallback), UsedPercent: used, RemainingPercent: clamp(100 - used), WindowSeconds: seconds, ResetAt: resetAt})
	}
	if credits, ok := root["credits"].(map[string]any); ok {
		if balance, ok := floatValue(credits["balance"]); ok {
			usage.Credits = &balance
		}
	}
	if len(usage.Windows) == 0 && usage.Credits == nil {
		return Usage{}, fmt.Errorf("Codex 额度响应缺少可识别的额度窗口")
	}
	return usage, nil
}

func windowLabel(seconds int64, fallback string) string {
	switch {
	case seconds > 0 && seconds <= 5*3600+600:
		return "5 小时限额"
	case seconds > 0 && seconds <= 24*3600+600:
		return "24 小时限额"
	case seconds > 0 && seconds <= 7*24*3600+3600:
		return "周限额"
	case seconds > 0 && seconds <= 31*24*3600:
		return "月限额"
	default:
		return fallback
	}
}

func objectAt(root map[string]any, key string) map[string]any {
	value, _ := root[key].(map[string]any)
	return value
}

func firstString(objects ...any) string {
	var keys []string
	var maps []map[string]any
	for _, item := range objects {
		switch value := item.(type) {
		case map[string]any:
			maps = append(maps, value)
		case string:
			keys = append(keys, value)
		}
	}
	for _, object := range maps {
		for _, key := range keys {
			if value := strings.TrimSpace(stringValue(object[key])); value != "" {
				return value
			}
		}
	}
	return ""
}

func parseJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func nestedStringClaim(claims map[string]any, object, key string) string {
	value, _ := claims[object].(map[string]any)
	return stringClaim(value, key)
}

func stringClaim(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(claims[key]))
}

func numberClaim(claims map[string]any, key string) (int64, bool) { return intValue(claims[key]) }
func stringValue(value any) string                                { text, _ := value.(string); return text }
func floatValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		result, err := number.Float64()
		return result, err == nil
	case float64:
		return number, true
	case string:
		result, err := json.Number(number).Float64()
		return result, err == nil
	default:
		return 0, false
	}
}
func intValue(value any) (int64, bool) { number, ok := floatValue(value); return int64(number), ok }
func parseEpoch(value any) *time.Time {
	epoch, ok := intValue(value)
	if !ok || epoch <= 0 {
		return nil
	}
	parsed := time.Unix(epoch, 0).UTC()
	return &parsed
}
func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
