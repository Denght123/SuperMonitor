package qoder

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
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
	"github.com/google/uuid"
)

const (
	RegionCN     = "cn"
	RegionGlobal = "global"
)

type Client struct{ http *http.Client }

type Credential struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Region       string    `json:"region"`
	UserID       string    `json:"userId,omitempty"`
	Nickname     string    `json:"nickname,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

type Challenge struct {
	Nonce     string
	Verifier  string
	Region    string
	VerifyURL string
	ExpiresAt time.Time
}

type Quota struct {
	Plan      string
	ExpiresAt *time.Time
	Windows   []QuotaWindow
}

type QuotaWindow struct {
	Label     string
	Remaining float64
	Total     float64
	Unit      string
	ExpiresAt *time.Time
}

type deviceTokenResponse struct {
	Token                 string `json:"token"`
	DeviceToken           string `json:"device_token"`
	RefreshToken          string `json:"refresh_token"`
	UserID                string `json:"user_id"`
	ExpiresAt             string `json:"expires_at"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at"`
}

type quotaResponse struct {
	UserID          string  `json:"userId"`
	UserType        string  `json:"userType"`
	ExpiresAt       int64   `json:"expiresAt"`
	IsQuotaExceeded bool    `json:"isQuotaExceeded"`
	UserQuota       bucket  `json:"userQuota"`
	AddOnQuota      bucket  `json:"addOnQuota"`
	TotalPercent    float64 `json:"totalUsagePercentage"`
}

type bucket struct {
	Total     float64 `json:"total"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit"`
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(30 * time.Second)} }

func StartLogin(region string) (Challenge, error) {
	region = normalizeRegion(region)
	verifierBytes := make([]byte, 48)
	if _, err := rand.Read(verifierBytes); err != nil {
		return Challenge{}, fmt.Errorf("生成 Qoder PKCE 失败: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	nonce := uuid.NewString()
	if region == RegionGlobal {
		nonce = strings.ReplaceAll(nonce, "-", "")
	}
	values := url.Values{
		"nonce":            {nonce},
		"challenge":        {challenge},
		"challenge_method": {"S256"},
		"redirect_uri":     {redirectURI(region)},
	}
	if region == RegionCN {
		values.Set("client_id", "1c5e33e1-364d-4ce6-b02c-acaa81274a5c")
		values.Set("machine_id", uuid.NewString())
	}
	return Challenge{Nonce: nonce, Verifier: verifier, Region: region, VerifyURL: website(region) + "/device/selectAccounts?" + values.Encode(), ExpiresAt: time.Now().Add(10 * time.Minute)}, nil
}

func (c *Client) PollLogin(ctx context.Context, challenge Challenge) (Credential, bool, error) {
	values := url.Values{"nonce": {challenge.Nonce}, "verifier": {challenge.Verifier}, "challenge_method": {"S256"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openAPI(challenge.Region)+"/api/v1/deviceToken/poll?"+values.Encode(), nil)
	if err != nil {
		return Credential{}, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "QoderWork")
	resp, err := c.http.Do(req)
	if err != nil {
		return Credential{}, false, fmt.Errorf("连接 Qoder 官方登录接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusAccepted {
		return Credential{}, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credential{}, false, fmt.Errorf("Qoder 登录轮询返回 HTTP %d: %s", resp.StatusCode, compact(body))
	}
	var token deviceTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return Credential{}, false, fmt.Errorf("解析 Qoder 登录响应失败: %w", err)
	}
	if token.accessToken() == "" {
		return Credential{}, true, nil
	}
	return credentialFromToken(token, challenge.Region), false, nil
}

func (c *Client) FetchQuota(ctx context.Context, credential *Credential) (Quota, bool, error) {
	quota, status, err := c.fetchQuota(ctx, credential)
	if err == nil {
		return quota, false, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return Quota{}, false, err
	}
	if credential.RefreshToken == "" {
		return Quota{}, false, fmt.Errorf("Qoder 登录已过期且没有 refresh_token，请重新授权")
	}
	refreshed, refreshErr := c.Refresh(ctx, credential.RefreshToken, credential.Region)
	if refreshErr != nil {
		return Quota{}, false, fmt.Errorf("Qoder 登录已过期，刷新失败: %w", refreshErr)
	}
	if refreshed.UserID == "" {
		refreshed.UserID = credential.UserID
	}
	if refreshed.Nickname == "" {
		refreshed.Nickname = credential.Nickname
	}
	*credential = refreshed
	quota, _, err = c.fetchQuota(ctx, credential)
	return quota, true, err
}

func (c *Client) fetchQuota(ctx context.Context, credential *Credential) (Quota, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openAPI(credential.Region)+"/api/v2/quota/usage", nil)
	if err != nil {
		return Quota{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "QoderWork")
	resp, err := c.http.Do(req)
	if err != nil {
		return Quota{}, 0, fmt.Errorf("连接 Qoder 官方额度接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Quota{}, resp.StatusCode, fmt.Errorf("Qoder 额度接口返回 HTTP %d: %s", resp.StatusCode, compact(body))
	}
	var payload quotaResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Quota{}, resp.StatusCode, fmt.Errorf("解析 Qoder 额度响应失败: %w", err)
	}
	if payload.UserQuota.Total == 0 && payload.AddOnQuota.Total == 0 && payload.UserQuota.Remaining == 0 && payload.AddOnQuota.Remaining == 0 {
		return Quota{}, resp.StatusCode, fmt.Errorf("Qoder 额度响应缺少 userQuota/addOnQuota 数据")
	}
	unit := payload.UserQuota.Unit
	if unit == "" {
		unit = "credits"
	}
	var expires *time.Time
	if payload.ExpiresAt > 0 {
		t := time.UnixMilli(payload.ExpiresAt).UTC()
		expires = &t
	}
	return Quota{Plan: payload.UserType, ExpiresAt: expires, Windows: []QuotaWindow{
		{Label: "基础额度", Remaining: payload.UserQuota.Remaining, Total: payload.UserQuota.Total, Unit: unit, ExpiresAt: expires},
		{Label: "赠送 / 签到额度", Remaining: payload.AddOnQuota.Remaining, Total: payload.AddOnQuota.Total, Unit: unit, ExpiresAt: expires},
	}}, resp.StatusCode, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken, region string) (Credential, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAPI(region)+"/api/v1/deviceToken/refresh", bytes.NewReader(body))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("Qoder token 刷新返回 HTTP %d: %s", resp.StatusCode, compact(raw))
	}
	var token deviceTokenResponse
	if err := json.Unmarshal(raw, &token); err != nil {
		return Credential{}, err
	}
	if token.accessToken() == "" {
		return Credential{}, fmt.Errorf("Qoder token 刷新响应缺少 access token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	return credentialFromToken(token, region), nil
}

func ParseCredential(raw []byte, region string) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("Qoder 认证文件为空或超过 1 MB")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return Credential{}, fmt.Errorf("Qoder 认证文件不是有效 JSON: %w", err)
	}
	var result Credential
	walk(value, func(m map[string]any) bool {
		result.AccessToken = firstString(m, "accessToken", "access_token", "token", "device_token")
		if result.AccessToken == "" {
			return false
		}
		result.RefreshToken = firstString(m, "refreshToken", "refresh_token")
		result.UserID = firstString(m, "userId", "user_id", "uid", "id")
		result.Nickname = firstString(m, "nickname", "name", "email")
		return true
	})
	if result.AccessToken == "" {
		return Credential{}, fmt.Errorf("Qoder 认证文件缺少 token/device_token/access_token")
	}
	result.Region = normalizeRegion(region)
	return result, nil
}

func credentialFromToken(token deviceTokenResponse, region string) Credential {
	expires := time.Now().Add(30 * 24 * time.Hour)
	if token.ExpiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
			expires = parsed
		}
	} else if token.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(token.ExpiresIn) * time.Millisecond)
	}
	return Credential{AccessToken: token.accessToken(), RefreshToken: token.RefreshToken, Region: normalizeRegion(region), UserID: token.UserID, ExpiresAt: expires.UTC()}
}

func (d deviceTokenResponse) accessToken() string {
	if d.Token != "" {
		return d.Token
	}
	return d.DeviceToken
}

func normalizeRegion(region string) string {
	if strings.EqualFold(region, RegionCN) {
		return RegionCN
	}
	return RegionGlobal
}
func website(region string) string {
	if normalizeRegion(region) == RegionCN {
		return "https://qoder.com.cn"
	}
	return "https://qoder.com"
}
func openAPI(region string) string {
	if normalizeRegion(region) == RegionCN {
		return "https://openapi.qoder.com.cn"
	}
	return "https://openapi.qoder.sh"
}
func redirectURI(region string) string {
	if normalizeRegion(region) == RegionCN {
		return "qoder-work-cn://"
	}
	return "qoder://aicoding.aicoding-agent/login-success"
}
func compact(body []byte) string {
	value := strings.Join(strings.Fields(string(body)), " ")
	if len(value) > 240 {
		value = value[:240] + "…"
	}
	return value
}
func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
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
