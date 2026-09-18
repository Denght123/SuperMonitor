package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/integration/claude"
	"github.com/Denght123/SuperMonitor/internal/integration/codex"
	"github.com/Denght123/SuperMonitor/internal/integration/deepseek"
	"github.com/Denght123/SuperMonitor/internal/integration/gemini"
	"github.com/Denght123/SuperMonitor/internal/integration/genericquota"
	"github.com/Denght123/SuperMonitor/internal/integration/mimo"
	"github.com/Denght123/SuperMonitor/internal/integration/tokenrhythm"
	"github.com/Denght123/SuperMonitor/internal/integration/workbuddy"
	"github.com/Denght123/SuperMonitor/internal/integration/zhipu"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
	"github.com/google/uuid"
)

type Accounts struct {
	store       *sqlite.Store
	vault       *secure.Vault
	claude      *claude.Client
	codex       *codex.Client
	deepseek    *deepseek.Client
	gemini      *gemini.Client
	generic     *genericquota.Client
	mimo        *mimo.Client
	tokenrhythm *tokenrhythm.Client
	workbuddy   *workbuddy.Client
	zhipu       *zhipu.Client
	events      *EventHub
	mu          sync.RWMutex
	sessions    map[string]*DeviceLoginSession
}

type DeviceLoginSession struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	UserCode  string    `json:"userCode,omitempty"`
	VerifyURL string    `json:"verifyUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
	AccountID string    `json:"accountId,omitempty"`
	Message   string    `json:"message"`
}

func NewAccounts(store *sqlite.Store, vault *secure.Vault, events *EventHub) *Accounts {
	return &Accounts{store: store, vault: vault, claude: claude.NewClient(), codex: codex.NewClient(), deepseek: deepseek.NewClient(), gemini: gemini.NewClient(), generic: genericquota.NewClient(), mimo: mimo.NewClient(), tokenrhythm: tokenrhythm.NewClient(), workbuddy: workbuddy.NewClient(), zhipu: zhipu.NewClient(), events: events, sessions: make(map[string]*DeviceLoginSession)}
}

func (s *Accounts) ImportCodex(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := codex.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	if credential.AccessToken == "" && credential.RefreshToken != "" {
		credential, err = s.codex.Refresh(ctx, credential.RefreshToken)
		if err != nil {
			return domain.AccountSummary{}, fmt.Errorf("OAuth 文件仅含 refresh_token，但刷新失败: %w", err)
		}
	}
	return s.connectCodexWithID(ctx, "codex-"+uuid.NewString(), alias, "credential_import", credential)
}

func (s *Accounts) ImportWorkBuddy(ctx context.Context, providerID, alias string, raw []byte) (domain.AccountSummary, error) {
	variant, err := workbuddy.ProviderVariant(providerID)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	credential, err := workbuddy.ParseCredential(raw, variant)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectWorkBuddyWithID(ctx, providerID+"-"+uuid.NewString(), providerID, alias, "credential_import", credential)
}

func (s *Accounts) ImportClaude(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := claude.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectClaudeWithID(ctx, "claude-code-"+uuid.NewString(), alias, credential)
}

func (s *Accounts) ImportGemini(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := gemini.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectGeminiWithID(ctx, "gemini-cli-"+uuid.NewString(), alias, credential)
}

func (s *Accounts) ConnectDeepSeek(ctx context.Context, alias, apiKey string) (domain.AccountSummary, error) {
	return s.connectDeepSeekWithID(ctx, "deepseek-"+uuid.NewString(), alias, deepseek.Credential{APIKey: strings.TrimSpace(apiKey)})
}

func (s *Accounts) ConnectMimo(ctx context.Context, alias, cookie string) (domain.AccountSummary, error) {
	normalized, err := mimo.NormalizeCookie(cookie)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectMimoWithID(ctx, "mimo-"+uuid.NewString(), alias, mimo.Credential{Cookie: normalized})
}

func (s *Accounts) ConnectZhipu(ctx context.Context, alias, apiKey string) (domain.AccountSummary, error) {
	return s.connectZhipuWithID(ctx, "zhipu-"+uuid.NewString(), alias, zhipu.Credential{APIKey: strings.TrimSpace(apiKey)})
}

func (s *Accounts) ConnectTokenRhythm(ctx context.Context, alias, rawCredential string) (domain.AccountSummary, error) {
	credential, err := tokenrhythm.NormalizeCredential(rawCredential)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectTokenRhythmWithID(ctx, "tokenrhythm-"+uuid.NewString(), alias, credential)
}

func (s *Accounts) ConnectGeneric(ctx context.Context, providerID, alias string, credential genericquota.Credential) (domain.AccountSummary, error) {
	if _, _, supported := providerMeta(providerID); !supported {
		return domain.AccountSummary{}, fmt.Errorf("未知平台 %s", providerID)
	}
	return s.connectGenericWithID(ctx, providerID+"-"+uuid.NewString(), providerID, alias, credential)
}

func (s *Accounts) StartCodexDeviceLogin(ctx context.Context, alias string) (DeviceLoginSession, error) {
	challenge, err := s.codex.StartDeviceLogin(ctx)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{ID: uuid.NewString(), Provider: "codex", Status: "pending", UserCode: challenge.UserCode, VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待你在 OpenAI 页面确认登录"}
	s.saveSession(&session)
	go func(sessionID string) {
		loginCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
		defer cancel()
		credential, loginErr := s.codex.CompleteDeviceLogin(loginCtx, challenge)
		if loginErr != nil {
			s.updateDeviceSession(sessionID, "failed", "", friendlyLoginError(loginErr))
			return
		}
		account, connectErr := s.connectCodexWithID(loginCtx, "codex-"+uuid.NewString(), alias, "device_code", credential)
		if connectErr != nil {
			s.updateDeviceSession(sessionID, "failed", "", connectErr.Error())
			return
		}
		s.updateDeviceSession(sessionID, "completed", account.ID, "登录成功，实时额度已写入账号池")
	}(session.ID)
	return session, nil
}

func (s *Accounts) StartWorkBuddyOAuth(ctx context.Context, providerID, alias string) (DeviceLoginSession, error) {
	variant, err := workbuddy.ProviderVariant(providerID)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	challenge, err := s.workbuddy.StartOAuth(ctx, variant)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{ID: uuid.NewString(), Provider: providerID, Status: "pending", VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待在 WorkBuddy 官方页面完成扫码登录"}
	s.saveSession(&session)
	go func(sessionID string) {
		loginCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
		defer cancel()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			credential, pending, pollErr := s.workbuddy.PollOAuth(loginCtx, challenge)
			if pollErr != nil {
				s.updateDeviceSession(sessionID, "failed", "", friendlyLoginError(pollErr))
				return
			}
			if !pending {
				account, connectErr := s.connectWorkBuddyWithID(loginCtx, providerID+"-"+uuid.NewString(), providerID, alias, "oauth_qr", credential)
				if connectErr != nil {
					s.updateDeviceSession(sessionID, "failed", "", connectErr.Error())
					return
				}
				s.updateDeviceSession(sessionID, "completed", account.ID, "登录成功，真实积分已写入账号池")
				return
			}
			select {
			case <-loginCtx.Done():
				s.updateDeviceSession(sessionID, "failed", "", "登录已超时，请重新发起")
				return
			case <-ticker.C:
			}
		}
	}(session.ID)
	return session, nil
}

func (s *Accounts) DeviceLoginStatus(id string) (DeviceLoginSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return DeviceLoginSession{}, false
	}
	return *session, true
}

func (s *Accounts) RefreshOne(ctx context.Context, id string) (domain.AccountSummary, error) {
	account, encrypted, err := s.store.ConnectedAccount(ctx, id)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	plain, err := s.vault.Decrypt(encrypted)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	switch account.ProviderID {
	case "claude-code":
		var credential claude.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectClaudeWithID(ctx, account.ID, account.Alias, credential)
	case "codex":
		var credential codex.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectCodexWithID(ctx, account.ID, account.Alias, account.AuthMethod, credential)
	case "deepseek":
		var credential deepseek.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectDeepSeekWithID(ctx, account.ID, account.Alias, credential)
	case "gemini-cli":
		var credential gemini.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectGeminiWithID(ctx, account.ID, account.Alias, credential)
	case "mimo":
		var credential mimo.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectMimoWithID(ctx, account.ID, account.Alias, credential)
	case "tokenrhythm":
		var credential tokenrhythm.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectTokenRhythmWithID(ctx, account.ID, account.Alias, credential)
	case "workbuddy-cn", "workbuddy-global":
		var credential workbuddy.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectWorkBuddyWithID(ctx, account.ID, account.ProviderID, account.Alias, account.AuthMethod, credential)
	case "zhipu":
		var credential zhipu.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectZhipuWithID(ctx, account.ID, account.Alias, credential)
	case "trae-cn", "qoder-cn", "coze-cn", "bailian", "qoder-global", "kiro", "cursor":
		var credential genericquota.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectGenericWithID(ctx, account.ID, account.ProviderID, account.Alias, credential)
	default:
		return domain.AccountSummary{}, fmt.Errorf("平台 %s 尚未实现真实刷新", account.ProviderID)
	}
}

func (s *Accounts) connectClaudeWithID(ctx context.Context, id, alias string, credential claude.Credential) (domain.AccountSummary, error) {
	usage, err := s.claude.FetchUsage(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 Claude Code 额度: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = "Claude Code 账号"
	}
	windows := make([]domain.QuotaSignal, 0, len(usage.Windows))
	for index, window := range usage.Windows {
		remaining, total := window.RemainingPercent, 100.0
		windows = append(windows, domain.QuotaSignal{ID: fmt.Sprintf("claude-live-%d", index), Provider: "Claude Code", Label: window.Label, Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, ResetAt: window.ResetAt, Status: quotaStatus(remaining), Source: "Anthropic /api/oauth/usage", Confidence: "live"})
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "claude-code", alias, "", firstNonEmpty(usage.Plan, "Subscription"), "credential_import", "Claude Code OAuth Usage", windows, now)
	return s.persist(ctx, account, credential, "Claude Code")
}

func (s *Accounts) connectGeminiWithID(ctx context.Context, id, alias string, credential gemini.Credential) (domain.AccountSummary, error) {
	usage, _, err := s.gemini.FetchUsage(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 Gemini Code Assist 额度: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = "Gemini Code Assist"
	}
	windows := make([]domain.QuotaSignal, 0, len(usage.Windows))
	for index, window := range usage.Windows {
		remaining, total := window.RemainingPercent, 100.0
		windows = append(windows, domain.QuotaSignal{ID: fmt.Sprintf("gemini-live-%d", index), Provider: "Gemini", Label: window.Label, Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, ResetAt: window.ResetAt, Status: quotaStatus(remaining), Source: "Gemini Code Assist retrieveUserQuota", Confidence: "live"})
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "gemini-cli", alias, "", "Code Assist", "credential_import", "Gemini Code Assist", windows, now)
	return s.persist(ctx, account, credential, "Gemini")
}

func (s *Accounts) RefreshAll(ctx context.Context) error {
	ids, err := s.store.ConnectedAccountIDs(ctx)
	if err != nil {
		return err
	}
	var failures []string
	for _, id := range ids {
		if _, err := s.RefreshOne(ctx, id); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d 个真实账号刷新失败: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

func (s *Accounts) connectCodexWithID(ctx context.Context, id, alias, authMethod string, credential codex.Credential) (domain.AccountSummary, error) {
	usage, _, err := s.codex.FetchUsage(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 Codex 额度: %s", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(credential.Email, "Codex 账号")
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "codex", alias, credential.Email, usage.Plan, authMethod, "Codex 实时接口", codexWindows(usage), now)
	return s.persist(ctx, account, credential, "Codex")
}

func (s *Accounts) connectDeepSeekWithID(ctx context.Context, id, alias string, credential deepseek.Credential) (domain.AccountSummary, error) {
	balance, err := s.deepseek.FetchBalance(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	if strings.TrimSpace(alias) == "" {
		alias = "DeepSeek API"
	}
	status := "healthy"
	if !balance.Available {
		status = "critical"
	}
	now := time.Now().UTC().Truncate(time.Second)
	windows := []domain.QuotaSignal{{ID: "deepseek-live-balance", Provider: "DeepSeek", Label: "账户余额", Kind: "balance", Value: balance.Total, Unit: balance.Currency, Status: status, Source: "DeepSeek /user/balance", Confidence: "live"}}
	account := newConnected(id, "deepseek", alias, "", "API Balance", "api_key", "DeepSeek 官方接口", windows, now)
	account.Status = status
	return s.persist(ctx, account, credential, "DeepSeek")
}

func (s *Accounts) connectMimoWithID(ctx context.Context, id, alias string, credential mimo.Credential) (domain.AccountSummary, error) {
	usage, err := s.mimo.FetchUsage(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	if strings.TrimSpace(alias) == "" {
		alias = "MiMo Token Plan"
	}
	total, remaining := usage.Limit, usage.Limit-usage.Used
	if remaining < 0 {
		remaining = 0
	}
	signal := domain.QuotaSignal{ID: "mimo-live-plan", Provider: "小米 MiMo", Label: "月度 Token Plan", Kind: "token_plan", Value: remaining, Unit: "credits", RemainingPercent: &usage.RemainingPercent, ResetAt: usage.ResetAt, Status: quotaStatus(usage.RemainingPercent), Source: "MiMo 控制台 /tokenPlan/usage", Confidence: "live"}
	if total > 0 {
		signal.Total = &total
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "mimo", alias, "", "Token Plan", "cookie", "MiMo 官方控制台", []domain.QuotaSignal{signal}, now)
	return s.persist(ctx, account, credential, "MiMo")
}

func (s *Accounts) connectZhipuWithID(ctx context.Context, id, alias string, credential zhipu.Credential) (domain.AccountSummary, error) {
	usage, err := s.zhipu.FetchUsage(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实智谱额度: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = "智谱 Coding Plan"
	}
	windows := make([]domain.QuotaSignal, 0, len(usage.Windows))
	for index, window := range usage.Windows {
		remaining := window.RemainingPercent
		signal := domain.QuotaSignal{ID: fmt.Sprintf("zhipu-live-%d", index), Provider: "智谱 AI", Label: window.Label, Kind: window.Kind, Value: window.Remaining, Unit: window.Unit, RemainingPercent: &remaining, ResetAt: window.ResetAt, Status: quotaStatus(remaining), Source: "智谱 /api/monitor/usage/quota/limit", Confidence: "live"}
		if window.Total > 0 {
			total := window.Total
			signal.Total = &total
		}
		windows = append(windows, signal)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "zhipu", alias, "", firstNonEmpty(usage.Plan, "Coding Plan"), "api_key", "智谱 Coding Plan 实时接口", windows, now)
	return s.persist(ctx, account, credential, "智谱 AI")
}

func (s *Accounts) connectTokenRhythmWithID(ctx context.Context, id, alias string, credential tokenrhythm.Credential) (domain.AccountSummary, error) {
	usage, err := s.tokenrhythm.FetchUsage(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实基元律动余额: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = "基元律动账号"
	}
	signal := domain.QuotaSignal{ID: "tokenrhythm-live-balance", Provider: "基元律动 TokenRhythm", Label: "可用余额", Kind: "balance", Value: usage.AvailableBalance, Unit: "CNY", ExpiresAt: usage.ExpiresAt, Status: "healthy", Source: "TokenRhythm /api/usage-summary", Confidence: "live"}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "tokenrhythm", alias, "", "钱包余额", "session_token", "TokenRhythm 官方站点接口", []domain.QuotaSignal{signal}, now)
	return s.persist(ctx, account, credential, "基元律动")
}

func (s *Accounts) connectWorkBuddyWithID(ctx context.Context, id, providerID, alias, authMethod string, credential workbuddy.Credential) (domain.AccountSummary, error) {
	credits, _, err := s.workbuddy.FetchCredits(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(credential.Email, credential.Nickname, "WorkBuddy 账号")
	}
	name, _, _ := providerMeta(providerID)
	signal := domain.QuotaSignal{ID: "workbuddy-live-credits", Provider: name, Label: "总积分余额", Kind: "credits", Value: credits.Remaining, Unit: "credits", ExpiresAt: credits.ExpiresAt, Status: "healthy", Source: "WorkBuddy Billing", Confidence: "live"}
	if credits.Total > 0 {
		signal.Total = &credits.Total
		percent := credits.Remaining / credits.Total * 100
		signal.RemainingPercent = &percent
		signal.Status = quotaStatus(percent)
	}
	windows := []domain.QuotaSignal{signal}
	for index, resource := range credits.Resources {
		label := firstNonEmpty(resource.Name, shortPackageCode(resource.Code), fmt.Sprintf("积分包 %d", index+1))
		window := domain.QuotaSignal{ID: fmt.Sprintf("workbuddy-package-%d", index), Provider: name, Label: label, Kind: "credits", Value: resource.Remaining, Unit: "credits", ExpiresAt: resource.ExpiresAt, Status: "healthy", Source: "WorkBuddy Billing", Confidence: "live"}
		if resource.Total > 0 {
			total := resource.Total
			percent := resource.Remaining / total * 100
			window.Total = &total
			window.RemainingPercent = &percent
			window.Status = quotaStatus(percent)
		}
		windows = append(windows, window)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, providerID, alias, credential.Email, "Credits", authMethod, "WorkBuddy 官方 Billing", windows, now)
	return s.persist(ctx, account, credential, "WorkBuddy")
}

func (s *Accounts) connectGenericWithID(ctx context.Context, id, providerID, alias string, credential genericquota.Credential) (domain.AccountSummary, error) {
	reading, err := s.generic.Fetch(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实额度: %w", err)
	}
	name, _, _ := providerMeta(providerID)
	if strings.TrimSpace(alias) == "" {
		alias = name + " 账号"
	}
	signal := domain.QuotaSignal{ID: providerID + "-live-quota", Provider: name, Label: credential.Label, Kind: credential.Kind, Value: reading.Value, Total: reading.Total, Unit: credential.Unit, ResetAt: reading.ResetAt, ExpiresAt: reading.ExpiresAt, Status: "healthy", Source: "用户配置的真实额度接口", Confidence: "live"}
	if reading.Total != nil && *reading.Total > 0 {
		percent := reading.Value / *reading.Total * 100
		signal.RemainingPercent = &percent
		signal.Status = quotaStatus(percent)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, providerID, alias, "", credential.Label, "custom_endpoint", "已验证自定义额度 API", []domain.QuotaSignal{signal}, now)
	return s.persist(ctx, account, credential, name)
}

func (s *Accounts) persist(ctx context.Context, account domain.ConnectedAccount, credential any, eventProvider string) (domain.AccountSummary, error) {
	plain, err := json.Marshal(credential)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	encrypted, err := s.vault.Encrypt(plain)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	if err := s.store.SaveConnectedAccount(ctx, account, encrypted); err != nil {
		return domain.AccountSummary{}, fmt.Errorf("保存 %s 账号失败: %w", eventProvider, err)
	}
	s.events.Publish(Event{Type: "account.updated", Message: fmt.Sprintf("%s 的 %s 额度已更新", account.Alias, eventProvider), Timestamp: account.LastRefreshedAt})
	return summaryFromConnected(account), nil
}

func newConnected(id, providerID, alias, email, plan, authMethod, source string, windows []domain.QuotaSignal, now time.Time) domain.ConnectedAccount {
	name, region, _ := providerMeta(providerID)
	status := "healthy"
	for _, window := range windows {
		if window.Status == "critical" {
			status = "critical"
			break
		}
		if window.Status == "warning" {
			status = "warning"
		}
	}
	return domain.ConnectedAccount{ID: id, ProviderID: providerID, ProviderName: name, Region: region, Alias: alias, Email: email, Plan: plan, AuthMethod: authMethod, Status: status, Source: source, LastRefreshedAt: now, NextRefreshAt: now.Add(10 * time.Minute), QuotaWindows: windows}
}

func codexWindows(usage codex.Usage) []domain.QuotaSignal {
	result := make([]domain.QuotaSignal, 0, len(usage.Windows)+1)
	for index, window := range usage.Windows {
		remaining, total := window.RemainingPercent, 100.0
		result = append(result, domain.QuotaSignal{ID: fmt.Sprintf("codex-live-%d", index), Provider: "Codex", Label: window.Label, Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, WindowSeconds: window.WindowSeconds, ResetAt: window.ResetAt, Status: quotaStatus(remaining), Source: "Codex /wham/usage", Confidence: "live"})
	}
	if usage.Credits != nil {
		result = append(result, domain.QuotaSignal{ID: "codex-live-credits", Provider: "Codex", Label: "Credits 余额", Kind: "credits", Value: *usage.Credits, Unit: "credits", Status: "healthy", Source: "Codex /wham/usage", Confidence: "live"})
	}
	return result
}

func quotaStatus(remaining float64) string {
	if remaining <= 15 {
		return "critical"
	}
	if remaining <= 35 {
		return "warning"
	}
	return "healthy"
}
func summaryFromConnected(account domain.ConnectedAccount) domain.AccountSummary {
	primary, secondary := "等待额度数据", ""
	if len(account.QuotaWindows) > 0 {
		primary = signalSummary(account.QuotaWindows[0])
	}
	if len(account.QuotaWindows) > 1 {
		secondary = signalSummary(account.QuotaWindows[1])
	}
	return domain.AccountSummary{ID: account.ID, ProviderID: account.ProviderID, Provider: account.ProviderName, Region: account.Region, Alias: account.Alias, Services: providerServices(account.ProviderID), PrimaryMetric: primary, SecondaryMetric: secondary, Status: account.Status, Source: account.Source, LastRefreshedAt: account.LastRefreshedAt, NextRefreshAt: account.NextRefreshAt, Error: account.Error, Email: account.Email, Plan: account.Plan, AuthMethod: account.AuthMethod, Synthetic: false, QuotaWindows: account.QuotaWindows}
}
func signalSummary(signal domain.QuotaSignal) string {
	if signal.Kind == "rate_window" && signal.RemainingPercent != nil {
		return fmt.Sprintf("%s %.0f%%", signal.Label, *signal.RemainingPercent)
	}
	if signal.Unit == "CNY" {
		return fmt.Sprintf("%s ¥%.2f", signal.Label, signal.Value)
	}
	if signal.Unit == "credits" {
		return fmt.Sprintf("%s %.2f credits", signal.Label, signal.Value)
	}
	return fmt.Sprintf("%s %.2f %s", signal.Label, signal.Value, signal.Unit)
}
func providerMeta(id string) (string, string, bool) {
	providers := map[string][2]string{
		"trae-cn": {"TRAE CN / TraeCode / TraeWork", "CN"}, "qoder-cn": {"Qoder CN", "CN"},
		"workbuddy-cn": {"WorkBuddy / CodeBuddy 国内版", "CN"}, "coze-cn": {"扣子 Coze", "CN"},
		"bailian": {"阿里云百炼 Token Plan", "CN"}, "mimo": {"小米 MiMo", "CN"}, "tokenrhythm": {"基元律动 TokenRhythm", "CN"},
		"deepseek": {"DeepSeek", "CN"}, "zhipu": {"智谱 AI", "CN"}, "codex": {"Codex", "Global"},
		"gemini-cli": {"Gemini", "Global"}, "claude-code": {"Claude Code", "Global"},
		"qoder-global": {"Qoder 国际版", "Global"}, "workbuddy-global": {"WorkBuddy / CodeBuddy 国际版", "Global"},
		"kiro": {"Kiro", "Global"}, "cursor": {"Cursor", "Global"},
	}
	provider, ok := providers[id]
	if !ok {
		return id, "Global", false
	}
	return provider[0], provider[1], true
}
func providerServices(id string) []string {
	name, _, _ := providerMeta(id)
	return []string{name}
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "账号"
}
func shortPackageCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "_")
	if len(parts) >= 3 {
		return "积分包 " + parts[2]
	}
	return value
}
func credentialError(err error) error { return fmt.Errorf("读取本地加密凭据失败: %w", err) }
func friendlyLoginError(err error) string {
	if err == context.DeadlineExceeded {
		return "登录已超时，请重新发起"
	}
	return err.Error()
}
func (s *Accounts) saveSession(session *DeviceLoginSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}
func (s *Accounts) updateDeviceSession(id, status, accountID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[id]; session != nil {
		session.Status = status
		session.AccountID = accountID
		session.Message = message
	}
}
