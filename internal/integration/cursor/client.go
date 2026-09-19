package cursor

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
	loginURL     = "https://cursor.com/loginDeepControl"
	pollURL      = "https://api2.cursor.sh/auth/poll"
	refreshURL   = "https://api2.cursor.sh/oauth/token"
	usageURL     = "https://cursor.com/api/usage-summary"
	clientID     = "KbZUR41cY7W6zRSdpSUJ7I7mLYBKOCmB"
	browserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126 Safari/537.36"
)

type Client struct{ http *http.Client }

type Credential struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	AuthID       string `json:"authId,omitempty"`
	Email        string `json:"email,omitempty"`
}

type Challenge struct {
	UUID      string
	Verifier  string
	VerifyURL string
	ExpiresAt time.Time
}

type Usage struct {
	Plan        string
	CycleEnd    *time.Time
	Remaining   float64
	Used        *float64
	Limit       *float64
	OnDemand    *float64
	OnDemandMax *float64
}

type pollResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	AuthID       string `json:"authId"`
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(30 * time.Second)} }

func StartLogin() (Challenge, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Challenge{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	id := uuid.NewString()
	values := url.Values{"challenge": {challenge}, "uuid": {id}, "mode": {"login"}}
	return Challenge{UUID: id, Verifier: verifier, VerifyURL: loginURL + "?" + values.Encode(), ExpiresAt: time.Now().Add(5 * time.Minute)}, nil
}

func (c *Client) PollLogin(ctx context.Context, challenge Challenge) (Credential, bool, error) {
	values := url.Values{"uuid": {challenge.UUID}, "verifier": {challenge.Verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL+"?"+values.Encode(), nil)
	if err != nil {
		return Credential{}, false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Credential{}, false, fmt.Errorf("连接 Cursor 官方登录接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusAccepted {
		return Credential{}, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credential{}, false, fmt.Errorf("Cursor 登录轮询返回 HTTP %d", resp.StatusCode)
	}
	var payload pollResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Credential{}, false, fmt.Errorf("解析 Cursor 登录响应失败: %w", err)
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" {
		return Credential{}, true, nil
	}
	email := ""
	if strings.Contains(payload.AuthID, "@") {
		email = payload.AuthID
	}
	return Credential{AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, AuthID: payload.AuthID, Email: email}, false, nil
}

func (c *Client) FetchUsage(ctx context.Context, credential *Credential) (Usage, bool, error) {
	usage, status, err := c.fetchUsage(ctx, credential.AccessToken)
	if err == nil {
		return usage, false, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return Usage{}, false, err
	}
	if credential.RefreshToken == "" {
		return Usage{}, false, fmt.Errorf("Cursor 登录已过期且没有 refresh_token，请重新授权")
	}
	refreshed, refreshErr := c.Refresh(ctx, credential.RefreshToken)
	if refreshErr != nil {
		return Usage{}, false, fmt.Errorf("Cursor 登录已过期，刷新失败: %w", refreshErr)
	}
	credential.AccessToken = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		credential.RefreshToken = refreshed.RefreshToken
	}
	usage, _, err = c.fetchUsage(ctx, credential.AccessToken)
	return usage, true, err
}

func (c *Client) fetchUsage(ctx context.Context, accessToken string) (Usage, int, error) {
	userID := workOSUserID(accessToken)
	if userID == "" {
		return Usage{}, 0, fmt.Errorf("无法从 Cursor access_token 解析 WorkOS 用户 ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return Usage{}, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+url.QueryEscape(userID+"::"+accessToken))
	req.Header.Set("Referer", "https://www.cursor.com/settings")
	req.Header.Set("User-Agent", browserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, 0, fmt.Errorf("连接 Cursor 官方用量接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, resp.StatusCode, fmt.Errorf("Cursor 用量接口返回 HTTP %d", resp.StatusCode)
	}
	var root struct {
		BillingCycleEnd string `json:"billingCycleEnd"`
		MembershipType  string `json:"membershipType"`
		IndividualUsage struct {
			Plan struct {
				Used             *float64 `json:"used"`
				Limit            *float64 `json:"limit"`
				Remaining        *float64 `json:"remaining"`
				TotalPercentUsed *float64 `json:"totalPercentUsed"`
			} `json:"plan"`
			OnDemand struct {
				Enabled   bool     `json:"enabled"`
				Used      *float64 `json:"used"`
				Limit     *float64 `json:"limit"`
				Remaining *float64 `json:"remaining"`
			} `json:"onDemand"`
		} `json:"individualUsage"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return Usage{}, resp.StatusCode, fmt.Errorf("解析 Cursor 用量响应失败: %w", err)
	}
	remaining := -1.0
	if root.IndividualUsage.Plan.TotalPercentUsed != nil {
		remaining = 100 - *root.IndividualUsage.Plan.TotalPercentUsed
	} else if root.IndividualUsage.Plan.Remaining != nil && root.IndividualUsage.Plan.Limit != nil && *root.IndividualUsage.Plan.Limit > 0 {
		remaining = *root.IndividualUsage.Plan.Remaining / *root.IndividualUsage.Plan.Limit * 100
	} else if root.IndividualUsage.Plan.Used != nil && root.IndividualUsage.Plan.Limit != nil && *root.IndividualUsage.Plan.Limit > 0 {
		remaining = 100 - *root.IndividualUsage.Plan.Used / *root.IndividualUsage.Plan.Limit * 100
	}
	if remaining < 0 {
		return Usage{}, resp.StatusCode, fmt.Errorf("Cursor 用量响应缺少 plan 百分比或 used/limit 字段")
	}
	if remaining > 100 {
		remaining = 100
	}
	var cycleEnd *time.Time
	if parsed, err := time.Parse(time.RFC3339, root.BillingCycleEnd); err == nil {
		parsed = parsed.UTC()
		cycleEnd = &parsed
	}
	return Usage{Plan: root.MembershipType, CycleEnd: cycleEnd, Remaining: remaining, Used: root.IndividualUsage.Plan.Used, Limit: root.IndividualUsage.Plan.Limit, OnDemand: root.IndividualUsage.OnDemand.Remaining, OnDemandMax: root.IndividualUsage.OnDemand.Limit}, resp.StatusCode, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (Credential, error) {
	body, _ := json.Marshal(map[string]string{"grant_type": "refresh_token", "client_id": clientID, "refresh_token": refreshToken})
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
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("Cursor token 刷新返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccessCamel  string `json:"accessToken"`
		RefreshCamel string `json:"refreshToken"`
		ShouldLogout bool   `json:"shouldLogout"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Credential{}, err
	}
	if payload.ShouldLogout {
		return Credential{}, fmt.Errorf("Cursor refresh_token 已失效，请重新登录")
	}
	access := first(payload.AccessToken, payload.AccessCamel)
	if access == "" {
		return Credential{}, fmt.Errorf("Cursor token 刷新响应缺少 access_token")
	}
	return Credential{AccessToken: access, RefreshToken: first(payload.RefreshToken, payload.RefreshCamel, refreshToken)}, nil
}

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("Cursor 认证文件为空或超过 1 MB")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return Credential{}, fmt.Errorf("Cursor 认证文件不是有效 JSON: %w", err)
	}
	var result Credential
	walk(value, func(m map[string]any) bool {
		result.AccessToken = mapString(m, "accessToken", "access_token", "cursor_access_token")
		if result.AccessToken == "" {
			return false
		}
		result.RefreshToken = mapString(m, "refreshToken", "refresh_token", "cursor_refresh_token")
		result.AuthID = mapString(m, "authId", "auth_id", "cachedAuthId")
		result.Email = mapString(m, "email", "cachedEmail")
		return true
	})
	if result.AccessToken == "" {
		return Credential{}, fmt.Errorf("Cursor 认证文件缺少 accessToken")
	}
	return result, nil
}

func workOSUserID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	if index := strings.LastIndex(sub, "|"); index >= 0 {
		sub = sub[index+1:]
	}
	if strings.HasPrefix(sub, "user_") {
		return sub
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
func mapString(m map[string]any, keys ...string) string {
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
