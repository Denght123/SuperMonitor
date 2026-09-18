package mimo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const usageURL = "https://platform.xiaomimimo.com/api/v1/tokenPlan/usage"
const detailURL = "https://platform.xiaomimimo.com/api/v1/tokenPlan/detail"

type Client struct{ http *http.Client }
type Credential struct {
	Cookie string `json:"cookie"`
}
type Usage struct {
	Used, Limit, RemainingPercent float64
	ResetAt                       *time.Time
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func NormalizeCookie(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if index := strings.Index(strings.ToLower(text), "cookie:"); index >= 0 {
		text = text[index+len("cookie:"):]
	}
	if line, _, ok := strings.Cut(text, "\n"); ok {
		text = line
	}
	allowed := map[string]bool{"api-platform_ph": true, "api-platform_serviceToken": true, "api-platform_slh": true, "userId": true}
	values := map[string]string{}
	for _, pair := range strings.Split(strings.Trim(text, " '\"`\\"), ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && allowed[name] && strings.TrimSpace(value) != "" {
			values[name] = strings.Trim(strings.TrimSpace(value), "'\"")
		}
	}
	if values["api-platform_serviceToken"] == "" || values["userId"] == "" {
		return "", fmt.Errorf("MiMo Cookie 缺少 api-platform_serviceToken 或 userId")
	}
	order := []string{"api-platform_ph", "api-platform_serviceToken", "api-platform_slh", "userId"}
	parts := make([]string, 0, len(values))
	for _, name := range order {
		if values[name] != "" {
			parts = append(parts, name+"="+values[name])
		}
	}
	return strings.Join(parts, "; "), nil
}

func (c *Client) FetchUsage(ctx context.Context, credential Credential) (Usage, error) {
	cookie, err := NormalizeCookie(credential.Cookie)
	if err != nil {
		return Usage{}, err
	}
	usage, err := c.fetch(ctx, usageURL, cookie)
	if err != nil {
		return Usage{}, err
	}
	detail, _ := c.fetch(ctx, detailURL, cookie)
	item, ok := nestedMap(usage, "data", "monthUsage")
	if !ok {
		return Usage{}, fmt.Errorf("MiMo usage 响应缺少 monthUsage")
	}
	items, _ := item["items"].([]any)
	if len(items) == 0 {
		return Usage{}, fmt.Errorf("MiMo usage 响应缺少 monthUsage.items")
	}
	row, _ := items[0].(map[string]any)
	used, _ := number(row["used"])
	limit, _ := number(row["limit"])
	percent, hasPercent := number(row["percent"])
	if !hasPercent && limit > 0 {
		percent = used / limit
		hasPercent = true
	}
	if !hasPercent {
		return Usage{}, fmt.Errorf("MiMo usage 响应缺少 percent/used/limit")
	}
	if percent <= 1 {
		percent *= 100
	}
	result := Usage{Used: used, Limit: limit, RemainingPercent: clamp(100 - percent)}
	if data, ok := nestedMap(detail, "data"); ok {
		if raw, ok := data["currentPeriodEnd"].(string); ok {
			for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
				if value, parseErr := time.ParseInLocation(layout, raw, time.Local); parseErr == nil {
					utc := value.UTC()
					result.ResetAt = &utc
					break
				}
			}
		}
	}
	return result, nil
}

func (c *Client) fetch(ctx context.Context, url, cookie string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", "https://platform.xiaomimimo.com")
	req.Header.Set("Referer", "https://platform.xiaomimimo.com/#/console/balance")
	req.Header.Set("x-timeZone", "Asia/Shanghai")
	req.Header.Set("User-Agent", "Mozilla/5.0 SuperMonitor/0.5.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MiMo usage 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("MiMo 登录态已失效，请重新复制 Cookie")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("MiMo Cookie 无效或权限不足")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MiMo usage 接口返回 HTTP %d", resp.StatusCode)
	}
	var value map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return nil, fmt.Errorf("MiMo usage JSON 解析失败: %w", err)
	}
	if code, ok := number(value["code"]); ok && code != 0 {
		return nil, fmt.Errorf("MiMo usage 返回业务错误 %.0f", code)
	}
	return value, nil
}

func nestedMap(root map[string]any, path ...string) (map[string]any, bool) {
	current := root
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}
func number(value any) (float64, bool) {
	switch item := value.(type) {
	case float64:
		return item, true
	case json.Number:
		result, err := item.Float64()
		return result, err == nil
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		return result, err == nil
	}
	return 0, false
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
