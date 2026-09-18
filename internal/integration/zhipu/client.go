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

const quotaURL = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"

type Client struct{ http *http.Client }

type Credential struct {
	APIKey string `json:"apiKey"`
}

type Window struct {
	Label                  string
	Kind, Unit             string
	Used, Total, Remaining float64
	RemainingPercent       float64
	ResetAt                *time.Time
}

type Usage struct {
	Plan    string
	Windows []Window
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func (c *Client) FetchUsage(ctx context.Context, credential Credential) (Usage, error) {
	apiKey := strings.TrimSpace(credential.APIKey)
	if apiKey == "" {
		return Usage{}, fmt.Errorf("智谱 API Key 不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quotaURL, nil)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, fmt.Errorf("智谱额度接口返回 HTTP %d", resp.StatusCode)
	}
	return parseUsage(body)
}

func parseUsage(body []byte) (Usage, error) {
	var payload struct {
		Code    any    `json:"code"`
		Message string `json:"msg"`
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
	if payload.Success != nil && !*payload.Success {
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = "认证或业务校验失败"
		}
		if code, _ := number(payload.Code); code == 401 {
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
		window := Window{Kind: kind, Unit: unit, Used: used, Total: total, Remaining: remaining, RemainingPercent: remainingPercent, ResetAt: millisTime(item.NextResetTime)}
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
