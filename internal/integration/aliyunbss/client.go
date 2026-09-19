package aliyunbss

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- Aliyun RPC v1 requires HMAC-SHA1.
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
)

const endpoint = "https://business.aliyuncs.com/"

type Client struct{ http *http.Client }
type Credential struct {
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
}
type Balance struct {
	Available float64
	Cash      *float64
	Currency  string
}

func NewClient() *Client { return &Client{http: netutil.NewHTTPClient(30 * time.Second)} }

func Normalize(accessKeyID, accessKeySecret string) (Credential, error) {
	credential := Credential{AccessKeyID: strings.TrimSpace(accessKeyID), AccessKeySecret: strings.TrimSpace(accessKeySecret)}
	if credential.AccessKeyID == "" || credential.AccessKeySecret == "" {
		return Credential{}, fmt.Errorf("请同时填写阿里云 RAM AccessKey ID 与 AccessKey Secret")
	}
	return credential, nil
}

func (c *Client) FetchBalance(ctx context.Context, credential Credential) (Balance, error) {
	params := map[string]string{
		"Action":           "QueryAccountBalance",
		"Version":          "2017-12-14",
		"Format":           "JSON",
		"AccessKeyId":      credential.AccessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   randomNonce(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	params["Signature"] = sign(params, credential.AccessKeySecret)
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return Balance{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Balance{}, fmt.Errorf("连接阿里云 BSS 官方余额接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	var payload struct {
		Code      any    `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
		Data      struct {
			AvailableAmount     string `json:"AvailableAmount"`
			AvailableCashAmount string `json:"AvailableCashAmount"`
			Currency            string `json:"Currency"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Balance{}, fmt.Errorf("阿里云余额响应不是有效 JSON（HTTP %d）", resp.StatusCode)
	}
	code := fmt.Sprint(payload.Code)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (code != "200" && code != "<nil>") {
		switch code {
		case "NotAuthorized":
			return Balance{}, fmt.Errorf("RAM 用户没有费用中心只读权限，请授予 AliyunBSSReadOnlyAccess")
		case "InvalidAccessKeyId.NotFound":
			return Balance{}, fmt.Errorf("AccessKey ID 无效或已被停用")
		case "SignatureDoesNotMatch":
			return Balance{}, fmt.Errorf("AccessKey Secret 不正确或复制不完整")
		default:
			return Balance{}, fmt.Errorf("阿里云余额接口返回 %s（HTTP %d）: %s", code, resp.StatusCode, strings.TrimSpace(payload.Message))
		}
	}
	available, err := strconv.ParseFloat(payload.Data.AvailableAmount, 64)
	if err != nil {
		return Balance{}, fmt.Errorf("阿里云余额响应缺少 Data.AvailableAmount")
	}
	currency := strings.ToUpper(strings.TrimSpace(payload.Data.Currency))
	if currency == "" {
		currency = "CNY"
	}
	result := Balance{Available: available, Currency: currency}
	if payload.Data.AvailableCashAmount != "" {
		if cash, parseErr := strconv.ParseFloat(payload.Data.AvailableCashAmount, 64); parseErr == nil {
			result.Cash = &cash
		}
	}
	return result, nil
}

func sign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, percentEncode(key)+"="+percentEncode(params[key]))
	}
	stringToSign := "GET&%2F&" + percentEncode(strings.Join(parts, "&"))
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func percentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return strings.ReplaceAll(encoded, "*", "%2A")
}
func randomNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
