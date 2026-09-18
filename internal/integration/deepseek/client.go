package deepseek

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

const balanceURL = "https://api.deepseek.com/user/balance"

type Client struct{ http *http.Client }
type Credential struct {
	APIKey string `json:"apiKey"`
}
type Balance struct {
	Currency                 string
	Total, Granted, ToppedUp float64
	Available                bool
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func (c *Client) FetchBalance(ctx context.Context, credential Credential) (Balance, error) {
	if strings.TrimSpace(credential.APIKey) == "" {
		return Balance{}, fmt.Errorf("DeepSeek API Key 不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, balanceURL, nil)
	if err != nil {
		return Balance{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential.APIKey))
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Balance{}, fmt.Errorf("连接 DeepSeek 余额接口失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Balance{}, fmt.Errorf("DeepSeek 余额接口返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Available    bool `json:"is_available"`
		BalanceInfos []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
			Granted  string `json:"granted_balance"`
			ToppedUp string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Balance{}, fmt.Errorf("解析 DeepSeek 余额失败: %w", err)
	}
	if len(payload.BalanceInfos) == 0 {
		return Balance{}, fmt.Errorf("DeepSeek 余额响应没有 balance_infos")
	}
	item := payload.BalanceInfos[0]
	parse := func(value string) float64 { number, _ := strconv.ParseFloat(value, 64); return number }
	return Balance{Currency: item.Currency, Total: parse(item.Total), Granted: parse(item.Granted), ToppedUp: parse(item.ToppedUp), Available: payload.Available}, nil
}
