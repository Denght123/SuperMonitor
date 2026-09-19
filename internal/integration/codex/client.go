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

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const (
	clientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	tokenURL              = "https://auth.openai.com/oauth/token"
	deviceUserCodeURL     = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	deviceTokenURL        = "https://auth.openai.com/api/accounts/deviceauth/token"
	deviceVerificationURL = "https://auth.openai.com/codex/device"
	deviceRedirectURI     = "https://auth.openai.com/deviceauth/callback"
	usageURL              = "https://chatgpt.com/backend-api/wham/usage"
	userAgent             = "codex-cli/0.154.0 SuperMonitor/0.6.2"
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
	Plan             string
	Windows          []QuotaWindow
	Credits          *float64
	CreditsUnlimited bool
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
	return &Client{http: netutil.NewHTTPClient(30 * time.Second)}
}

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("OAuth 文件为空或超过 1 MB")
	}
	var root any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Credential{}, fmt.Errorf("OAuth 文件不是有效 JSON: %w", err)
	}
	credential, expires := selectCredential(root)
	if credential.AccessToken == "" && credential.RefreshToken == "" {
		return Credential{}, fmt.Errorf("OAuth 文件缺少 access_token 或 refresh_token；支持 Codex auth.json、CPA 与 Sub2API 导出格式")
	}
	if expires != nil {
		credential.ExpiresAt = parseCredentialTime(expires)
	}
	credential.enrichFromJWT()
	if accountID := accountIDFromJWT(credential.IDToken, credential.AccessToken); accountID != "" {
		credential.AccountID = accountID
	}
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
	}
	if claims := parseJWTClaims(c.AccessToken); claims != nil {
		if exp, ok := numberClaim(claims, "exp"); ok {
			c.ExpiresAt = time.Unix(exp, 0).UTC()
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
		if isRotatedRefreshToken(refreshErr) {
			return Usage{}, false, fmt.Errorf("该认证文件是已被刷新过的旧快照，refresh token 已轮换，不能重复导入。请从正在运行的 CPA 导出最新凭据，或使用 OpenAI 官方设备登录")
		}
		return Usage{}, false, fmt.Errorf("Codex access_token 已被额度接口拒绝（%s）；refresh_token 也无法刷新，请使用设备登录重新授权。刷新详情: %s", compactError(err), compactError(refreshErr))
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
		return Usage{}, resp.StatusCode, responseError("Codex 额度接口", resp.StatusCode, body)
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
	req.Header.Set("User-Agent", userAgent)
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
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return DeviceChallenge{}, fmt.Errorf("启动 Codex 设备登录失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return DeviceChallenge{}, fmt.Errorf("读取 Codex 设备登录响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeviceChallenge{}, responseError("Codex 设备登录接口", resp.StatusCode, body)
	}
	var value struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		UserCodeAlt  string          `json:"usercode"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
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
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return deviceTokenResponse{}, false, fmt.Errorf("轮询 Codex 设备登录失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return deviceTokenResponse{}, false, fmt.Errorf("读取 Codex 设备登录轮询响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return deviceTokenResponse{}, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceTokenResponse{}, false, responseError("Codex 设备登录轮询", resp.StatusCode, body)
	}
	var result deviceTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
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
	req.Header.Set("User-Agent", userAgent)
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
		return Credential{}, responseError("Codex 授权刷新", resp.StatusCode, body)
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
		hasCredits, hasFlag := boolValue(credits["has_credits"])
		unlimited, _ := boolValue(credits["unlimited"])
		usage.CreditsUnlimited = hasCredits && unlimited
		if balance, ok := floatValue(credits["balance"]); ok && !unlimited && (hasCredits || !hasFlag && balance > 0) {
			usage.Credits = &balance
		}
	}
	if len(usage.Windows) == 0 && usage.Credits == nil {
		return Usage{}, fmt.Errorf("Codex 额度响应缺少可识别的额度窗口")
	}
	return usage, nil
}

func isRotatedRefreshToken(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "refresh_token_reused") || strings.Contains(message, "invalid_refresh_token") || strings.Contains(message, "invalid refresh token")
}

func boolValue(value any) (bool, bool) {
	switch item := value.(type) {
	case bool:
		return item, true
	case string:
		if strings.EqualFold(strings.TrimSpace(item), "true") {
			return true, true
		}
		if strings.EqualFold(strings.TrimSpace(item), "false") {
			return false, true
		}
	}
	return false, false
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

type credentialCandidate struct {
	credential Credential
	expires    any
	score      int
	order      int
}

func selectCredential(root any) (Credential, any) {
	var candidates []credentialCandidate
	order := 0
	var walk func(any, []map[string]any, int)
	walk = func(value any, ancestors []map[string]any, depth int) {
		if depth > 10 {
			return
		}
		switch item := value.(type) {
		case map[string]any:
			if hasCredentialToken(item) {
				maps := credentialContextMaps(item, ancestors)
				credential := Credential{
					AccessToken:  firstStringInMaps(maps, "access_token", "accessToken", "access"),
					RefreshToken: firstStringInMaps(maps, "refresh_token", "refreshToken", "refresh"),
					IDToken:      firstStringInMaps(maps, "id_token", "idToken"),
					AccountID:    firstStringInMaps(maps, "account_id", "accountId", "chatgpt_account_id", "chatgptAccountId"),
					Email:        firstStringInMaps(maps, "email", "account_email", "accountEmail"),
					Plan:         firstStringInMaps(maps, "plan_type", "planType", "plan"),
				}
				score := credentialScore(item, ancestors, credential)
				candidates = append(candidates, credentialCandidate{credential: credential, expires: firstValueInMaps(maps, "expires_at", "expiresAt", "expired", "expiry"), score: score, order: order})
				order++
			}
			nextAncestors := append(append([]map[string]any{}, ancestors...), item)
			for _, child := range item {
				walk(child, nextAncestors, depth+1)
			}
		case []any:
			for _, child := range item {
				walk(child, ancestors, depth+1)
			}
		case string:
			text := strings.TrimSpace(item)
			if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
				var nested any
				decoder := json.NewDecoder(strings.NewReader(text))
				decoder.UseNumber()
				if decoder.Decode(&nested) == nil {
					walk(nested, ancestors, depth+1)
				}
			}
		}
	}
	walk(root, nil, 0)
	if len(candidates) == 0 {
		return Credential{}, nil
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.score > best.score || candidate.score == best.score && candidate.order < best.order {
			best = candidate
		}
	}
	return best.credential, best.expires
}

func hasCredentialToken(object map[string]any) bool {
	return firstStringInMaps([]map[string]any{object}, "access_token", "accessToken", "access", "refresh_token", "refreshToken", "refresh") != ""
}

func credentialContextMaps(object map[string]any, ancestors []map[string]any) []map[string]any {
	maps := []map[string]any{object}
	for _, ancestor := range reverseMaps(ancestors) {
		maps = append(maps, ancestor)
		// Sub2API commonly stores account metadata beside a credentials/auth_json
		// wrapper. Only inspect explicitly metadata-shaped siblings so tokens from
		// another account record can never be combined with this candidate.
		for _, key := range []string{"account", "profile", "user"} {
			if metadata, ok := ancestor[key].(map[string]any); ok {
				maps = append(maps, metadata)
			}
		}
	}
	return maps
}

func credentialScore(object map[string]any, ancestors []map[string]any, credential Credential) int {
	score := 0
	provider := strings.ToLower(firstStringInMaps(append([]map[string]any{object}, reverseMaps(ancestors)...), "type", "provider", "provider_id", "providerId"))
	if provider == "codex" || strings.Contains(provider, "openai") {
		score += 100
	}
	if credential.AccessToken != "" {
		score += 40
	}
	if parseJWTClaims(credential.AccessToken) != nil {
		score += 20
	}
	if credential.RefreshToken != "" {
		score += 10
	}
	if credential.AccountID != "" || accountIDFromJWT(credential.IDToken, credential.AccessToken) != "" {
		score += 8
	}
	return score
}

func reverseMaps(values []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for index := len(values) - 1; index >= 0; index-- {
		result = append(result, values[index])
	}
	return result
}

func firstStringInMaps(maps []map[string]any, keys ...string) string {
	for _, object := range maps {
		for _, key := range keys {
			if value := strings.TrimSpace(stringValue(object[key])); value != "" {
				return value
			}
		}
	}
	return ""
}

func firstValueInMaps(maps []map[string]any, keys ...string) any {
	for _, object := range maps {
		for _, key := range keys {
			if value, ok := object[key]; ok && value != nil {
				return value
			}
		}
	}
	return nil
}

func accountIDFromJWT(tokens ...string) string {
	for _, token := range tokens {
		claims := parseJWTClaims(token)
		if claims == nil {
			continue
		}
		if value := nestedStringClaim(claims, "https://api.openai.com/auth", "chatgpt_account_id"); value != "" {
			return value
		}
		if value := stringClaim(claims, "chatgpt_account_id"); value != "" {
			return value
		}
	}
	return ""
}

func compactError(err error) string {
	if err == nil {
		return "未知错误"
	}
	text := strings.Join(strings.Fields(err.Error()), " ")
	if len(text) > 260 {
		return text[:260] + "…"
	}
	return text
}

func parseCredentialTime(value any) time.Time {
	if epoch, ok := intValue(value); ok && epoch > 0 {
		if epoch > 10_000_000_000 {
			epoch /= 1000
		}
		return time.Unix(epoch, 0).UTC()
	}
	if text, ok := value.(string); ok {
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(text)); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func responseError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 500 {
		detail = detail[:500] + "…"
	}
	if detail == "" {
		detail = "响应正文为空"
	}
	return fmt.Errorf("%s返回 HTTP %d: %s", operation, status, detail)
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
