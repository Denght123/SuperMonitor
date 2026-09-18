package genericquota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

type Client struct{ http *http.Client }

type Credential struct {
	Endpoint      string `json:"endpoint"`
	Method        string `json:"method"`
	AuthType      string `json:"authType"`
	HeaderName    string `json:"headerName,omitempty"`
	HeaderPrefix  string `json:"headerPrefix,omitempty"`
	Secret        string `json:"secret,omitempty"`
	RequestBody   string `json:"requestBody,omitempty"`
	ValuePath     string `json:"valuePath"`
	TotalPath     string `json:"totalPath,omitempty"`
	ResetAtPath   string `json:"resetAtPath,omitempty"`
	ExpiresAtPath string `json:"expiresAtPath,omitempty"`
	Label         string `json:"label"`
	Unit          string `json:"unit"`
	Kind          string `json:"kind"`
}

type Reading struct {
	Value     float64
	Total     *float64
	ResetAt   *time.Time
	ExpiresAt *time.Time
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(20 * time.Second)} }

func (credential *Credential) Normalize() error {
	credential.Endpoint = strings.TrimSpace(credential.Endpoint)
	parsed, err := url.Parse(credential.Endpoint)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("额度接口 URL 无效")
	}
	if parsed.User != nil {
		return fmt.Errorf("额度接口 URL 不能内嵌用户名或密码")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return fmt.Errorf("额度接口必须使用 HTTPS；本机调试允许 HTTP localhost")
	}
	credential.Method = strings.ToUpper(strings.TrimSpace(credential.Method))
	if credential.Method == "" {
		credential.Method = http.MethodGet
	}
	if credential.Method != http.MethodGet && credential.Method != http.MethodPost {
		return fmt.Errorf("额度接口只支持 GET 或 POST")
	}
	credential.AuthType = strings.ToLower(strings.TrimSpace(credential.AuthType))
	if credential.AuthType == "" {
		credential.AuthType = "bearer"
	}
	if credential.AuthType != "bearer" && credential.AuthType != "header" && credential.AuthType != "cookie" && credential.AuthType != "none" {
		return fmt.Errorf("认证类型必须是 bearer、header、cookie 或 none")
	}
	credential.HeaderName = strings.TrimSpace(credential.HeaderName)
	if strings.ContainsAny(credential.HeaderName, "\r\n") {
		return fmt.Errorf("认证 Header 名称无效")
	}
	if credential.AuthType == "bearer" {
		credential.HeaderName = "Authorization"
		credential.HeaderPrefix = "Bearer "
	}
	if credential.AuthType == "header" && credential.HeaderName == "" {
		credential.HeaderName = "Authorization"
	}
	if credential.AuthType != "none" && strings.TrimSpace(credential.Secret) == "" {
		return fmt.Errorf("认证内容不能为空")
	}
	credential.ValuePath = strings.TrimSpace(credential.ValuePath)
	if credential.ValuePath == "" {
		return fmt.Errorf("剩余额度字段路径不能为空")
	}
	credential.Label = strings.TrimSpace(credential.Label)
	if credential.Label == "" {
		credential.Label = "剩余额度"
	}
	credential.Unit = strings.TrimSpace(credential.Unit)
	if credential.Unit == "" {
		credential.Unit = "credits"
	}
	credential.Kind = strings.TrimSpace(credential.Kind)
	if credential.Kind == "" {
		credential.Kind = "quota"
	}
	if credential.Method == http.MethodPost && strings.TrimSpace(credential.RequestBody) != "" && !json.Valid([]byte(credential.RequestBody)) {
		return fmt.Errorf("POST 请求体不是有效 JSON")
	}
	return nil
}

func (c *Client) Fetch(ctx context.Context, credential Credential) (Reading, error) {
	if err := credential.Normalize(); err != nil {
		return Reading{}, err
	}
	var body io.Reader
	if credential.Method == http.MethodPost {
		payload := strings.TrimSpace(credential.RequestBody)
		if payload == "" {
			payload = "{}"
		}
		body = bytes.NewBufferString(payload)
	}
	request, err := http.NewRequestWithContext(ctx, credential.Method, credential.Endpoint, body)
	if err != nil {
		return Reading{}, err
	}
	request.Header.Set("Accept", "application/json")
	if credential.Method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	switch credential.AuthType {
	case "bearer", "header":
		request.Header.Set(credential.HeaderName, credential.HeaderPrefix+credential.Secret)
	case "cookie":
		request.Header.Set("Cookie", credential.Secret)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Reading{}, fmt.Errorf("连接额度接口失败: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return Reading{}, fmt.Errorf("读取额度接口响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := strings.TrimSpace(string(raw))
		if len(detail) > 300 {
			detail = detail[:300] + "…"
		}
		return Reading{}, fmt.Errorf("额度接口返回 HTTP %d: %s", response.StatusCode, detail)
	}
	var root any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return Reading{}, fmt.Errorf("额度接口响应不是有效 JSON: %w", err)
	}
	value, ok := numberAt(root, credential.ValuePath)
	if !ok {
		return Reading{}, fmt.Errorf("额度响应中找不到数值字段 %s", credential.ValuePath)
	}
	reading := Reading{Value: value}
	if credential.TotalPath != "" {
		if total, ok := numberAt(root, credential.TotalPath); ok && total > 0 {
			reading.Total = &total
		} else {
			return Reading{}, fmt.Errorf("额度响应中找不到有效总量字段 %s", credential.TotalPath)
		}
	}
	if credential.ResetAtPath != "" {
		if parsed, ok := timeAt(root, credential.ResetAtPath); ok {
			reading.ResetAt = &parsed
		} else {
			return Reading{}, fmt.Errorf("额度响应中无法解析重置时间字段 %s", credential.ResetAtPath)
		}
	}
	if credential.ExpiresAtPath != "" {
		if parsed, ok := timeAt(root, credential.ExpiresAtPath); ok {
			reading.ExpiresAt = &parsed
		} else {
			return Reading{}, fmt.Errorf("额度响应中无法解析到期时间字段 %s", credential.ExpiresAtPath)
		}
	}
	return reading, nil
}

func valueAt(root any, path string) (any, bool) {
	current := root
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch value := current.(type) {
		case map[string]any:
			current, _ = value[part]
			if current == nil {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func numberAt(root any, path string) (float64, bool) {
	value, ok := valueAt(root, path)
	if !ok {
		return 0, false
	}
	switch number := value.(type) {
	case json.Number:
		result, err := number.Float64()
		return result, err == nil
	case float64:
		return number, true
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func timeAt(root any, path string) (time.Time, bool) {
	value, ok := valueAt(root, path)
	if !ok {
		return time.Time{}, false
	}
	if number, ok := numberValue(value); ok {
		seconds := int64(number)
		if seconds > 10_000_000_000 {
			seconds /= 1000
		}
		return time.Unix(seconds, 0).UTC(), true
	}
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(text), time.Local); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func numberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		result, err := number.Float64()
		return result, err == nil
	case float64:
		return number, true
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return result, err == nil
	default:
		return 0, false
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
