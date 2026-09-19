package coze

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const (
	userInfoURL = "https://www.coze.cn/api/user/info"
	balanceURL  = "https://www.coze.cn/api/marketplace/trade/credit/balance"
)

type Client struct{ http *http.Client }
type Credential struct {
	Cookie string `json:"cookie"`
}
type Balance struct {
	UserID    string
	Nickname  string
	Remaining float64
	Total     *float64
	ExpiresAt *time.Time
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(30 * time.Second)} }

func NormalizeCookie(raw string) (Credential, error) {
	cookie := strings.TrimSpace(raw)
	if cookie == "" || !strings.Contains(cookie, "=") {
		return Credential{}, fmt.Errorf("请粘贴登录 www.coze.cn 后的完整 Cookie")
	}
	return Credential{Cookie: cookie}, nil
}

func (c *Client) FetchBalance(ctx context.Context, credential Credential) (Balance, error) {
	info, err := c.getJSON(ctx, userInfoURL, credential.Cookie)
	if err != nil {
		return Balance{}, fmt.Errorf("读取扣子账号信息失败: %w", err)
	}
	userID := findString(info, "user_id_str", "userId", "user_id", "id")
	if userID == "" {
		return Balance{}, fmt.Errorf("扣子登录态有效，但账号响应缺少 user_id_str")
	}
	values := url.Values{"account_type": {"2"}, "coze_account_id": {userID}}
	root, err := c.getJSON(ctx, balanceURL+"?"+values.Encode(), credential.Cookie)
	if err != nil {
		return Balance{}, fmt.Errorf("读取扣子积分失败: %w", err)
	}
	remaining, ok := findNumber(root, "credit_balance", "balance", "available_balance", "available_credit", "remaining", "amount")
	if !ok {
		return Balance{}, fmt.Errorf("扣子积分接口响应缺少可识别的 balance/remaining 字段")
	}
	result := Balance{UserID: userID, Nickname: findString(info, "name", "nickname", "user_name"), Remaining: remaining}
	if total, found := findNumber(root, "total_credit", "total_credits", "total", "credit_total"); found && total > 0 {
		result.Total = &total
	}
	if expires, found := findTime(root, "expire_time", "expires_at", "expiration_time", "valid_until"); found {
		result.ExpiresAt = expires
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint, cookie string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://www.coze.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126 Safari/537.36")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("官方接口返回 HTTP %d", resp.StatusCode)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("官方接口响应不是有效 JSON")
	}
	if code, found := findNumber(root, "code"); found && code != 0 {
		return nil, fmt.Errorf("官方接口拒绝登录态（code %.0f）：%s", code, findString(root, "msg", "message"))
	}
	return root, nil
}

func findString(root any, keys ...string) string {
	var result string
	walk(root, func(m map[string]any) bool {
		for _, key := range keys {
			if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
				result = strings.TrimSpace(value)
				return true
			}
		}
		return false
	})
	return result
}
func findNumber(root any, keys ...string) (float64, bool) {
	var result float64
	found := walk(root, func(m map[string]any) bool {
		for _, key := range keys {
			switch value := m[key].(type) {
			case float64:
				result = value
				return true
			case string:
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					result = parsed
					return true
				}
			}
		}
		return false
	})
	return result, found
}
func findTime(root any, keys ...string) (*time.Time, bool) {
	var result time.Time
	found := walk(root, func(m map[string]any) bool {
		for _, key := range keys {
			switch value := m[key].(type) {
			case float64:
				seconds := int64(value)
				if seconds > 1e12 {
					seconds /= 1000
				}
				if seconds > 0 {
					result = time.Unix(seconds, 0).UTC()
					return true
				}
			case string:
				if parsed, err := time.Parse(time.RFC3339, value); err == nil {
					result = parsed.UTC()
					return true
				}
			}
		}
		return false
	})
	if !found {
		return nil, false
	}
	return &result, true
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
