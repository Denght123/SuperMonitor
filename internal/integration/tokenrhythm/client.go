package tokenrhythm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const usageURL = "https://tokenrhythm.studio/api/usage-summary"

type Client struct{ http *http.Client }

type Credential struct {
	SessionToken string `json:"sessionToken"`
	RefDevice    string `json:"refDevice,omitempty"`
}

type Usage struct {
	AvailableBalance float64
	Cost             float64
	InputTokens      int64
	OutputTokens     int64
	Calls            int64
	ExpiresAt        *time.Time
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func NormalizeCredential(raw string) (Credential, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Credential{}, fmt.Errorf("基元律动登录凭据不能为空")
	}
	tokenMatch := regexp.MustCompile(`(?:tr_session=)?(sess_[A-Za-z0-9_-]+?)(?:----|[;,\s|]|$)`).FindStringSubmatch(text)
	if len(tokenMatch) < 2 {
		return Credential{}, fmt.Errorf("未识别到基元律动 sess_ 会话令牌；请从 tokenrhythm.studio 登录态中复制")
	}
	credential := Credential{SessionToken: tokenMatch[1]}
	if refMatch := regexp.MustCompile(`tr_ref_device=([A-Za-z0-9_-]+)`).FindStringSubmatch(text); len(refMatch) >= 2 {
		credential.RefDevice = refMatch[1]
	}
	return credential, nil
}

func (c *Client) FetchUsage(ctx context.Context, credential Credential) (Usage, error) {
	token := strings.TrimSpace(credential.SessionToken)
	if token == "" {
		return Usage{}, fmt.Errorf("基元律动 sess_ 会话令牌不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "tr_session="+token+"; tr_ref_device="+strings.TrimSpace(credential.RefDevice))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 SuperMonitor/0.6.2")
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, fmt.Errorf("连接基元律动余额接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Usage{}, fmt.Errorf("读取基元律动余额响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Usage{}, fmt.Errorf("基元律动登录态已失效，请重新复制 sess_ 会话令牌")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, fmt.Errorf("基元律动余额接口返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			AvailableBalance any    `json:"availableBalanceCny"`
			Cost             any    `json:"costCny"`
			InputTokens      any    `json:"inputTokens"`
			OutputTokens     any    `json:"outputTokens"`
			Calls            any    `json:"calls"`
			NextExpiryAt     string `json:"nextExpiryAt"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return Usage{}, fmt.Errorf("基元律动余额响应不是有效 JSON: %w", err)
	}
	if code, ok := number(payload.Code); ok && code != 0 {
		return Usage{}, fmt.Errorf("基元律动余额接口返回业务错误 %.0f", code)
	}
	balance, ok := number(payload.Data.AvailableBalance)
	if !ok {
		return Usage{}, fmt.Errorf("基元律动余额响应缺少 availableBalanceCny")
	}
	cost, _ := number(payload.Data.Cost)
	input, _ := number(payload.Data.InputTokens)
	output, _ := number(payload.Data.OutputTokens)
	calls, _ := number(payload.Data.Calls)
	return Usage{AvailableBalance: balance, Cost: cost, InputTokens: int64(input), OutputTokens: int64(output), Calls: int64(calls), ExpiresAt: parseTime(payload.Data.NextExpiryAt)}, nil
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

func parseTime(value string) *time.Time {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}
