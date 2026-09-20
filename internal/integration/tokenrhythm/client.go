package tokenrhythm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const (
	defaultUsageURL      = "https://tokenrhythm.studio/api/usage-summary"
	defaultUsagePanelURL = "https://tokenrhythm.studio/api/usage/panel?range=today&page=1&pageSize=1"
	usageHistoryPageSize = 100
	maxUsageHistoryPages = 500
)

type Client struct {
	http          *http.Client
	usageURL      string
	usagePanelURL string
}

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

// ModelUsage is an official TokenRhythm usage aggregate for one model. The
// provider calls cache-write tokens "cacheCreationTokens"; SuperMonitor keeps
// the provider-neutral cache-write wording outside the integration boundary.
type ModelUsage struct {
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Calls            int64
}

// UsagePanel contains the current Beijing-calendar-day aggregate returned by
// TokenRhythm's own account console. Values are absolute for the day, not
// deltas, so callers must replace the account/day snapshot idempotently.
type UsagePanel struct {
	Date             string
	Timezone         string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Calls            int64
	Models           []ModelUsage
}

type usageHistoryItem struct {
	ID                  json.RawMessage `json:"id"`
	RequestAt           string          `json:"requestAt"`
	Model               string          `json:"model"`
	ModelID             string          `json:"modelId"`
	InputTokens         any             `json:"inputTokens"`
	OutputTokens        any             `json:"outputTokens"`
	CacheReadTokens     any             `json:"cacheReadTokens"`
	CacheCreationTokens any             `json:"cacheCreationTokens"`
}

type usageHistoryPage struct {
	Items    []usageHistoryItem
	Total    int64
	Page     int64
	PageSize int64
}

func NewClient() *Client {
	return &Client{
		http:          netutil.NewHTTPClient(20 * time.Second),
		usageURL:      defaultUsageURL,
		usagePanelURL: defaultUsagePanelURL,
	}
}

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
	req, err := c.authenticatedRequest(ctx, c.usageURL, credential)
	if err != nil {
		return Usage{}, err
	}
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

// FetchTodayUsage reads the same verified usage panel used by TokenRhythm's
// account console. It intentionally requests only aggregates; call details and
// prompts are neither requested nor stored by SuperMonitor.
func (c *Client) FetchTodayUsage(ctx context.Context, credential Credential) (UsagePanel, error) {
	if strings.TrimSpace(credential.SessionToken) == "" {
		return UsagePanel{}, fmt.Errorf("基元律动 sess_ 会话令牌不能为空")
	}
	req, err := c.authenticatedRequest(ctx, c.usagePanelURL, credential)
	if err != nil {
		return UsagePanel{}, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.http.Do(req)
	if err != nil {
		return UsagePanel{}, fmt.Errorf("连接基元律动用量接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return UsagePanel{}, fmt.Errorf("读取基元律动用量响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return UsagePanel{}, fmt.Errorf("基元律动登录态已失效，请重新复制 sess_ 会话令牌")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UsagePanel{}, fmt.Errorf("基元律动用量接口返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			StartAt  string `json:"startAt"`
			Timezone string `json:"timezone"`
			Summary  struct {
				InputTokens         any `json:"inputTokens"`
				OutputTokens        any `json:"outputTokens"`
				CacheReadTokens     any `json:"cacheReadTokens"`
				CacheCreationTokens any `json:"cacheCreationTokens"`
				Calls               any `json:"calls"`
			} `json:"summary"`
			ByModel []struct {
				Model               string `json:"model"`
				ModelID             string `json:"modelId"`
				InputTokens         any    `json:"inputTokens"`
				OutputTokens        any    `json:"outputTokens"`
				CacheReadTokens     any    `json:"cacheReadTokens"`
				CacheCreationTokens any    `json:"cacheCreationTokens"`
				Calls               any    `json:"calls"`
			} `json:"byModel"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return UsagePanel{}, fmt.Errorf("基元律动用量响应不是有效 JSON: %w", err)
	}
	if code, ok := number(payload.Code); ok && code != 0 {
		return UsagePanel{}, fmt.Errorf("基元律动用量接口返回业务错误 %.0f", code)
	}
	panel := UsagePanel{
		Date:             usageDate(payload.Data.StartAt),
		Timezone:         firstNonEmpty(payload.Data.Timezone, "Asia/Shanghai"),
		InputTokens:      intNumber(payload.Data.Summary.InputTokens),
		OutputTokens:     intNumber(payload.Data.Summary.OutputTokens),
		CacheReadTokens:  intNumber(payload.Data.Summary.CacheReadTokens),
		CacheWriteTokens: intNumber(payload.Data.Summary.CacheCreationTokens),
		Calls:            intNumber(payload.Data.Summary.Calls),
		Models:           make([]ModelUsage, 0, len(payload.Data.ByModel)),
	}
	for _, item := range payload.Data.ByModel {
		model := firstNonEmpty(strings.TrimSpace(item.Model), strings.TrimSpace(item.ModelID))
		if model == "" {
			continue
		}
		panel.Models = append(panel.Models, ModelUsage{
			Model:            model,
			InputTokens:      intNumber(item.InputTokens),
			OutputTokens:     intNumber(item.OutputTokens),
			CacheReadTokens:  intNumber(item.CacheReadTokens),
			CacheWriteTokens: intNumber(item.CacheCreationTokens),
			Calls:            intNumber(item.Calls),
		})
	}
	return panel, nil
}

// FetchUsageHistory reads TokenRhythm's official request list and converts it
// into Beijing-calendar-day aggregates. Only usage counters and the provider's
// real model identifiers are retained; prompt and preview fields are ignored.
// An empty usageRange defaults to the console's 30-day view.
func (c *Client) FetchUsageHistory(ctx context.Context, credential Credential, usageRange string) ([]UsagePanel, error) {
	if strings.TrimSpace(credential.SessionToken) == "" {
		return nil, fmt.Errorf("基元律动 sess_ 会话令牌不能为空")
	}
	usageRange = strings.TrimSpace(usageRange)
	if usageRange == "" {
		usageRange = "30d"
	}

	type dailyAggregate struct {
		panel  UsagePanel
		models map[string]*ModelUsage
	}
	days := make(map[string]*dailyAggregate)
	seenIDs := make(map[string]struct{})
	var fetched int64
	for pageNumber := 1; pageNumber <= maxUsageHistoryPages; pageNumber++ {
		page, err := c.fetchUsageHistoryPage(ctx, credential, usageRange, pageNumber)
		if err != nil {
			return nil, err
		}
		fetched += int64(len(page.Items))
		for _, item := range page.Items {
			id := usageHistoryItemID(item.ID)
			if id != "" {
				if _, exists := seenIDs[id]; exists {
					continue
				}
				seenIDs[id] = struct{}{}
			}
			date, err := beijingUsageDate(item.RequestAt)
			if err != nil {
				return nil, fmt.Errorf("基元律动用量明细 requestAt 无效: %w", err)
			}
			aggregate := days[date]
			if aggregate == nil {
				aggregate = &dailyAggregate{
					panel:  UsagePanel{Date: date, Timezone: "Asia/Shanghai"},
					models: make(map[string]*ModelUsage),
				}
				days[date] = aggregate
			}
			input := intNumber(item.InputTokens)
			output := intNumber(item.OutputTokens)
			cacheRead := intNumber(item.CacheReadTokens)
			cacheWrite := intNumber(item.CacheCreationTokens)
			aggregate.panel.InputTokens += input
			aggregate.panel.OutputTokens += output
			aggregate.panel.CacheReadTokens += cacheRead
			aggregate.panel.CacheWriteTokens += cacheWrite
			aggregate.panel.Calls++

			model := firstNonEmpty(item.Model, item.ModelID)
			if model == "" {
				continue
			}
			modelAggregate := aggregate.models[model]
			if modelAggregate == nil {
				modelAggregate = &ModelUsage{Model: model}
				aggregate.models[model] = modelAggregate
			}
			modelAggregate.InputTokens += input
			modelAggregate.OutputTokens += output
			modelAggregate.CacheReadTokens += cacheRead
			modelAggregate.CacheWriteTokens += cacheWrite
			modelAggregate.Calls++
		}

		if len(page.Items) == 0 || (page.Total > 0 && fetched >= page.Total) || (page.Total <= 0 && len(page.Items) < usageHistoryPageSize) {
			break
		}
		if pageNumber == maxUsageHistoryPages {
			return nil, fmt.Errorf("基元律动用量明细超过分页安全上限 %d 页", maxUsageHistoryPages)
		}
	}

	dates := make([]string, 0, len(days))
	for date := range days {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	result := make([]UsagePanel, 0, len(dates))
	for _, date := range dates {
		aggregate := days[date]
		modelNames := make([]string, 0, len(aggregate.models))
		for model := range aggregate.models {
			modelNames = append(modelNames, model)
		}
		sort.Strings(modelNames)
		aggregate.panel.Models = make([]ModelUsage, 0, len(modelNames))
		for _, model := range modelNames {
			aggregate.panel.Models = append(aggregate.panel.Models, *aggregate.models[model])
		}
		result = append(result, aggregate.panel)
	}
	return result, nil
}

func (c *Client) fetchUsageHistoryPage(ctx context.Context, credential Credential, usageRange string, page int) (usageHistoryPage, error) {
	endpoint, err := usageHistoryEndpoint(c.usagePanelURL, usageRange, page)
	if err != nil {
		return usageHistoryPage{}, err
	}
	req, err := c.authenticatedRequest(ctx, endpoint, credential)
	if err != nil {
		return usageHistoryPage{}, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.http.Do(req)
	if err != nil {
		return usageHistoryPage{}, fmt.Errorf("连接基元律动用量接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return usageHistoryPage{}, fmt.Errorf("读取基元律动用量响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return usageHistoryPage{}, fmt.Errorf("基元律动登录态已失效，请重新复制 sess_ 会话令牌")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return usageHistoryPage{}, fmt.Errorf("基元律动用量接口返回 HTTP %d", resp.StatusCode)
	}
	parsed, err := decodeUsageHistoryPage(body, 0)
	if err != nil {
		return usageHistoryPage{}, fmt.Errorf("基元律动用量响应解析失败: %w", err)
	}
	return parsed, nil
}

func usageHistoryEndpoint(endpoint, usageRange string, page int) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("基元律动用量接口地址无效: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("基元律动用量接口地址无效")
	}
	query := parsed.Query()
	query.Set("range", usageRange)
	query.Set("page", strconv.Itoa(page))
	query.Set("pageSize", strconv.Itoa(usageHistoryPageSize))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func decodeUsageHistoryPage(body []byte, depth int) (usageHistoryPage, error) {
	if depth > 4 {
		return usageHistoryPage{}, fmt.Errorf("data 包装层级过深")
	}
	var payload struct {
		Code     any             `json:"code"`
		Message  string          `json:"message"`
		Data     json.RawMessage `json:"data"`
		Items    json.RawMessage `json:"items"`
		Total    any             `json:"total"`
		Page     any             `json:"page"`
		PageSize any             `json:"pageSize"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return usageHistoryPage{}, fmt.Errorf("不是有效 JSON: %w", err)
	}
	if code, ok := number(payload.Code); ok && code != 0 {
		if message := strings.TrimSpace(payload.Message); message != "" {
			return usageHistoryPage{}, fmt.Errorf("业务错误 %.0f: %s", code, message)
		}
		return usageHistoryPage{}, fmt.Errorf("业务错误 %.0f", code)
	}
	if len(payload.Items) > 0 && string(payload.Items) != "null" {
		var items []usageHistoryItem
		itemsDecoder := json.NewDecoder(strings.NewReader(string(payload.Items)))
		itemsDecoder.UseNumber()
		if err := itemsDecoder.Decode(&items); err != nil {
			return usageHistoryPage{}, fmt.Errorf("items 不是有效数组: %w", err)
		}
		return usageHistoryPage{
			Items:    items,
			Total:    intNumber(payload.Total),
			Page:     intNumber(payload.Page),
			PageSize: intNumber(payload.PageSize),
		}, nil
	}
	if len(payload.Data) > 0 && string(payload.Data) != "null" {
		return decodeUsageHistoryPage(payload.Data, depth+1)
	}
	return usageHistoryPage{}, fmt.Errorf("缺少 data.items")
}

func usageHistoryItemID(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return text
}

func beijingUsageDate(value string) (string, error) {
	text := strings.TrimSpace(value)
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, parseErr := time.Parse(layout, text); parseErr == nil {
			return parsed.In(location).Format("2006-01-02"), nil
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if parsed, parseErr := time.ParseInLocation(layout, text, location); parseErr == nil {
			return parsed.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("%q", text)
}

func (c *Client) authenticatedRequest(ctx context.Context, endpoint string, credential Credential) (*http.Request, error) {
	token := strings.TrimSpace(credential.SessionToken)
	if token == "" {
		return nil, fmt.Errorf("基元律动 sess_ 会话令牌不能为空")
	}
	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("基元律动接口地址不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cookie", "tr_session="+token+"; tr_ref_device="+strings.TrimSpace(credential.RefDevice))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 SuperMonitor/0.8.1")
	return req, nil
}

func intNumber(value any) int64 {
	parsed, _ := number(value)
	if parsed <= 0 {
		return 0
	}
	return int64(parsed)
}

func usageDate(value string) string {
	text := strings.TrimSpace(value)
	if len(text) >= len("2006-01-02") {
		if _, err := time.Parse("2006-01-02", text[:len("2006-01-02")]); err == nil {
			return text[:len("2006-01-02")]
		}
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	return time.Now().In(location).Format("2006-01-02")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
