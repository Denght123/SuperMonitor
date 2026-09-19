package workbuddy

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
	Resources        []CreditResource
}
type CreditResource struct {
	Code, Name             string
	Total, Remaining, Used float64
	ExpiresAt              *time.Time
}
type CheckinStatus struct {
	TodayCheckedIn bool
}

const (
	resourceSummaryPath = "/billing/meter/get-user-resource-summary"
	resourcePaidPath    = "/billing/meter/get-user-resource-paid-packages"
	resourceFreePath    = "/billing/meter/get-user-resource-free-packages"
)

var paidPackageCodes = []string{
	"TCACA_code_002_AkiJS3ZHF5", "TCACA_code_023_4xbGhMrE6q", "TCACA_code_026_BaESVICNoi", "TCACA_code_027_0FCGVA6vSa",
	"TCACA_code_009_0XmEQc2xOf", "TCACA_code_038_OhvqZtiPKr", "TCACA_code_003_FAnt7lcmRT", "TCACA_code_036_lupO5WgNdG",
}

var freePackageCodes = []string{
	"TCACA_code_008_cfWoLwvjU4", "TCACA_code_007_nzdH5h4Nl0", "TCACA_code_028_NtpWi0jzXs", "TCACA_code_029_6wCGEWquYy",
	"TCACA_code_030_BjSt89qTvr", "TCACA_code_001_PqouKr6QWV", "TCACA_code_006_DbXS0lrypC", "TCACA_code_035_ArVxJcGDsm",
	"TCACA_code_037_WxOD3MpI2o", "TCACA_code_039_KRcQj7wUat", "TCACA_code_040_mi9rCYg46x",
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }
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
	refreshed := false
	if !credential.ExpiresAt.IsZero() && time.Until(credential.ExpiresAt) < 30*time.Minute && credential.RefreshToken != "" {
		if next, err := c.Refresh(ctx, *credential); err == nil {
			*credential = next
			refreshed = true
		}
	}
	credits, unauthorized, err := c.fetchCreditsOnce(ctx, *credential)
	if err == nil {
		return credits, refreshed, nil
	}
	if !unauthorized || refreshed || credential.RefreshToken == "" {
		return Credits{}, refreshed, err
	}
	next, refreshErr := c.Refresh(ctx, *credential)
	if refreshErr != nil {
		return Credits{}, refreshed, refreshErr
	}
	*credential = next
	credits, _, err = c.fetchCreditsOnce(ctx, *credential)
	return credits, true, err
}

func (c *Client) FetchCheckinStatus(ctx context.Context, credential *Credential) (CheckinStatus, bool, error) {
	if credential.Variant != VariantCN {
		return CheckinStatus{}, false, fmt.Errorf("该 WorkBuddy 档位没有签到活动")
	}
	status, unauthorized, err := c.fetchCheckinStatusOnce(ctx, *credential)
	if err == nil {
		return status, false, nil
	}
	if !unauthorized || credential.RefreshToken == "" {
		return CheckinStatus{}, false, err
	}
	next, refreshErr := c.Refresh(ctx, *credential)
	if refreshErr != nil {
		return CheckinStatus{}, false, refreshErr
	}
	*credential = next
	status, _, err = c.fetchCheckinStatusOnce(ctx, *credential)
	return status, true, err
}

func (c *Client) fetchCheckinStatusOnce(ctx context.Context, credential Credential) (CheckinStatus, bool, error) {
	origin := apiBase(credential)
	var lastMessage string
	for _, path := range []string{"/v2/billing/meter/checkin-activity-status", "/v2/billing/meter/checkin-status"} {
		value, status, err := c.requestJSON(ctx, http.MethodPost, origin+path, map[string]any{}, resourceHeaders(credential, origin))
		if err != nil {
			return CheckinStatus{}, false, err
		}
		if isUnauthorized(status, value) {
			return CheckinStatus{}, true, fmt.Errorf("WorkBuddy 登录已失效，请重新登录")
		}
		if status >= 200 && status < 300 && isSuccessResponse(value) {
			data := childMap(value, "data")
			checked, _ := firstBool(data, "today_checked_in", "todayCheckedIn")
			return CheckinStatus{TodayCheckedIn: checked}, false, nil
		}
		lastMessage = responseMessage(value)
	}
	return CheckinStatus{}, false, fmt.Errorf("WorkBuddy 当前没有可用的签到活动: %s", lastMessage)
}

func (c *Client) Checkin(ctx context.Context, credential *Credential) (bool, bool, error) {
	status, refreshed, err := c.FetchCheckinStatus(ctx, credential)
	if err != nil {
		return false, refreshed, err
	}
	if status.TodayCheckedIn {
		return true, refreshed, nil
	}
	already, unauthorized, err := c.checkinOnce(ctx, *credential)
	if err == nil {
		return already, refreshed, nil
	}
	if !unauthorized || refreshed || credential.RefreshToken == "" {
		return false, refreshed, err
	}
	next, refreshErr := c.Refresh(ctx, *credential)
	if refreshErr != nil {
		return false, refreshed, refreshErr
	}
	*credential = next
	already, _, err = c.checkinOnce(ctx, *credential)
	return already, true, err
}

func (c *Client) checkinOnce(ctx context.Context, credential Credential) (bool, bool, error) {
	origin := apiBase(credential)
	value, status, err := c.requestJSON(ctx, http.MethodPost, origin+"/v2/billing/meter/daily-checkin", map[string]any{}, resourceHeaders(credential, origin))
	if err != nil {
		return false, false, err
	}
	if isUnauthorized(status, value) {
		return false, true, fmt.Errorf("WorkBuddy 登录已失效，请重新登录")
	}
	if status >= 200 && status < 300 && isSuccessResponse(value) {
		return false, false, nil
	}
	message := responseMessage(value)
	if strings.Contains(message, "已签到") || strings.Contains(strings.ToLower(message), "repeat") {
		return true, false, nil
	}
	return false, false, fmt.Errorf("WorkBuddy 签到失败: %s", message)
}

func (c *Client) fetchCreditsOnce(ctx context.Context, credential Credential) (Credits, bool, error) {
	if credential.Variant == VariantCN {
		credits, recognized, unauthorized, err := c.fetchDomesticCredits(ctx, credential)
		if err != nil || unauthorized || recognized {
			return credits, unauthorized, err
		}
	}
	return c.fetchLegacyCredits(ctx, credential)
}

func (c *Client) fetchDomesticCredits(ctx context.Context, credential Credential) (Credits, bool, bool, error) {
	now := time.Now()
	requests := []struct {
		path  string
		body  map[string]any
		field string
	}{
		{resourceSummaryPath, map[string]any{}, "Packages"},
		{resourcePaidPath, map[string]any{"PageNumber": 1, "PageSize": 200, "Status": []int{0, 3}, "PackageCodes": paidPackageCodes, "NeedRenewInfo": true}, "Accounts"},
		{resourceFreePath, map[string]any{"PageNumber": 1, "PageSize": 200, "Status": []int{0, 3}, "SlicePeriodStartTime": now.Format("2006-01-02") + " 00:00:00", "SlicePeriodEndTime": now.Format("2006-01-02") + " 23:59:59", "PackageCodes": freePackageCodes}, "Accounts"},
	}
	var summary, details []CreditResource
	recognized := false
	for _, request := range requests {
		value, status, err := c.requestResource(ctx, credential, []string{request.path}, request.body)
		if err != nil {
			return Credits{}, false, false, err
		}
		if isUnauthorized(status, value) {
			return Credits{}, false, true, fmt.Errorf("WorkBuddy 登录已失效，请重新登录")
		}
		if status < 200 || status >= 300 || !isSuccessResponse(value) {
			continue
		}
		items, present := resourceList(value, request.field)
		if !present {
			continue
		}
		recognized = true
		normalized := normalizeResources(items, time.Now().UTC())
		if request.field == "Packages" {
			summary = normalized
		} else {
			details = append(details, normalized...)
		}
	}
	if !recognized {
		return Credits{}, false, false, nil
	}
	return summarizeResources(dedupeResources(mergeResources(summary, details))), true, false, nil
}

func (c *Client) fetchLegacyCredits(ctx context.Context, credential Credential) (Credits, bool, error) {
	body := map[string]any{"PageNumber": 1, "PageSize": 100, "ProductCode": "p_tcaca", "Status": []int{0, 3}, "PackageEndTimeRangeBegin": time.Now().Format("2006-01-02 15:04:05"), "PackageEndTimeRangeEnd": time.Now().AddDate(101, 0, 0).Format("2006-01-02 15:04:05")}
	paths := []string{"/v2/billing/meter/get-user-resource"}
	if credential.Variant == VariantGlobal {
		paths = []string{"/billing/meter/get-user-resource", "/v2/billing/meter/get-user-resource"}
	}
	value, status, err := c.requestResource(ctx, credential, paths, body)
	if err != nil {
		return Credits{}, false, err
	}
	if isUnauthorized(status, value) {
		return Credits{}, true, fmt.Errorf("WorkBuddy 登录已失效，请重新登录")
	}
	if status < 200 || status >= 300 {
		return Credits{}, false, fmt.Errorf("WorkBuddy 积分接口返回 HTTP %d: %s", status, responseMessage(value))
	}
	if !isSuccessResponse(value) {
		return Credits{}, false, fmt.Errorf("WorkBuddy 积分接口返回业务错误 code=%d: %s", responseCode(value), responseMessage(value))
	}
	items, present := resourceList(value, "Accounts")
	if !present {
		return Credits{}, false, fmt.Errorf("WorkBuddy 积分响应缺少 Accounts")
	}
	return summarizeResources(dedupeResources(normalizeResources(items, time.Now().UTC()))), false, nil
}

func (c *Client) requestResource(ctx context.Context, credential Credential, paths []string, body map[string]any) (map[string]any, int, error) {
	origin := apiBase(credential)
	var last map[string]any
	var lastStatus int
	for index, path := range paths {
		value, status, err := c.requestJSON(ctx, http.MethodPost, origin+path, body, resourceHeaders(credential, origin))
		if err != nil {
			return nil, status, err
		}
		last, lastStatus = value, status
		if index+1 == len(paths) || !isRouteMissing(status, value) {
			return value, status, nil
		}
	}
	return last, lastStatus, nil
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取 WorkBuddy 响应失败: %w", err)
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		value = map[string]any{"message": strings.TrimSpace(string(raw))}
	}
	return value, resp.StatusCode, nil
}

func resourceList(root map[string]any, field string) ([]map[string]any, bool) {
	lower := strings.ToLower(field)
	paths := [][]string{{"data", field}, {"data", "data", field}, {"data", "Response", "Data", field}, {"data", "data", "Response", "Data", field}, {"data", lower}, {"data", "data", lower}}
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
			return result, true
		}
	}
	return nil, false
}

func normalizeResources(items []map[string]any, now time.Time) []CreditResource {
	result := make([]CreditResource, 0, len(items))
	for _, item := range items {
		result = append(result, normalizeResource(item, now))
	}
	return result
}

func normalizeResource(resource map[string]any, now time.Time) CreditResource {
	slice := map[string]any{}
	for _, key := range []string{"SlicePeriodUsageDetails", "slicePeriodUsageDetails"} {
		if array, ok := resource[key].([]any); ok && len(array) > 0 {
			slice, _ = array[0].(map[string]any)
		}
	}
	totalKeys := []string{"CycleCapacitySizePrecise", "CycleCapacitySize", "CycleTotalCapacity", "CapacitySizePrecise", "CapacitySize", "SlicePeriodCapacitySizePrecise", "SlicePeriodCapacitySize"}
	remainingKeys := []string{"CycleCapacityRemainPrecise", "CycleCapacityRemain", "CycleRemainCapacity", "CapacityRemainPrecise", "CapacityRemain", "SlicePeriodCapacityRemainPrecise", "SlicePeriodCapacityRemain"}
	usedKeys := []string{"CycleCapacityUsedPrecise", "CycleCapacityUsed", "CycleUsedCapacity", "CapacityUsedPrecise", "CapacityUsed", "SlicePeriodCapacityUsedPrecise", "SlicePeriodCapacityUsed"}
	total, hasTotal := firstNumberOK(resource, totalKeys...)
	if !hasTotal {
		total, hasTotal = firstNumberOK(slice, totalKeys...)
	}
	remaining, hasRemaining := firstNumberOK(resource, remainingKeys...)
	if !hasRemaining {
		remaining, hasRemaining = firstNumberOK(slice, remainingKeys...)
	}
	used, hasUsed := firstNumberOK(resource, usedKeys...)
	if !hasUsed {
		used, hasUsed = firstNumberOK(slice, usedKeys...)
	}
	if !hasTotal {
		switch {
		case hasRemaining && hasUsed:
			total = remaining + used
		case hasRemaining:
			total = remaining
		case hasUsed:
			total = used
		}
	}
	if !hasRemaining {
		remaining = maxFloat(total-used, 0)
	}
	if !hasUsed {
		used = maxFloat(total-remaining, 0)
	}
	return CreditResource{Code: firstString(resource, nil, "PackageCode", "packageCode"), Name: firstString(resource, nil, "PackageName", "packageName"), Total: maxFloat(total, 0), Remaining: maxFloat(remaining, 0), Used: maxFloat(used, 0), ExpiresAt: resolveExpiry(resource, now)}
}

func resolveExpiry(resource map[string]any, now time.Time) *time.Time {
	var deduction, cycle *time.Time
	for _, key := range []string{"DeductionEndTime", "deductionEndTime", "ExpiredTime", "expiredTime"} {
		if value, ok := parseTime(resource[key]); ok {
			copy := value
			deduction = &copy
			break
		}
	}
	for _, key := range []string{"CycleEndTime", "cycleEndTime"} {
		if value, ok := parseTime(resource[key]); ok {
			copy := value
			cycle = &copy
			break
		}
	}
	result := deduction
	if result == nil || (cycle != nil && result.Sub(*cycle) > 365*24*time.Hour) {
		result = cycle
	}
	if result != nil && result.Sub(now) > 730*24*time.Hour {
		return nil
	}
	return result
}

func mergeResources(summary, details []CreditResource) []CreditResource {
	codes := map[string]bool{}
	for _, item := range details {
		if item.Code != "" {
			codes[item.Code] = true
		}
	}
	result := append([]CreditResource{}, details...)
	for _, item := range summary {
		if item.Code == "" || !codes[item.Code] {
			result = append(result, item)
		}
	}
	return result
}

func dedupeResources(resources []CreditResource) []CreditResource {
	seen := make(map[string]bool, len(resources))
	result := make([]CreditResource, 0, len(resources))
	for _, resource := range resources {
		expires := ""
		if resource.ExpiresAt != nil {
			expires = resource.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		key := fmt.Sprintf("%s\x00%s\x00%.9f\x00%.9f\x00%.9f\x00%s", resource.Code, resource.Name, resource.Total, resource.Remaining, resource.Used, expires)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, resource)
	}
	return result
}

func summarizeResources(resources []CreditResource) Credits {
	result := Credits{Resources: resources}
	for _, resource := range resources {
		result.Total += resource.Total
		result.Remaining += resource.Remaining
		if resource.Remaining > 0 && resource.ExpiresAt != nil && (result.ExpiresAt == nil || resource.ExpiresAt.Before(*result.ExpiresAt)) {
			copy := *resource.ExpiresAt
			result.ExpiresAt = &copy
		}
	}
	return result
}
func firstNumber(object map[string]any, keys ...string) float64 {
	value, _ := firstNumberOK(object, keys...)
	return value
}
func firstNumberOK(object map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := number(object[key]); ok {
			return value, true
		}
	}
	return 0, false
}
func resourceHeaders(credential Credential, origin string) map[string]string {
	headers := authHeaders(credential, origin)
	headers["X-Client-Platform"] = "web"
	headers["Origin"] = origin
	headers["Referer"] = origin + "/profile/plans-usage"
	return headers
}
func responseCode(value map[string]any) int {
	if code, ok := number(value["code"]); ok {
		return int(code)
	}
	if data, ok := value["data"].(map[string]any); ok {
		if code, ok := number(data["code"]); ok {
			return int(code)
		}
	}
	return -1
}
func isSuccessResponse(value map[string]any) bool {
	code := responseCode(value)
	if code == 0 || code == 200 {
		return true
	}
	if code != -1 {
		return false
	}
	_, hasData := value["data"]
	return hasData && value["ok"] != false && value["success"] != false
}
func isUnauthorized(status int, value map[string]any) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	code := responseCode(value)
	return code != 10085 && (code == 401 || code == 403)
}
func isRouteMissing(status int, value map[string]any) bool {
	return status == http.StatusNotFound || responseCode(value) == 404
}
func responseMessage(value map[string]any) string {
	for _, key := range []string{"message", "msg"} {
		if text := stringValue(value[key]); text != "" {
			return text
		}
	}
	if data, ok := value["data"].(map[string]any); ok {
		for _, key := range []string{"message", "msg"} {
			if text := stringValue(data[key]); text != "" {
				return text
			}
		}
	}
	return "响应未提供错误详情"
}
func firstBool(object map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := object[key].(bool); ok {
			return value, true
		}
	}
	return false, false
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
	if variant == VariantGlobal && value != "workbuddy.ai" && value != "www.workbuddy.ai" {
		return fmt.Errorf("登录响应域名与 WorkBuddy 国际版不符")
	}
	if variant == VariantCN && value != "codebuddy.cn" && value != "www.codebuddy.cn" && value != "workbuddy.cn" && value != "www.workbuddy.cn" {
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
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
				if layout == "2006-01-02" {
					parsed = parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				}
				return parsed.UTC(), true
			}
		}
	}
	return time.Time{}, false
}
func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
