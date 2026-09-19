package zhipu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const (
	defaultQuotaURL         = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
	defaultAccountReportURL = "https://open.bigmodel.cn/api/biz/account/query-customer-account-report"
	defaultModelsURL        = "https://open.bigmodel.cn/api/paas/v4/models"
)

type Client struct {
	http             *http.Client
	quotaURL         string
	accountReportURL string
	modelsURL        string
}

type Credential struct {
	APIKey string `json:"apiKey"`
}

type Window struct {
	Label                  string
	Kind, Unit             string
	Used, Total, Remaining float64
	RemainingPercent       float64
	HasRemainingPercent    bool
	ResetAt                *time.Time
}

type Usage struct {
	Plan           string
	Windows        []Window
	ModelCount     int
	QuotaAvailable bool
	Source         string
}

func NewClient() *Client {
	return &Client{
		http:             netutil.NewHTTPClient(20 * time.Second),
		quotaURL:         defaultQuotaURL,
		accountReportURL: defaultAccountReportURL,
		modelsURL:        defaultModelsURL,
	}
}

func (c *Client) FetchUsage(ctx context.Context, credential Credential) (Usage, error) {
	apiKey := strings.TrimSpace(credential.APIKey)
	if apiKey == "" {
		return Usage{}, fmt.Errorf("智谱 API Key 不能为空")
	}

	// Coding Plan and the ordinary metered API are separate billing domains.
	// Probe both before deciding that a key is invalid so an ordinary API key is
	// never rejected merely because the account has no Coding Plan subscription.
	if usage, err := c.fetchCodingPlan(ctx, apiKey); err == nil {
		return usage, nil
	}
	if usage, err := c.fetchAccountBalance(ctx, apiKey); err == nil {
		return usage, nil
	}
	usage, err := c.fetchModels(ctx, apiKey)
	if err != nil {
		return Usage{}, err
	}
	return usage, nil
}

func (c *Client) fetchCodingPlan(ctx context.Context, apiKey string) (Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.quotaURL, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, fmt.Errorf("连接智谱额度接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Usage{}, fmt.Errorf("读取智谱额度响应失败: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		usage, parseErr := parseUsage(body)
		if parseErr == nil {
			usage.QuotaAvailable = true
			usage.Source = "智谱 /api/monitor/usage/quota/limit"
			return usage, nil
		}
		return Usage{}, parseErr
	}
	if isNoCodingPlanBody(body) {
		return Usage{}, fmt.Errorf("智谱账号未订阅 Coding Plan")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Usage{}, fmt.Errorf("智谱 Coding Plan 接口未接受该凭据")
	}
	return Usage{}, fmt.Errorf("智谱额度接口返回 HTTP %d", resp.StatusCode)
}

func (c *Client) fetchAccountBalance(ctx context.Context, apiKey string) (Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.accountReportURL, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, fmt.Errorf("连接智谱按量余额接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Usage{}, fmt.Errorf("读取智谱按量余额响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Usage{}, fmt.Errorf("智谱按量余额接口未接受该 API Key")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, fmt.Errorf("智谱按量余额接口返回 HTTP %d", resp.StatusCode)
	}
	return parseAccountBalance(body)
}

func (c *Client) fetchModels(ctx context.Context, apiKey string) (Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.modelsURL, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, fmt.Errorf("连接智谱开放平台模型接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Usage{}, fmt.Errorf("读取智谱模型响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Usage{}, fmt.Errorf("智谱 API Key 已失效或无权访问，请重新复制开放平台 API Key")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, fmt.Errorf("智谱模型接口返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Usage{}, fmt.Errorf("智谱模型响应不是有效 JSON: %w", err)
	}
	return Usage{
		Plan:       fmt.Sprintf("开放平台 API Key · %d 个可用模型", len(payload.Data)),
		ModelCount: len(payload.Data),
		Source:     "智谱官方 /api/paas/v4/models",
	}, nil
}

func isNoCodingPlan(err error) bool {
	return err != nil && isNoCodingPlanText(err.Error())
}

func isNoCodingPlanBody(body []byte) bool { return isNoCodingPlanText(string(body)) }

func isNoCodingPlanText(message string) bool {
	message = strings.ToLower(message)
	compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "_", "", "-", "").Replace(message)
	return strings.Contains(compact, "不存在codingplan") || strings.Contains(compact, "notexistcodingplan") || strings.Contains(compact, "nocodingplan")
}

func parseAccountBalance(body []byte) (Usage, error) {
	var payload struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
		Success *bool  `json:"success"`
		Data    struct {
			AvailableBalance any `json:"availableBalance"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, fmt.Errorf("智谱按量余额响应不是有效 JSON: %w", err)
	}
	code, hasCode := number(payload.Code)
	if payload.Success != nil && !*payload.Success || hasCode && code != 0 && code != 200 {
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = strings.TrimSpace(payload.Msg)
		}
		if message == "" {
			message = "认证或业务校验失败"
		}
		return Usage{}, fmt.Errorf("智谱按量余额接口返回业务错误: %s", message)
	}
	available, ok := number(payload.Data.AvailableBalance)
	if !ok {
		return Usage{}, fmt.Errorf("智谱按量余额响应缺少 data.availableBalance")
	}
	return Usage{
		Plan:   "开放平台按量计费",
		Source: "智谱 /api/biz/account/query-customer-account-report",
		Windows: []Window{{
			Label:     "账户可用余额",
			Kind:      "balance",
			Unit:      "CNY",
			Remaining: available,
		}},
	}, nil
}

func parseUsage(body []byte) (Usage, error) {
	var payload struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
		Success *bool  `json:"success"`
		Data    struct {
			Level  string `json:"level"`
			Limits []struct {
				Type          string `json:"type"`
				Unit          int    `json:"unit"`
				Usage         any    `json:"usage"`
				CurrentValue  any    `json:"currentValue"`
				Remaining     any    `json:"remaining"`
				Percentage    any    `json:"percentage"`
				NextResetTime any    `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, fmt.Errorf("智谱额度响应不是有效 JSON: %w", err)
	}
	code, hasCode := number(payload.Code)
	if payload.Success != nil && !*payload.Success || hasCode && code != 0 && code != 200 {
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = strings.TrimSpace(payload.Msg)
		}
		if message == "" {
			message = "认证或业务校验失败"
		}
		if code == 401 {
			return Usage{}, fmt.Errorf("智谱 API Key 已失效，请重新复制开放平台 API Key")
		}
		return Usage{}, fmt.Errorf("智谱额度接口返回业务错误: %s", message)
	}
	usage := Usage{Plan: strings.TrimSpace(payload.Data.Level)}
	var fallback []Window
	for _, item := range payload.Data.Limits {
		if !strings.EqualFold(item.Type, "TOKENS_LIMIT") && !strings.EqualFold(item.Type, "CREDIT_LIMIT") {
			continue
		}
		usedPercent, _ := number(item.Percentage)
		total, _ := number(item.Usage)
		used, _ := number(item.CurrentValue)
		remaining, hasRemaining := number(item.Remaining)
		if !hasRemaining && total > 0 {
			remaining = total - used
		}
		remainingPercent := clamp(100 - usedPercent)
		kind, unit := "rate_window", "%"
		if strings.EqualFold(item.Type, "CREDIT_LIMIT") {
			kind, unit = "credits", "credits"
		}
		window := Window{Kind: kind, Unit: unit, Used: used, Total: total, Remaining: remaining, RemainingPercent: remainingPercent, HasRemainingPercent: true, ResetAt: millisTime(item.NextResetTime)}
		switch item.Unit {
		case 3:
			window.Label = "5 小时限额"
		case 6:
			window.Label = "周限额"
		default:
			fallback = append(fallback, window)
			continue
		}
		usage.Windows = append(usage.Windows, window)
	}
	for _, window := range fallback {
		if !hasLabel(usage.Windows, "5 小时限额") {
			window.Label = "5 小时限额"
		} else if !hasLabel(usage.Windows, "周限额") {
			window.Label = "周限额"
		} else {
			break
		}
		usage.Windows = append(usage.Windows, window)
	}
	if len(usage.Windows) == 0 {
		return Usage{}, fmt.Errorf("智谱额度响应缺少可识别的套餐窗口")
	}
	return usage, nil
}

func hasLabel(windows []Window, label string) bool {
	for _, window := range windows {
		if window.Label == label {
			return true
		}
	}
	return false
}

func number(value any) (float64, bool) {
	switch item := value.(type) {
	case json.Number:
		result, err := item.Float64()
		return result, err == nil
	case float64:
		return item, true
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func millisTime(value any) *time.Time {
	millis, ok := number(value)
	if !ok || millis <= 0 {
		return nil
	}
	result := time.UnixMilli(int64(millis)).UTC()
	return &result
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
