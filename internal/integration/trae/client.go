package trae

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/netutil"
	"github.com/google/uuid"
)

const (
	usageURL   = "https://api.trae.cn/trae/api/v2/pay/ide_user_ent_usage"
	clientID   = "ono9krqynydwx5"
	ideVersion = "0.1.61"
	loginTTL   = 15 * time.Minute
)

type Client struct {
	http    *http.Client
	mu      sync.Mutex
	pending map[string]*loginState
}
type Credential struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	DeviceID         string    `json:"deviceId,omitempty"`
	MachineID        string    `json:"machineId,omitempty"`
	DevicePublicKey  string    `json:"devicePublicKey,omitempty"`
	DevicePrivateKey string    `json:"devicePrivateKey,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt,omitempty"`
	UID              string    `json:"uid,omitempty"`
	Nickname         string    `json:"nickname,omitempty"`
}
type Challenge struct {
	State     string
	VerifyURL string
	ExpiresAt time.Time
}
type loginState struct {
	challenge                                Challenge
	verifier, deviceID, machineID, loginHost string
	listener                                 net.Listener
	done                                     bool
	authCode, refreshToken                   string
	err                                      error
}
type Usage struct{ Credits []CreditWindow }
type CreditWindow struct {
	Label            string
	Remaining, Total float64
	ExpiresAt        *time.Time
}
type response struct {
	IsCreditsBilling bool   `json:"is_credits_billing"`
	Packs            []pack `json:"user_entitlement_pack_list"`
}
type pack struct {
	Base struct {
		ProductType  int   `json:"product_type"`
		EndTime      int64 `json:"end_time"`
		IsHide       bool  `json:"is_hide"`
		Status       *int  `json:"status"`
		Quota        quota `json:"quota"`
		ProductExtra struct {
			Subscription struct {
				Quota quota `json:"quota"`
			} `json:"subscription_extra"`
			Package struct {
				Quota quota `json:"quota"`
			} `json:"package_extra"`
		} `json:"product_extra"`
	} `json:"entitlement_base_info"`
	Usage struct {
		CreditsAmount *float64 `json:"credits_amount"`
		BasicAmount   *float64 `json:"basic_usage_amount"`
		BonusAmount   *float64 `json:"bonus_usage_amount"`
	} `json:"usage"`
	DisplayDesc string `json:"display_desc"`
}
type quota struct {
	CreditsLimit *float64 `json:"credits_limit"`
	BasicLimit   *float64 `json:"basic_usage_limit"`
	BonusLimit   *float64 `json:"bonus_usage_limit"`
}

func NewClient() *Client {
	return &Client{http: netutil.NewHTTPClient(30 * time.Second), pending: make(map[string]*loginState)}
}

// StartLogin mirrors the TRAE desktop client's PKCE and loopback-callback
// flow. TRAE validates the callback shape client-side, so a regular web
// callback cannot replace 127.0.0.1:<port>/authorize.
func (c *Client) StartLogin(ctx context.Context) (Challenge, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Challenge{}, fmt.Errorf("无法创建 TRAE 本地授权回调: %w", err)
	}
	state := uuid.NewString()
	verifierBytes := make([]byte, 48)
	if _, err = rand.Read(verifierBytes); err != nil {
		_ = listener.Close()
		return Challenge{}, fmt.Errorf("生成 TRAE PKCE 失败: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	digest := sha256.Sum256([]byte(verifier))
	challengeCode := base64.RawURLEncoding.EncodeToString(digest[:])
	deviceID, err := randomDigits(16)
	if err != nil {
		_ = listener.Close()
		return Challenge{}, err
	}
	machineID := uuid.NewString()
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/authorize", listener.Addr().(*net.TCPAddr).Port)
	loginHost := c.loginGuidance(ctx, state)
	challenge := Challenge{State: state, VerifyURL: buildVerificationURL(loginHost, state, callbackURL, machineID, deviceID, challengeCode), ExpiresAt: time.Now().Add(loginTTL)}
	pending := &loginState{challenge: challenge, verifier: verifier, deviceID: deviceID, machineID: machineID, loginHost: loginHost, listener: listener}
	c.mu.Lock()
	for key, previous := range c.pending {
		_ = previous.listener.Close()
		delete(c.pending, key)
	}
	c.pending[state] = pending
	c.mu.Unlock()
	go c.serveCallback(pending)
	return challenge, nil
}

func (c *Client) PollLogin(ctx context.Context, challenge Challenge) (Credential, bool, error) {
	c.mu.Lock()
	pending, ok := c.pending[challenge.State]
	if !ok {
		c.mu.Unlock()
		return Credential{}, false, fmt.Errorf("TRAE 登录会话不存在或已过期")
	}
	if time.Now().After(pending.challenge.ExpiresAt) {
		delete(c.pending, challenge.State)
		_ = pending.listener.Close()
		c.mu.Unlock()
		return Credential{}, false, fmt.Errorf("TRAE 登录已超时，请重新发起")
	}
	if !pending.done {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Credential{}, false, ctx.Err()
		default:
			return Credential{}, true, nil
		}
	}
	delete(c.pending, challenge.State)
	_ = pending.listener.Close()
	authCode, refreshToken, callbackErr := pending.authCode, pending.refreshToken, pending.err
	c.mu.Unlock()
	if callbackErr != nil {
		return Credential{}, false, callbackErr
	}
	if authCode == "" {
		return Credential{}, false, fmt.Errorf("TRAE 授权回调缺少 authCode；请重新发起登录")
	}
	credential, err := c.exchangeAuthCode(ctx, pending, authCode)
	if err != nil {
		return Credential{}, false, err
	}
	if credential.RefreshToken == "" {
		credential.RefreshToken = refreshToken
	}
	return credential, false, nil
}

func (c *Client) serveCallback(pending *loginState) {
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		params := parseCallback(r.URL.Query())
		c.mu.Lock()
		current, ok := c.pending[pending.challenge.State]
		if ok {
			current.authCode = params.authCode
			current.refreshToken = params.refreshToken
			current.err = params.err
			if current.err == nil && current.authCode == "" && current.refreshToken == "" {
				current.err = fmt.Errorf("TRAE 授权回调没有令牌信息，请重新登录")
			}
			current.done = true
		}
		c.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if params.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, callbackHTML("TRAE 登录失败", params.err.Error()))
			return
		}
		_, _ = io.WriteString(w, callbackHTML("TRAE 登录成功", "真实额度正在写入 SuperMonitor，此页面将自动关闭。"))
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = pending.listener.Close()
		}()
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	_ = server.Serve(pending.listener)
}

type callbackData struct {
	authCode, refreshToken string
	err                    error
}

func parseCallback(values url.Values) callbackData {
	for _, key := range []string{"error", "error_code", "err", "errorCode"} {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			return callbackData{err: fmt.Errorf("TRAE 授权失败: %s", value)}
		}
	}
	if values.Get("isRedirect") == "false" {
		return callbackData{err: fmt.Errorf("TRAE 授权未完成")}
	}
	result := callbackData{}
	for _, key := range []string{"authCode", "auth_code", "AuthCode", "authorization_code", "code"} {
		if result.authCode = strings.TrimSpace(values.Get(key)); result.authCode != "" {
			break
		}
	}
	for _, key := range []string{"refreshToken", "refresh_token", "RefreshToken", "refresh-token"} {
		if result.refreshToken = strings.TrimSpace(values.Get(key)); result.refreshToken != "" {
			break
		}
	}
	if result.authCode == "" {
		for _, key := range []string{"authCodeInfo", "auth_code_info", "AuthCodeInfo"} {
			if result.authCode = authCodeFromJSON(values.Get(key)); result.authCode != "" {
				break
			}
		}
	}
	return result
}

func authCodeFromJSON(raw string) string {
	for _, candidate := range []string{raw, queryUnescape(raw)} {
		var value map[string]any
		if json.Unmarshal([]byte(candidate), &value) != nil {
			continue
		}
		if code := str(value, "authCode", "auth_code", "AuthCode", "authorization_code", "code"); code != "" {
			return code
		}
	}
	return ""
}

func queryUnescape(value string) string { decoded, _ := url.QueryUnescape(value); return decoded }

func (c *Client) loginGuidance(ctx context.Context, traceID string) string {
	body, _ := json.Marshal(map[string]string{"loginTraceID": traceID, "login_trace_id": traceID})
	for _, endpoint := range []string{
		"https://api.trae.cn/cloudide/api/v3/trae/GetLoginGuidance",
		"https://api.trae.com.cn/cloudide/api/v3/trae/GetLoginGuidance",
		"https://www.trae.cn/cloudide/api/v3/trae/GetLoginGuidance",
	} {
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Trae/1.0.0 SuperMonitor/0.6.0")
		resp, err := c.http.Do(req)
		if err != nil {
			cancel()
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if host := extractLoginHost(raw); host != "" {
				return host
			}
		}
	}
	return "https://api.trae.cn"
}

func buildVerificationURL(loginHost, traceID, callbackURL, machineID, deviceID, codeChallenge string) string {
	loginHost = strings.TrimRight(loginHost, "/")
	if !strings.HasPrefix(loginHost, "http://") && !strings.HasPrefix(loginHost, "https://") {
		loginHost = "https://" + loginHost
	}
	params := []string{
		"login_version=1", "auth_from=trae", "login_channel=native_ide", "plugin_version=1.0.0",
		"auth_type=local", "client_id=" + clientID, "redirect=0", "login_trace_id=" + url.QueryEscape(traceID),
		"auth_callback_url=" + callbackURL, "machine_id=" + url.QueryEscape(machineID), "device_id=" + url.QueryEscape(deviceID),
		"x_device_id=" + url.QueryEscape(deviceID), "x_machine_id=" + url.QueryEscape(machineID), "x_device_brand=83DG",
		"x_device_type=windows", "x_os_version=" + url.QueryEscape("Windows 11 Pro"), "x_env=prod", "x_app_version=" + ideVersion,
		"x_app_type=trae", "code_challenge=" + url.QueryEscape(codeChallenge), "code_challenge_method=S256",
	}
	return loginHost + "/authorization?" + strings.Join(params, "&")
}

func extractLoginHost(raw []byte) string {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	keys := []string{"LoginHost", "loginHost", "LoginURL", "loginUrl", "login_url"}
	for _, current := range []map[string]any{root, mapAt(root, "Result"), mapAt(root, "result"), mapAt(root, "data"), mapAt(mapAt(root, "data"), "Result"), mapAt(mapAt(root, "data"), "result")} {
		if current == nil {
			continue
		}
		if value := str(current, keys...); value != "" {
			return value
		}
	}
	return ""
}

func mapAt(value map[string]any, key string) map[string]any {
	result, _ := value[key].(map[string]any)
	return result
}

func (c *Client) exchangeAuthCode(ctx context.Context, state *loginState, authCode string) (Credential, error) {
	publicKey, privateKey, err := generateDeviceKeyPair()
	if err != nil {
		return Credential{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"ClientID": clientID, "AuthCode": authCode, "CodeVerifier": state.verifier, "IDEVersion": ideVersion,
		"DeviceInfo": map[string]any{
			"DeviceID": state.deviceID, "MachineID": state.machineID, "PlatformCode": "IDE_PC", "DeviceType": "PC",
			"DeviceName": "DESKTOP-SUPMON", "DeviceModel": "83DG", "ClientVersion": ideVersion,
			"DevicePublicKey": publicKey, "DeviceBrand": "Microsoft", "DeviceCPU": "", "OSInfo": "windows", "OSVersion": "Windows 11 Pro",
		},
	})
	urls := []string{"https://api.trae.cn/trae/api/v3/oauth/ExchangeToken", "https://api.trae.com.cn/trae/api/v3/oauth/ExchangeToken"}
	if candidate := exchangeURLForHost(state.loginHost); candidate != "" {
		urls = append(urls, candidate)
	}
	var failures []string
	for _, endpoint := range uniqueStrings(urls) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Trae/1.0.0 SuperMonitor/0.6.0")
		resp, err := c.http.Do(req)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			failures = append(failures, fmt.Sprintf("%s: HTTP %d", req.URL.Host, resp.StatusCode))
			continue
		}
		credential := parseExchangeCredential(raw)
		if credential.AccessToken == "" {
			failures = append(failures, req.URL.Host+": 响应缺少 access token")
			continue
		}
		credential.DeviceID, credential.MachineID = state.deviceID, state.machineID
		credential.DevicePublicKey, credential.DevicePrivateKey = publicKey, privateKey
		return credential, nil
	}
	return Credential{}, fmt.Errorf("TRAE ExchangeToken 失败: %s", strings.Join(failures, "；"))
}

func exchangeURLForHost(raw string) string {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/trae/api/v3/oauth/ExchangeToken"
}

func parseExchangeCredential(raw []byte) Credential {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return Credential{}
	}
	result := mapAt(root, "Result")
	if result == nil {
		result = mapAt(root, "result")
	}
	if result == nil {
		result = root
	}
	credential := Credential{AccessToken: str(result, "AccessToken", "accessToken", "Token", "token"), RefreshToken: str(result, "RefreshToken", "refreshToken")}
	if expires := numberAt(result, "TokenExpireAt"); expires > 0 {
		if expires > 1e12 {
			expires /= 1000
		}
		credential.ExpiresAt = time.Unix(expires, 0).UTC()
	} else if duration := numberAt(result, "TokenExpireDuration"); duration > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(duration) * time.Second).UTC()
	}
	return credential
}

func numberAt(value map[string]any, key string) int64 {
	switch current := value[key].(type) {
	case float64:
		return int64(current)
	case json.Number:
		result, _ := current.Int64()
		return result
	}
	return 0
}

func generateDeviceKeyPair() (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("生成 TRAE 设备密钥失败: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})), nil
}

func randomDigits(length int) (string, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成 TRAE 设备 ID 失败: %w", err)
	}
	for index := range buffer {
		buffer[index] = '0' + buffer[index]%10
	}
	return string(buffer), nil
}

func uniqueStrings(values []string) []string {
	seen, result := map[string]bool{}, make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func callbackHTML(title, detail string) string {
	return "<!doctype html><meta charset=utf-8><title>" + html.EscapeString(title) + "</title><style>body{font:16px system-ui;max-width:620px;margin:12vh auto;padding:24px;color:#18221d}main{border:1px solid #dfe5e1;border-radius:16px;padding:28px;box-shadow:0 18px 50px #1d3b2b12}p{line-height:1.7;color:#5b6961}</style><main><h1>" + html.EscapeString(title) + "</h1><p>" + html.EscapeString(detail) + "</p></main><script>setTimeout(()=>window.close(),900)</script>"
}

func ParseCredential(raw []byte) (Credential, error) {
	if len(raw) == 0 || len(raw) > 2*1024*1024 {
		return Credential{}, fmt.Errorf("TRAE 认证文件为空或超过 2 MB")
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return Credential{}, fmt.Errorf("TRAE 认证文件不是有效 JSON: %w", err)
	}
	var result Credential
	walk(root, func(m map[string]any) bool {
		result.AccessToken = str(m, "accessToken", "access_token", "token", "jwt")
		if result.AccessToken == "" {
			return false
		}
		result.RefreshToken = str(m, "refreshToken", "refresh_token")
		result.DeviceID = str(m, "deviceId", "device_id", "did")
		result.MachineID = str(m, "machineId", "machine_id")
		result.DevicePublicKey = str(m, "devicePublicKey", "device_public_key")
		result.DevicePrivateKey = str(m, "devicePrivateKey", "device_private_key")
		result.UID = str(m, "uid", "userId", "user_id")
		result.Nickname = str(m, "nickname", "name", "email")
		if expires := numberAt(m, "expiresAt"); expires > 0 {
			if expires > 1e12 {
				expires /= 1000
			}
			result.ExpiresAt = time.Unix(expires, 0).UTC()
		}
		return true
	})
	if result.AccessToken == "" {
		return Credential{}, fmt.Errorf("TRAE 认证文件缺少 accessToken")
	}
	return result, nil
}

func (c *Client) FetchUsage(ctx context.Context, credential *Credential) (Usage, bool, error) {
	usage, status, err := c.fetchUsage(ctx, *credential)
	if err == nil {
		return usage, false, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return Usage{}, false, err
	}
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return Usage{}, false, err
	}
	refreshed, refreshErr := c.Refresh(ctx, *credential)
	if refreshErr != nil {
		return Usage{}, false, fmt.Errorf("TRAE 登录已过期，自动刷新失败: %w", refreshErr)
	}
	*credential = refreshed
	usage, _, err = c.fetchUsage(ctx, *credential)
	return usage, true, err
}

func (c *Client) fetchUsage(ctx context.Context, credential Credential) (Usage, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, usageURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return Usage{}, 0, err
	}
	req.Header.Set("Authorization", "Cloud-IDE-JWT "+credential.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "VSCode 1.107.1 (TRAE CN)")
	req.Header.Set("X-User-Region", "CN")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Package-Type", "stable_cn")
	req.Header.Set("X-Market-Client-Id", "VSCode 1.107.1")
	req.Header.Set("X-Device-Type", "windows")
	req.Header.Set("X-OS-Version", "Windows 11 Pro")
	req.Header.Set("App-Version", "0.1.61")
	if credential.DeviceID != "" {
		req.Header.Set("X-Device-Id", credential.DeviceID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Usage{}, 0, fmt.Errorf("连接 TRAE 官方额度接口失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Usage{}, resp.StatusCode, fmt.Errorf("TRAE 额度接口返回 HTTP %d", resp.StatusCode)
	}
	var payload response
	if err := json.Unmarshal(body, &payload); err != nil {
		return Usage{}, resp.StatusCode, fmt.Errorf("解析 TRAE 额度响应失败: %w", err)
	}
	windows := make([]CreditWindow, 0, len(payload.Packs))
	for i, p := range payload.Packs {
		if p.Base.ProductType == 3 || p.Base.IsHide || (p.Base.Status != nil && *p.Base.Status == 3) {
			continue
		}
		q := effectiveQuota(p)
		var total, used float64
		if q.CreditsLimit != nil {
			if *q.CreditsLimit < 0 {
				continue
			}
			total = *q.CreditsLimit
			if p.Usage.CreditsAmount != nil {
				used = *p.Usage.CreditsAmount
			}
		} else if q.BasicLimit != nil {
			total = *q.BasicLimit
			if q.BonusLimit != nil {
				total += *q.BonusLimit
			}
			if p.Usage.BasicAmount != nil {
				used += *p.Usage.BasicAmount
			}
			if p.Usage.BonusAmount != nil {
				used += *p.Usage.BonusAmount
			}
		} else {
			continue
		}
		remaining := total - used
		if remaining < 0 {
			remaining = 0
		}
		label := strings.TrimSpace(p.DisplayDesc)
		if label == "" {
			label = fmt.Sprintf("权益包 %d", i+1)
		}
		var expires *time.Time
		if p.Base.EndTime > 0 {
			seconds := p.Base.EndTime
			if seconds > 1e12 {
				seconds /= 1000
			}
			t := time.Unix(seconds, 0).UTC()
			expires = &t
		}
		windows = append(windows, CreditWindow{Label: label, Remaining: remaining, Total: total, ExpiresAt: expires})
	}
	if len(windows) == 0 {
		return Usage{}, resp.StatusCode, fmt.Errorf("TRAE 额度响应没有可显示的积分包")
	}
	return Usage{Credits: windows}, resp.StatusCode, nil
}

func (c *Client) Refresh(ctx context.Context, credential Credential) (Credential, error) {
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return Credential{}, fmt.Errorf("TRAE 凭据缺少 refreshToken")
	}
	body, _ := json.Marshal(map[string]string{"ClientID": clientID, "RefreshToken": credential.RefreshToken, "ClientSecret": "-", "UserID": ""})
	var failures []string
	for _, endpoint := range []string{"https://api.trae.cn/cloudide/api/v3/trae/oauth/ExchangeToken", "https://api.trae.com.cn/cloudide/api/v3/trae/oauth/ExchangeToken"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Trae/"+ideVersion+" SuperMonitor/0.6.0")
		resp, err := c.http.Do(req)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			failures = append(failures, fmt.Sprintf("%s: HTTP %d", req.URL.Host, resp.StatusCode))
			continue
		}
		updated := parseExchangeCredential(raw)
		if updated.AccessToken == "" {
			failures = append(failures, req.URL.Host+": 响应缺少 token")
			continue
		}
		if updated.RefreshToken == "" {
			updated.RefreshToken = credential.RefreshToken
		}
		updated.DeviceID, updated.MachineID = credential.DeviceID, credential.MachineID
		updated.DevicePublicKey, updated.DevicePrivateKey = credential.DevicePublicKey, credential.DevicePrivateKey
		updated.UID, updated.Nickname = credential.UID, credential.Nickname
		return updated, nil
	}
	return Credential{}, fmt.Errorf("TRAE refreshToken 轮换失败: %s", strings.Join(failures, "；"))
}
func effectiveQuota(p pack) quota {
	if p.Base.Quota.CreditsLimit != nil || p.Base.Quota.BasicLimit != nil {
		return p.Base.Quota
	}
	if p.Base.ProductExtra.Subscription.Quota.CreditsLimit != nil || p.Base.ProductExtra.Subscription.Quota.BasicLimit != nil {
		return p.Base.ProductExtra.Subscription.Quota
	}
	return p.Base.ProductExtra.Package.Quota
}
func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func walk(v any, visit func(map[string]any) bool) bool {
	switch x := v.(type) {
	case map[string]any:
		if visit(x) {
			return true
		}
		for _, c := range x {
			if walk(c, visit) {
				return true
			}
		}
	case []any:
		for _, c := range x {
			if walk(c, visit) {
				return true
			}
		}
	}
	return false
}
