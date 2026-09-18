package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Variant string

const (
	VariantCN     Variant = "cn"
	VariantGlobal Variant = "global"
)

type Client struct{ http *http.Client }
type Challenge struct {
	Variant          Variant
	State, VerifyURL string
	ExpiresAt        time.Time
}
type Credential struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	UID          string    `json:"uid,omitempty"`
	Nickname     string    `json:"nickname,omitempty"`
	Email        string    `json:"email,omitempty"`
	Domain       string    `json:"domain,omitempty"`
	Variant      Variant   `json:"variant"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}
type Credits struct {
	Total, Remaining float64
	ExpiresAt        *time.Time
}

func NewClient() *Client { return &Client{http: &http.Client{Timeout: 20 * time.Second}} }
func ProviderVariant(providerID string) (Variant, error) {
	if providerID == "workbuddy-cn" {
		return VariantCN, nil
	}
	if providerID == "workbuddy-global" {
		return VariantGlobal, nil
	}
	return "", fmt.Errorf("不支持的 WorkBuddy 平台: %s", providerID)
}

func ParseCredential(raw []byte, variant Variant) (Credential, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return Credential{}, fmt.Errorf("认证文件为空或超过 1 MB")
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Credential{}, fmt.Errorf("认证文件不是有效 JSON: %w", err)
	}
	auth := childMap(root, "auth")
	account := childMap(root, "account")
	credential := Credential{
		AccessToken:  firstString(auth, root, "accessToken", "access_token", "token"),
		RefreshToken: firstString(auth, root, "refreshToken", "refresh_token"),
		UID:          firstString(account, root, "uid", "id"), Nickname: firstString(account, root, "nickname", "name"),
		Email: firstString(account, root, "email"), Domain: firstString(auth, root, "domain"), Variant: variant,
	}
	if credential.AccessToken == "" {
		return Credential{}, fmt.Errorf("认证文件缺少 accessToken")
	}
	if value := firstValue(auth, root, "expiresAt", "expires_at"); value != nil {
		credential.ExpiresAt, _ = parseTime(value)
	}
	if credential.Domain == "" {
		credential.Domain = defaultDomain(variant)
	}
	if err := validateDomain(variant, credential.Domain); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (c *Client) StartOAuth(ctx context.Context, variant Variant) (Challenge, error) {
	url := fmt.Sprintf("%s/v2/plugin/auth/state?platform=%s", endpoint(variant), platform(variant))
	value, status, err := c.requestJSON(ctx, http.MethodPost, url, map[string]any{}, nil)
	if err != nil {
		return Challenge{}, err
	}
	if status < 200 || status >= 300 {
		return Challenge{}, fmt.Errorf("WorkBuddy 登录接口返回 HTTP %d", status)
	}
	data := childMap(value, "data")
	state := stringValue(data["state"])
	if state == "" {
		return Challenge{}, fmt.Errorf("WorkBuddy 登录响应缺少 state")
	}
	verifyURL := firstNonEmpty(stringValue(data["authUrl"]), stringValue(data["auth_url"]), stringValue(data["url"]))
	if verifyURL == "" {
		verifyURL = endpoint(variant) + "/login?state=" + state
	}
	return Challenge{Variant: variant, State: state, VerifyURL: verifyURL, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
}

func (c *Client) PollOAuth(ctx context.Context, challenge Challenge) (Credential, bool, error) {
	url := fmt.Sprintf("%s/v2/plugin/auth/token?state=%s", endpoint(challenge.Variant), challenge.State)
	value, _, err := c.requestJSON(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return Credential{}, false, err
	}
	code := intValue(value["code"])
	data := childMap(value, "data")
	access := firstString(data, nil, "accessToken", "access_token")
	if (code != 0 && code != 200) || access == "" {
		return Credential{}, true, nil
	}
	credential := Credential{AccessToken: access, RefreshToken: firstString(data, nil, "refreshToken", "refresh_token"), Domain: firstString(data, nil, "domain"), Variant: challenge.Variant}
	if credential.Domain == "" {
		credential.Domain = defaultDomain(challenge.Variant)
	}
	if err := validateDomain(challenge.Variant, credential.Domain); err != nil {
		return Credential{}, false, err
	}
	if raw := firstValue(data, nil, "expiresAt", "expires_at"); raw != nil {
		credential.ExpiresAt, _ = parseTime(raw)
	} else if seconds := int64Value(data["expiresIn"]); seconds > 0 {
		credential.ExpiresAt = time.Now().UTC().Add(time.Duration(seconds) * time.Second)
	}
	headers := authHeaders(credential, endpoint(challenge.Variant))
	profileURL := fmt.Sprintf("%s/v2/plugin/login/account?state=%s", endpoint(challenge.Variant), challenge.State)
	profile, _, profileErr := c.requestJSON(ctx, http.MethodGet, profileURL, nil, headers)
	if profileErr == nil {
		info := childMap(profile, "data")
		credential.UID = firstString(info, nil, "uid", "id")
		credential.Nickname = firstString(info, nil, "nickname", "name")
		credential.Email = firstString(info, nil, "email")
		if domain := firstString(info, nil, "domain"); domain != "" {
			if err := validateDomain(challenge.Variant, domain); err != nil {
				return Credential{}, false, err
			}
			credential.Domain = domain
		}
	}
	return credential, false, nil
}

func (c *Client) Refresh(ctx context.Context, credential Credential) (Credential, error) {
	if credential.RefreshToken == "" {
		return credential, fmt.Errorf("WorkBuddy 授权缺少 refreshToken，请重新登录")
	}
	headers := authHeaders(credential, endpoint(credential.Variant))
	headers["X-Refresh-Token"] = credential.RefreshToken
	headers["X-Auth-Refresh-Source"] = "plugin"
	value, status, err := c.requestJSON(ctx, http.MethodPost, endpoint(credential.Variant)+"/v2/plugin/auth/token/refresh", map[string]any{}, headers)
	if err != nil {
		return credential, err
	}
	if status < 200 || status >= 300 {
		return credential, fmt.Errorf("WorkBuddy 刷新接口返回 HTTP %d", status)
	}
	if code := intValue(value["code"]); code != 0 && code != 200 {
		return credential, fmt.Errorf("WorkBuddy 授权刷新失败 code=%d", code)
	}
	data := childMap(value, "data")
	access := firstString(data, nil, "accessToken", "access_token")
	if access == "" {
		return credential, fmt.Errorf("WorkBuddy 刷新响应缺少 accessToken")
	}
	credential.AccessToken = access
	if refresh := firstString(data, nil, "refreshToken", "refresh_token"); refresh != "" {
		credential.RefreshToken = refresh
	}
	if seconds := int64Value(data["expiresIn"]); seconds > 0 {
		credential.ExpiresAt = time.Now().UTC().Add(time.Duration(seconds) * time.Second)
	}
	return credential, nil
}

func (c *Client) FetchCredits(ctx context.Context, credential *Credential) (Credits, bool, error) {
	return c.fetchCredits(ctx, credential, true)
}

func (c *Client) fetchCredits(ctx context.Context, credential *Credential, allowRefresh bool) (Credits, bool, error) {
	if !credential.ExpiresAt.IsZero() && time.Until(credential.ExpiresAt) < 30*time.Minute && credential.RefreshToken != "" {
		refreshed, err := c.Refresh(ctx, *credential)
		if err == nil {
			*credential = refreshed
		}
	}
	body := map[string]any{"PageNumber": 1, "PageSize": 100, "ProductCode": "p_tcaca", "Status": []int{0, 3}, "PackageEndTimeRangeBegin": time.Now().Format("2006-01-02 15:04:05"), "PackageEndTimeRangeEnd": time.Now().AddDate(101, 0, 0).Format("2006-01-02 15:04:05")}
	paths := []string{"/v2/billing/meter/get-user-resource"}
	if credential.Variant == VariantGlobal {
		paths = []string{"/billing/meter/get-user-resource", "/v2/billing/meter/get-user-resource"}
	}
	var lastStatus int
	for _, path := range paths {
		origin := apiBase(*credential)
		headers := authHeaders(*credential, origin)
		headers["X-Client-Platform"] = "web"
		headers["Origin"] = origin
		headers["Referer"] = origin + "/profile/plans-usage"
		value, status, err := c.requestJSON(ctx, http.MethodPost, origin+path, body, headers)
		lastStatus = status
		if err != nil {
			return Credits{}, false, err
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			if credential.RefreshToken == "" || !allowRefresh {
				return Credits{}, false, fmt.Errorf("WorkBuddy 登录已失效，请重新登录")
			}
			refreshed, refreshErr := c.Refresh(ctx, *credential)
			if refreshErr != nil {
				return Credits{}, false, refreshErr
			}
			*credential = refreshed
			return c.fetchCredits(ctx, credential, false)
		}
		if status == http.StatusNotFound {
			continue
		}
		if status < 200 || status >= 300 {
			return Credits{}, false, fmt.Errorf("WorkBuddy 积分接口返回 HTTP %d", status)
		}
		if code := intValue(value["code"]); code != 0 && code != 200 {
			return Credits{}, false, fmt.Errorf("WorkBuddy 积分接口返回业务错误 code=%d", code)
		}
		resources := resourceAccounts(value)
		if resources == nil {
			return Credits{}, false, fmt.Errorf("WorkBuddy 积分响应缺少 Accounts")
		}
		result := summarize(resources)
		return result, false, nil
	}
	return Credits{}, false, fmt.Errorf("WorkBuddy 积分接口不可用（HTTP %d）", lastStatus)
}

func (c *Client) requestJSON(ctx context.Context, method, url string, body any, headers map[string]string) (map[string]any, int, error) {
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		encoded, _ := json.Marshal(body)
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("连接 WorkBuddy 官方接口失败: %w", err)
	}
	defer resp.Body.Close()
	var value map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("WorkBuddy 响应不是有效 JSON: %w", err)
	}
	return value, resp.StatusCode, nil
}

func resourceAccounts(root map[string]any) []map[string]any {
	paths := [][]string{{"data", "Accounts"}, {"data", "data", "Accounts"}, {"data", "Response", "Data", "Accounts"}, {"data", "data", "Response", "Data", "Accounts"}}
	for _, path := range paths {
		var current any = root
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[key]
		}
		if array, ok := current.([]any); ok {
			result := make([]map[string]any, 0, len(array))
			for _, item := range array {
				if object, ok := item.(map[string]any); ok {
					result = append(result, object)
				}
			}
			return result
		}
	}
	return nil
}
func summarize(resources []map[string]any) Credits {
	result := Credits{}
	for _, resource := range resources {
		total := firstNumber(resource, "CycleCapacitySizePrecise", "CycleCapacitySize", "CycleTotalCapacity", "CapacitySizePrecise", "CapacitySize", "SlicePeriodCapacitySizePrecise", "SlicePeriodCapacitySize")
		remaining := firstNumber(resource, "CycleCapacityRemainPrecise", "CycleCapacityRemain", "CycleRemainCapacity", "CapacityRemainPrecise", "CapacityRemain", "SlicePeriodCapacityRemainPrecise", "SlicePeriodCapacityRemain")
		result.Total += total
		result.Remaining += remaining
		for _, key := range []string{"DeductionEndTime", "deductionEndTime", "CycleEndTime", "cycleEndTime", "ExpiredTime", "expiredTime"} {
			if resource[key] == nil {
				continue
			}
			if value, ok := parseTime(resource[key]); ok && (result.ExpiresAt == nil || value.Before(*result.ExpiresAt)) {
				copy := value
				result.ExpiresAt = &copy
			}
		}
	}
	return result
}
func firstNumber(object map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := number(object[key]); ok {
			return value
		}
	}
	return 0
}
func authHeaders(credential Credential, _ string) map[string]string {
	headers := map[string]string{"Authorization": "Bearer " + credential.AccessToken}
	if credential.UID != "" {
		headers["X-User-Id"] = credential.UID
	}
	if credential.Domain != "" {
		headers["X-Domain"] = credential.Domain
	}
	return headers
}
func endpoint(variant Variant) string {
	if variant == VariantGlobal {
		return "https://www.workbuddy.ai"
	}
	return "https://www.codebuddy.cn"
}
func platform(variant Variant) string {
	if variant == VariantGlobal {
		return "workbuddy-ai"
	}
	return "workbuddy"
}
func defaultDomain(variant Variant) string {
	if variant == VariantGlobal {
		return "www.workbuddy.ai"
	}
	return "www.codebuddy.cn"
}
func apiBase(credential Credential) string {
	if credential.Variant == VariantGlobal {
		return endpoint(VariantGlobal)
	}
	domain := strings.ToLower(strings.TrimSpace(credential.Domain))
	if domain == "workbuddy.cn" || domain == "www.workbuddy.cn" {
		return "https://www.workbuddy.cn"
	}
	return endpoint(VariantCN)
}
func validateDomain(variant Variant, domain string) error {
	value := strings.ToLower(strings.TrimSpace(domain))
	if variant == VariantGlobal && !strings.HasSuffix(value, ".workbuddy.ai") {
		return fmt.Errorf("登录响应域名与 WorkBuddy 国际版不符")
	}
	if variant == VariantCN && strings.HasSuffix(value, ".workbuddy.ai") {
		return fmt.Errorf("登录响应域名与 WorkBuddy 国内版不符")
	}
	return nil
}
func childMap(root map[string]any, key string) map[string]any {
	if value, ok := root[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}
func firstString(primary, fallback map[string]any, keys ...string) string {
	for _, object := range []map[string]any{primary, fallback} {
		for _, key := range keys {
			if value := stringValue(object[key]); value != "" {
				return value
			}
		}
	}
	return ""
}
func firstValue(primary, fallback map[string]any, keys ...string) any {
	for _, object := range []map[string]any{primary, fallback} {
		for _, key := range keys {
			if object != nil && object[key] != nil {
				return object[key]
			}
		}
	}
	return nil
}
func stringValue(value any) string { text, _ := value.(string); return strings.TrimSpace(text) }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func intValue(value any) int     { number, _ := number(value); return int(number) }
func int64Value(value any) int64 { number, _ := number(value); return int64(number) }
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
func parseTime(value any) (time.Time, bool) {
	if numeric, ok := number(value); ok {
		seconds := int64(numeric)
		if seconds > 10_000_000_000 {
			seconds /= 1000
		}
		return time.Unix(seconds, 0).UTC(), true
	}
	if text, ok := value.(string); ok {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
			if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
				return parsed.UTC(), true
			}
		}
	}
	return time.Time{}, false
}
