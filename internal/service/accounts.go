package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/integration/codex"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
	"github.com/google/uuid"
)

type Accounts struct {
	store    *sqlite.Store
	vault    *secure.Vault
	codex    *codex.Client
	events   *EventHub
	mu       sync.RWMutex
	sessions map[string]*DeviceLoginSession
}

type DeviceLoginSession struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	UserCode  string    `json:"userCode"`
	VerifyURL string    `json:"verifyUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
	AccountID string    `json:"accountId,omitempty"`
	Message   string    `json:"message"`
}

func NewAccounts(store *sqlite.Store, vault *secure.Vault, events *EventHub) *Accounts {
	return &Accounts{store: store, vault: vault, codex: codex.NewClient(), events: events, sessions: make(map[string]*DeviceLoginSession)}
}

func (s *Accounts) ImportCodex(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := codex.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectCodex(ctx, alias, "credential_import", credential)
}

func (s *Accounts) StartCodexDeviceLogin(ctx context.Context, alias string) (DeviceLoginSession, error) {
	challenge, err := s.codex.StartDeviceLogin(ctx)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{
		ID: uuid.NewString(), Provider: "codex", Status: "pending", UserCode: challenge.UserCode,
		VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待你在 OpenAI 页面确认登录",
	}
	s.mu.Lock()
	s.sessions[session.ID] = &session
	s.mu.Unlock()

	go func(sessionID string) {
		loginCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
		defer cancel()
		credential, loginErr := s.codex.CompleteDeviceLogin(loginCtx, challenge)
		if loginErr != nil {
			s.updateDeviceSession(sessionID, "failed", "", friendlyCodexError(loginErr))
			return
		}
		account, connectErr := s.connectCodex(loginCtx, alias, "device_code", credential)
		if connectErr != nil {
			s.updateDeviceSession(sessionID, "failed", "", friendlyCodexError(connectErr))
			return
		}
		s.updateDeviceSession(sessionID, "completed", account.ID, "登录成功，实时额度已写入账号池")
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
	var credential codex.Credential
	if err := json.Unmarshal(plain, &credential); err != nil {
		return domain.AccountSummary{}, fmt.Errorf("读取本地加密凭据失败: %w", err)
	}
	return s.connectCodexWithID(ctx, account.ID, account.Alias, account.AuthMethod, credential)
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

func (s *Accounts) connectCodex(ctx context.Context, alias, authMethod string, credential codex.Credential) (domain.AccountSummary, error) {
	id := "codex-" + uuid.NewString()
	return s.connectCodexWithID(ctx, id, alias, authMethod, credential)
}

func (s *Accounts) connectCodexWithID(ctx context.Context, id, alias, authMethod string, credential codex.Credential) (domain.AccountSummary, error) {
	usage, _, err := s.codex.FetchUsage(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, friendlyError(err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = credential.Email
		if alias == "" {
			alias = "Codex 账号"
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	windows := quotaWindows(usage, now)
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
	account := domain.ConnectedAccount{
		ID: id, ProviderID: "codex", Alias: alias, Email: credential.Email, Plan: usage.Plan,
		AuthMethod: authMethod, Status: status, Source: "Codex 实时接口", LastRefreshedAt: now,
		NextRefreshAt: now.Add(10 * time.Minute), QuotaWindows: windows,
	}
	plain, err := json.Marshal(credential)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	encrypted, err := s.vault.Encrypt(plain)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	if err := s.store.SaveConnectedAccount(ctx, account, encrypted); err != nil {
		return domain.AccountSummary{}, fmt.Errorf("保存 Codex 账号失败: %w", err)
	}
	s.events.Publish(Event{Type: "account.updated", Message: fmt.Sprintf("%s 的 Codex 额度已更新", alias), Timestamp: now})
	return summaryFromConnected(account), nil
}

func quotaWindows(usage codex.Usage, now time.Time) []domain.QuotaSignal {
	result := make([]domain.QuotaSignal, 0, len(usage.Windows)+1)
	for index, window := range usage.Windows {
		remaining := window.RemainingPercent
		total := 100.0
		result = append(result, domain.QuotaSignal{
			ID: fmt.Sprintf("codex-live-%d", index), Provider: "Codex", Label: window.Label,
			Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining,
			WindowSeconds: window.WindowSeconds, ResetAt: window.ResetAt, Status: quotaStatus(remaining),
			Source: "Codex /wham/usage", Confidence: "live",
		})
	}
	if usage.Credits != nil {
		value := *usage.Credits
		result = append(result, domain.QuotaSignal{ID: "codex-live-credits", Provider: "Codex", Label: "Credits 余额", Kind: "credits", Value: value, Unit: "credits", Status: "healthy", Source: "Codex /wham/usage", Confidence: "live"})
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
		primary = fmt.Sprintf("%s %.0f%%", account.QuotaWindows[0].Label, percent(account.QuotaWindows[0]))
	}
	if len(account.QuotaWindows) > 1 {
		secondary = fmt.Sprintf("%s %.0f%%", account.QuotaWindows[1].Label, percent(account.QuotaWindows[1]))
	}
	return domain.AccountSummary{ID: account.ID, Provider: "Codex", Region: "Global", Alias: account.Alias,
		Services: []string{"Codex CLI"}, PrimaryMetric: primary, SecondaryMetric: secondary, Status: account.Status,
		Source: account.Source, LastRefreshedAt: account.LastRefreshedAt, NextRefreshAt: account.NextRefreshAt,
		Email: account.Email, Plan: account.Plan, AuthMethod: account.AuthMethod, Synthetic: false, QuotaWindows: account.QuotaWindows}
}

func percent(signal domain.QuotaSignal) float64 {
	if signal.RemainingPercent == nil {
		return 0
	}
	return *signal.RemainingPercent
}
func friendlyError(err error) error {
	return fmt.Errorf("无法读取真实 Codex 额度: %s", err.Error())
}
func friendlyCodexError(err error) string {
	if err == context.DeadlineExceeded {
		return "登录已超时，请重新发起"
	}
	return err.Error()
}

func (s *Accounts) updateDeviceSession(id, status, accountID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[id]; session != nil {
		session.Status, session.AccountID, session.Message = status, accountID, message
	}
}
