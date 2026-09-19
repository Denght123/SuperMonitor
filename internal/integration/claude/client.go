package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const usageURL = "https://api.anthropic.com/api/oauth/usage"

type Client struct{ http *http.Client }

type Credential struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	ExpiresAt        int64  `json:"expiresAt,omitempty"`
}

type Window struct {
	Label            string
	UsedPercent      float64
	RemainingPercent float64
	ResetAt          *time.Time
}

type Usage struct {
	Plan    string
	Windows []Window
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("Claude 认证文件为空或超过 1 MB")
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Credential{}, fmt.Errorf("Claude 认证文件不是有效 JSON: %w", err)
	}
	value := root
	for _, key := range []string{"claudeAiOauth", "claude.ai_oauth", "oauth", "credentials"} {
		if nested, ok := root[key].(map[string]any); ok {
			value = nested
			break
		}
	}
	credential := Credential{
		AccessToken:      firstString(value, "accessToken", "access_token"),
		RefreshToken:     firstString(value, "refreshToken", "refresh_token"),
		SubscriptionType: firstString(value, "subscriptionType", "subscription_type"),
	}
	if expires, ok := intValue(value["expiresAt"]); ok {
		credential.ExpiresAt = expires
	} else if expires, ok := intValue(value["expires_at"]); ok {
		credential.ExpiresAt = expires
	}
	if credential.AccessToken == "" {
		return Credential{}, fmt.Errorf("Claude 认证文件缺少 claudeAiOauth.accessToken；请导入 Claude Code 的 .credentials.json")
	}
	return credential, nil
}

func (c *Client) FetchUsage(ctx context.Context, credential Credential) (Usage, error) {
	token := strings.TrimSpace(credential.AccessToken)
	if token == "" {
		return Usage{}, fmt.Errorf("Claude OAuth access token 不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1 SuperMonitor/0.6.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, fmt.Errorf("连接 Claude Code 用量接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Usage{}, fmt.Errorf("读取 Claude Code 用量响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Usage{}, fmt.Errorf("Claude Code OAuth 已失效，请在 Claude Code 中重新登录并导入新的 .credentials.json")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Usage{}, fmt.Errorf("Claude Code 用量接口请求过于频繁，请稍后重试")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, fmt.Errorf("Claude Code 用量接口返回 HTTP %d", resp.StatusCode)
	}
	return parseUsage(body, credential.SubscriptionType)
}

func parseUsage(body []byte, plan string) (Usage, error) {
	var payload struct {
		FiveHour *struct {
			Utilization any    `json:"utilization"`
			ResetsAt    string `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			Utilization any    `json:"utilization"`
			ResetsAt    string `json:"resets_at"`
		} `json:"seven_day"`
		Limits []struct {
			Kind     string `json:"kind"`
			Percent  any    `json:"percent"`
			ResetsAt string `json:"resets_at"`
			IsActive bool   `json:"is_active"`
			Scope    struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, fmt.Errorf("Claude Code 用量响应不是有效 JSON: %w", err)
	}
	usage := Usage{Plan: normalizePlan(plan)}
	appendWindow := func(label string, utilization any, reset string) {
		used, ok := floatValue(utilization)
		if !ok {
			return
		}
		usage.Windows = append(usage.Windows, Window{Label: label, UsedPercent: clamp(used), RemainingPercent: clamp(100 - used), ResetAt: parseTime(reset)})
	}
	if payload.FiveHour != nil {
		appendWindow("5 小时限额", payload.FiveHour.Utilization, payload.FiveHour.ResetsAt)
	}
	if payload.SevenDay != nil {
		appendWindow("周限额", payload.SevenDay.Utilization, payload.SevenDay.ResetsAt)
	}
	for _, item := range payload.Limits {
		if item.Kind != "weekly_scoped" || item.Scope.Model == nil || strings.TrimSpace(item.Scope.Model.DisplayName) == "" {
			continue
		}
		appendWindow("周限额 · "+strings.TrimSpace(item.Scope.Model.DisplayName), item.Percent, item.ResetsAt)
	}
	if len(usage.Windows) == 0 {
		return Usage{}, fmt.Errorf("Claude Code 用量响应缺少可识别的额度窗口")
	}
	return usage, nil
}

func firstString(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := root[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func intValue(value any) (int64, bool) {
	result, ok := floatValue(value)
	return int64(result), ok
}

func floatValue(value any) (float64, bool) {
	switch item := value.(type) {
	case json.Number:
		result, err := item.Float64()
		return result, err == nil
	case float64:
		return item, true
	}
	return 0, false
}

func parseTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func normalizePlan(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	for _, plan := range []string{"max", "pro", "team"} {
		if strings.Contains(text, plan) {
			return strings.ToUpper(plan[:1]) + plan[1:]
		}
	}
	return strings.TrimSpace(value)
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
