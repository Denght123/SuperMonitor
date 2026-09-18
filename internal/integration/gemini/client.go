package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const (
	loadURL    = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	quotaURL   = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	refreshURL = "https://oauth2.googleapis.com/token"
)

type Client struct{ http *http.Client }

type Credential struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiryDate   int64  `json:"expiryDate,omitempty"`
}

type Window struct {
	Label            string
	RemainingPercent float64
	ResetAt          *time.Time
}

type Usage struct{ Windows []Window }

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("Gemini 认证文件为空或超过 1 MB")
	}
	var value struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiryDate   int64  `json:"expiry_date"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return Credential{}, fmt.Errorf("Gemini 认证文件不是有效 JSON: %w", err)
	}
	if strings.TrimSpace(value.AccessToken) == "" && strings.TrimSpace(value.RefreshToken) == "" {
		return Credential{}, fmt.Errorf("Gemini 认证文件缺少 access_token 或 refresh_token；请导入 Gemini CLI 的 oauth_creds.json")
	}
	return Credential{AccessToken: strings.TrimSpace(value.AccessToken), RefreshToken: strings.TrimSpace(value.RefreshToken), ExpiryDate: value.ExpiryDate}, nil
}

func (c *Client) FetchUsage(ctx context.Context, credential *Credential) (Usage, bool, error) {
	refreshed := false
	if credential.AccessToken == "" || (credential.ExpiryDate > 0 && credential.ExpiryDate <= time.Now().UnixMilli()) {
		if credential.RefreshToken == "" {
			return Usage{}, false, fmt.Errorf("Gemini 登录已过期且认证文件没有 refresh_token，请在 Gemini CLI 中重新登录")
		}
		accessToken, expiry, err := c.refresh(ctx, credential.RefreshToken)
		if err != nil {
			return Usage{}, false, err
		}
		credential.AccessToken = accessToken
		credential.ExpiryDate = expiry
		refreshed = true
	}
	usage, status, err := c.fetch(ctx, credential.AccessToken)
	if err == nil {
		return usage, refreshed, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden || credential.RefreshToken == "" || refreshed {
		return Usage{}, refreshed, err
	}
	accessToken, expiry, refreshErr := c.refresh(ctx, credential.RefreshToken)
	if refreshErr != nil {
		return Usage{}, false, fmt.Errorf("Gemini access_token 已失效且 refresh_token 无法刷新，请在 Gemini CLI 中重新登录")
	}
	credential.AccessToken = accessToken
	credential.ExpiryDate = expiry
	usage, _, err = c.fetch(ctx, credential.AccessToken)
	return usage, true, err
}

func (c *Client) fetch(ctx context.Context, token string) (Usage, int, error) {
	projectPayload, _ := json.Marshal(map[string]any{"metadata": map[string]string{"ideType": "GEMINI_CLI", "pluginType": "GEMINI"}})
	projectBody, status, err := c.postJSON(ctx, loadURL, token, projectPayload)
	if err != nil {
		return Usage{}, status, err
	}
	var projectResponse map[string]any
	if err := json.Unmarshal(projectBody, &projectResponse); err != nil {
		return Usage{}, status, fmt.Errorf("Gemini 项目信息响应不是有效 JSON: %w", err)
	}
	project := projectID(projectResponse["cloudaicompanionProject"])
	quotaPayload := map[string]string{}
	if project != "" {
		quotaPayload["project"] = project
	}
	body, _ := json.Marshal(quotaPayload)
	quotaBody, status, err := c.postJSON(ctx, quotaURL, token, body)
	if err != nil {
		return Usage{}, status, err
	}
	return parseUsage(quotaBody)
}

func (c *Client) postJSON(ctx context.Context, endpoint, token string, payload []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("连接 Gemini Code Assist 额度接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取 Gemini Code Assist 响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, resp.StatusCode, fmt.Errorf("Gemini CLI OAuth 已失效，请重新登录")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("Gemini Code Assist 额度接口返回 HTTP %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

func (c *Client) refresh(ctx context.Context, refreshToken string) (string, int64, error) {
	clientID := strings.TrimSpace(os.Getenv("SUPMON_GEMINI_OAUTH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("SUPMON_GEMINI_OAUTH_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		return "", 0, fmt.Errorf("Gemini access_token 已过期；请在部署环境配置 SUPMON_GEMINI_OAUTH_CLIENT_ID 与 SUPMON_GEMINI_OAUTH_CLIENT_SECRET，或在 Gemini CLI 重新登录后导入最新 oauth_creds.json")
	}
	form := url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "refresh_token": {refreshToken}, "grant_type": {"refresh_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("刷新 Gemini OAuth 失败: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("Gemini OAuth 刷新返回 HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return "", 0, fmt.Errorf("Gemini OAuth 刷新响应无效")
	}
	return payload.AccessToken, time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli(), nil
}

func parseUsage(body []byte) (Usage, int, error) {
	var payload struct {
		Buckets []struct {
			ModelID           string `json:"modelId"`
			RemainingFraction any    `json:"remainingFraction"`
			ResetTime         string `json:"resetTime"`
		} `json:"buckets"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, http.StatusOK, fmt.Errorf("Gemini 额度响应不是有效 JSON: %w", err)
	}
	type group struct {
		remaining float64
		reset     *time.Time
	}
	groups := map[string]group{}
	for _, bucket := range payload.Buckets {
		label := modelLabel(bucket.ModelID)
		if label == "" {
			continue
		}
		remaining, ok := floatValue(bucket.RemainingFraction)
		if !ok {
			remaining = 1
		}
		remaining = clamp(remaining * 100)
		current, exists := groups[label]
		if !exists || remaining < current.remaining {
			groups[label] = group{remaining: remaining, reset: parseTime(bucket.ResetTime)}
		}
	}
	if len(groups) == 0 {
		return Usage{}, http.StatusOK, fmt.Errorf("Gemini 额度响应缺少 buckets")
	}
	labels := make([]string, 0, len(groups))
	for label := range groups {
		labels = append(labels, label)
	}
	sort.SliceStable(labels, func(i, j int) bool { return modelRank(labels[i]) < modelRank(labels[j]) })
	usage := Usage{Windows: make([]Window, 0, len(labels))}
	for _, label := range labels {
		item := groups[label]
		usage.Windows = append(usage.Windows, Window{Label: label + " 模型额度", RemainingPercent: item.remaining, ResetAt: item.reset})
	}
	return usage, http.StatusOK, nil
}

func projectID(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"id", "projectId"} {
			if text, ok := object[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func modelLabel(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(text, "flash-lite"):
		return "Flash Lite"
	case strings.Contains(text, "flash"):
		return "Flash"
	case strings.Contains(text, "pro"):
		return "Pro"
	case text != "":
		return value
	default:
		return ""
	}
}

func modelRank(value string) int {
	switch value {
	case "Pro":
		return 0
	case "Flash":
		return 1
	case "Flash Lite":
		return 2
	default:
		return 3
	}
}
func floatValue(value any) (float64, bool) {
	switch item := value.(type) {
	case json.Number:
		result, err := item.Float64()
		return result, err == nil
	case float64:
		return item, true
	default:
		return 0, false
	}
}
func parseTime(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
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
