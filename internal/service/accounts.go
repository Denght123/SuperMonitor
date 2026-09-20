package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/integration/aliyunbss"
	"github.com/Denght123/SuperMonitor/internal/integration/claude"
	"github.com/Denght123/SuperMonitor/internal/integration/codex"
	"github.com/Denght123/SuperMonitor/internal/integration/coze"
	"github.com/Denght123/SuperMonitor/internal/integration/cursor"
	"github.com/Denght123/SuperMonitor/internal/integration/deepseek"
	"github.com/Denght123/SuperMonitor/internal/integration/gemini"
	"github.com/Denght123/SuperMonitor/internal/integration/genericquota"
	"github.com/Denght123/SuperMonitor/internal/integration/kiro"
	"github.com/Denght123/SuperMonitor/internal/integration/mimo"
	"github.com/Denght123/SuperMonitor/internal/integration/qoder"
	"github.com/Denght123/SuperMonitor/internal/integration/tokenrhythm"
	"github.com/Denght123/SuperMonitor/internal/integration/trae"
	"github.com/Denght123/SuperMonitor/internal/integration/workbuddy"
	"github.com/Denght123/SuperMonitor/internal/integration/zhipu"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
	"github.com/google/uuid"
)

type Accounts struct {
	store       *sqlite.Store
	vault       *secure.Vault
	aliyun      *aliyunbss.Client
	claude      *claude.Client
	codex       *codex.Client
	coze        *coze.Client
	cursor      *cursor.Client
	deepseek    *deepseek.Client
	gemini      *gemini.Client
	generic     *genericquota.Client
	kiro        *kiro.Client
	mimo        *mimo.Client
	qoder       *qoder.Client
	tokenrhythm *tokenrhythm.Client
	trae        *trae.Client
	workbuddy   *workbuddy.Client
	zhipu       *zhipu.Client
	events      *EventHub
	mu          sync.RWMutex
	accountOps  sync.RWMutex
	activityMu  sync.Mutex
	sessions    map[string]*DeviceLoginSession
	kiroPending map[string]kiroLoginMeta
}

var ErrAccountNotFound = errors.New("账号不存在")

type kiroLoginMeta struct {
	SessionID string
	Alias     string
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
	return &Accounts{store: store, vault: vault, aliyun: aliyunbss.NewClient(), claude: claude.NewClient(), codex: codex.NewClient(), coze: coze.NewClient(), cursor: cursor.NewClient(), deepseek: deepseek.NewClient(), gemini: gemini.NewClient(), generic: genericquota.NewClient(), kiro: kiro.NewClient(), mimo: mimo.NewClient(), qoder: qoder.NewClient(), tokenrhythm: tokenrhythm.NewClient(), trae: trae.NewClient(), workbuddy: workbuddy.NewClient(), zhipu: zhipu.NewClient(), events: events, sessions: make(map[string]*DeviceLoginSession), kiroPending: make(map[string]kiroLoginMeta)}
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
	if existing, existingCredential, ok := s.findCodexCredential(ctx, credential.AccountID); ok {
		if strings.TrimSpace(alias) == "" {
			alias = existing.Alias
		}
		return s.connectCodexWithID(ctx, existing.ID, alias, existing.AuthMethod, existingCredential)
	}
	return s.connectCodexWithID(ctx, "codex-"+uuid.NewString(), alias, "credential_import", credential)
}

func (s *Accounts) findCodexCredential(ctx context.Context, accountID string) (domain.ConnectedAccount, codex.Credential, bool) {
	if strings.TrimSpace(accountID) == "" {
		return domain.ConnectedAccount{}, codex.Credential{}, false
	}
	accounts, err := s.store.ConnectedAccounts(ctx)
	if err != nil {
		return domain.ConnectedAccount{}, codex.Credential{}, false
	}
	for _, account := range accounts {
		if account.ProviderID != "codex" {
			continue
		}
		stored, encrypted, err := s.store.ConnectedAccount(ctx, account.ID)
		if err != nil {
			continue
		}
		plain, err := s.vault.Decrypt(encrypted)
		if err != nil {
			continue
		}
		var credential codex.Credential
		if json.Unmarshal(plain, &credential) == nil && credential.AccountID == accountID {
			return stored, credential, true
		}
	}
	return domain.ConnectedAccount{}, codex.Credential{}, false
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

func (s *Accounts) ImportQoder(ctx context.Context, providerID, alias string, raw []byte) (domain.AccountSummary, error) {
	region := qoder.RegionGlobal
	if providerID == "qoder-cn" {
		region = qoder.RegionCN
	}
	credential, err := qoder.ParseCredential(raw, region)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectQoderWithID(ctx, providerID+"-"+uuid.NewString(), providerID, alias, "credential_import", credential)
}

func (s *Accounts) ImportCursor(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := cursor.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectCursorWithID(ctx, "cursor-"+uuid.NewString(), alias, "credential_import", credential)
}

func (s *Accounts) ImportKiro(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := kiro.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectKiroWithID(ctx, "kiro-"+uuid.NewString(), alias, "credential_import", credential)
}

func (s *Accounts) ImportTrae(ctx context.Context, alias string, raw []byte) (domain.AccountSummary, error) {
	credential, err := trae.ParseCredential(raw)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectTraeWithID(ctx, "trae-cn-"+uuid.NewString(), alias, "credential_import", credential)
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

func (s *Accounts) ConnectAliyun(ctx context.Context, alias, accessKeyID, accessKeySecret string) (domain.AccountSummary, error) {
	credential, err := aliyunbss.Normalize(accessKeyID, accessKeySecret)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectAliyunWithID(ctx, "bailian-"+uuid.NewString(), alias, credential)
}

func (s *Accounts) ConnectCoze(ctx context.Context, alias, cookie string) (domain.AccountSummary, error) {
	credential, err := coze.NormalizeCookie(cookie)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	return s.connectCozeWithID(ctx, "coze-cn-"+uuid.NewString(), alias, credential)
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

func (s *Accounts) StartQoderOAuth(providerID, alias string) (DeviceLoginSession, error) {
	region := qoder.RegionGlobal
	if providerID == "qoder-cn" {
		region = qoder.RegionCN
	}
	challenge, err := qoder.StartLogin(region)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{ID: uuid.NewString(), Provider: providerID, Status: "pending", VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待在 Qoder 官方页面完成设备授权"}
	s.saveSession(&session)
	go func(sessionID string) {
		loginCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
		defer cancel()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			credential, pending, pollErr := s.qoder.PollLogin(loginCtx, challenge)
			if pollErr != nil {
				s.updateDeviceSession(sessionID, "failed", "", friendlyLoginError(pollErr))
				return
			}
			if !pending {
				account, connectErr := s.connectQoderWithID(loginCtx, providerID+"-"+uuid.NewString(), providerID, alias, "device_code", credential)
				if connectErr != nil {
					s.updateDeviceSession(sessionID, "failed", "", connectErr.Error())
					return
				}
				s.updateDeviceSession(sessionID, "completed", account.ID, "登录成功，Qoder 真实额度已写入账号池")
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

func (s *Accounts) StartCursorOAuth(alias string) (DeviceLoginSession, error) {
	challenge, err := cursor.StartLogin()
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{ID: uuid.NewString(), Provider: "cursor", Status: "pending", VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待在 Cursor 官方页面完成登录"}
	s.saveSession(&session)
	go func(sessionID string) {
		loginCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
		defer cancel()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			credential, pending, pollErr := s.cursor.PollLogin(loginCtx, challenge)
			if pollErr != nil {
				s.updateDeviceSession(sessionID, "failed", "", friendlyLoginError(pollErr))
				return
			}
			if !pending {
				account, connectErr := s.connectCursorWithID(loginCtx, "cursor-"+uuid.NewString(), alias, "oauth", credential)
				if connectErr != nil {
					s.updateDeviceSession(sessionID, "failed", "", connectErr.Error())
					return
				}
				s.updateDeviceSession(sessionID, "completed", account.ID, "登录成功，Cursor 订阅额度已写入账号池")
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

func (s *Accounts) StartTraeOAuth(ctx context.Context, alias string) (DeviceLoginSession, error) {
	challenge, err := s.trae.StartLogin(ctx)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{ID: uuid.NewString(), Provider: "trae-cn", Status: "pending", VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待在 TRAE CN 官方页面完成登录"}
	s.saveSession(&session)
	go func(sessionID string) {
		loginCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
		defer cancel()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			credential, pending, pollErr := s.trae.PollLogin(loginCtx, challenge)
			if pollErr != nil {
				s.updateDeviceSession(sessionID, "failed", "", friendlyLoginError(pollErr))
				return
			}
			if !pending {
				account, connectErr := s.connectTraeWithID(loginCtx, "trae-cn-"+uuid.NewString(), alias, "oauth", credential)
				if connectErr != nil {
					s.updateDeviceSession(sessionID, "failed", "", connectErr.Error())
					return
				}
				s.updateDeviceSession(sessionID, "completed", account.ID, "登录成功，TRAE 真实积分包已写入账号池")
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

func (s *Accounts) StartKiroOAuth(alias, callbackURL string) (DeviceLoginSession, error) {
	challenge, err := s.kiro.StartLogin(callbackURL)
	if err != nil {
		return DeviceLoginSession{}, err
	}
	session := DeviceLoginSession{ID: uuid.NewString(), Provider: "kiro", Status: "pending", VerifyURL: challenge.VerifyURL, ExpiresAt: challenge.ExpiresAt, Message: "等待在 Kiro 官方页面使用 Google 或 GitHub 完成登录"}
	s.mu.Lock()
	s.sessions[session.ID] = &session
	s.kiroPending[challenge.State] = kiroLoginMeta{SessionID: session.ID, Alias: alias}
	s.mu.Unlock()
	return session, nil
}

func (s *Accounts) CompleteKiroOAuth(ctx context.Context, values url.Values) (string, error) {
	state := values.Get("state")
	s.mu.Lock()
	meta, ok := s.kiroPending[state]
	if ok {
		delete(s.kiroPending, state)
	}
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("Kiro 登录会话不存在或已过期")
	}
	_, credential, err := s.kiro.CompleteLogin(ctx, values)
	if err != nil {
		s.updateDeviceSession(meta.SessionID, "failed", "", err.Error())
		return meta.SessionID, err
	}
	account, err := s.connectKiroWithID(ctx, "kiro-"+uuid.NewString(), meta.Alias, "oauth", credential)
	if err != nil {
		s.updateDeviceSession(meta.SessionID, "failed", "", err.Error())
		return meta.SessionID, err
	}
	s.updateDeviceSession(meta.SessionID, "completed", account.ID, "登录成功，Kiro 真实额度已写入账号池")
	return meta.SessionID, nil
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
	s.accountOps.RLock()
	defer s.accountOps.RUnlock()
	account, encrypted, err := s.store.ConnectedAccount(ctx, id)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	plain, err := s.vault.Decrypt(encrypted)
	if err != nil {
		return domain.AccountSummary{}, err
	}
	switch account.ProviderID {
	case "bailian":
		var credential aliyunbss.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectAliyunWithID(ctx, account.ID, account.Alias, credential)
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
	case "coze-cn":
		var credential coze.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectCozeWithID(ctx, account.ID, account.Alias, credential)
	case "cursor":
		var credential cursor.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectCursorWithID(ctx, account.ID, account.Alias, account.AuthMethod, credential)
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
	case "kiro":
		var credential kiro.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectKiroWithID(ctx, account.ID, account.Alias, account.AuthMethod, credential)
	case "mimo":
		var credential mimo.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectMimoWithID(ctx, account.ID, account.Alias, credential)
	case "qoder-cn", "qoder-global":
		var credential qoder.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectQoderWithID(ctx, account.ID, account.ProviderID, account.Alias, account.AuthMethod, credential)
	case "tokenrhythm":
		var credential tokenrhythm.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectTokenRhythmWithID(ctx, account.ID, account.Alias, credential)
	case "trae-cn":
		var credential trae.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return domain.AccountSummary{}, credentialError(err)
		}
		return s.connectTraeWithID(ctx, account.ID, account.Alias, account.AuthMethod, credential)
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
	default:
		return domain.AccountSummary{}, fmt.Errorf("平台 %s 尚未实现真实刷新", account.ProviderID)
	}
}

// Delete removes exactly one connected account, including its encrypted
// credential and cached quota windows. Synthetic dashboard fixtures are not
// addressable through this operation.
func (s *Accounts) Delete(ctx context.Context, id string) error {
	s.accountOps.Lock()
	defer s.accountOps.Unlock()
	account, err := s.store.DeleteConnectedAccount(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrAccountNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("删除账号失败: %w", err)
	}
	s.events.Publish(Event{Type: "account.deleted", Message: fmt.Sprintf("已删除账号 %s", account.Alias), Timestamp: time.Now().UTC().Truncate(time.Second)})
	return nil
}

func (s *Accounts) ListActivities(ctx context.Context) ([]domain.Activity, error) {
	s.accountOps.RLock()
	defer s.accountOps.RUnlock()
	accounts, err := s.store.ConnectedAccounts(ctx)
	if err != nil {
		return nil, err
	}
	activities := make([]domain.Activity, 0)
	var activityErrors []error
	for _, account := range accounts {
		if account.ProviderID != "workbuddy-cn" {
			continue
		}
		activity := domain.Activity{ID: "daily-checkin", AccountID: account.ID, AccountAlias: account.Alias, ProviderID: account.ProviderID, Provider: account.ProviderName, Title: "每日签到", Status: "error", Action: "checkin"}
		stored, encrypted, err := s.store.ConnectedAccount(ctx, account.ID)
		if err != nil {
			activity.Description = "账号信息已变化，请刷新活动状态"
			activities = append(activities, activity)
			activityErrors = append(activityErrors, fmt.Errorf("读取账号 %s 的活动凭据失败: %w", account.ID, err))
			continue
		}
		plain, err := s.vault.Decrypt(encrypted)
		if err != nil {
			activity.Description = "本机认证信息无法解密，请重新登录该账号"
			activities = append(activities, activity)
			activityErrors = append(activityErrors, fmt.Errorf("解密账号 %s 的活动凭据失败: %w", account.ID, err))
			continue
		}
		var credential workbuddy.Credential
		if err := json.Unmarshal(plain, &credential); err != nil {
			activity.Description = "认证文件格式已失效，请重新登录该账号"
			activities = append(activities, activity)
			activityErrors = append(activityErrors, fmt.Errorf("解析账号 %s 的活动凭据失败: %w", account.ID, err))
			continue
		}
		status, refreshed, err := s.workbuddy.FetchCheckinStatus(ctx, &credential)
		if err != nil {
			activity.Description = "无法读取官方签到状态，请检查登录状态后重试"
			activities = append(activities, activity)
			activityErrors = append(activityErrors, fmt.Errorf("读取账号 %s 的真实活动状态失败: %w", account.ID, err))
			continue
		}
		if refreshed {
			if _, err := s.persist(ctx, stored, credential, "WorkBuddy"); err != nil {
				activity.Description = "登录已刷新，但无法保存新的认证状态，请稍后重试"
				activities = append(activities, activity)
				activityErrors = append(activityErrors, fmt.Errorf("保存账号 %s 的刷新凭据失败: %w", account.ID, err))
				continue
			}
		}
		state, description := "available", "今日尚未签到，可领取平台活动积分"
		if status.TodayCheckedIn {
			state, description = "completed", "今日已签到"
		}
		activity.Description = description
		activity.Status = state
		activities = append(activities, activity)
	}
	return activities, errors.Join(activityErrors...)
}

func (s *Accounts) RunActivity(ctx context.Context, accountID, activityID string) (domain.Activity, error) {
	s.accountOps.RLock()
	defer s.accountOps.RUnlock()
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	if activityID != "daily-checkin" {
		return domain.Activity{}, fmt.Errorf("不支持的活动")
	}
	account, encrypted, err := s.store.ConnectedAccount(ctx, accountID)
	if err != nil {
		return domain.Activity{}, err
	}
	if account.ProviderID != "workbuddy-cn" {
		return domain.Activity{}, fmt.Errorf("该账号没有可执行的签到活动")
	}
	plain, err := s.vault.Decrypt(encrypted)
	if err != nil {
		return domain.Activity{}, credentialError(err)
	}
	var credential workbuddy.Credential
	if err := json.Unmarshal(plain, &credential); err != nil {
		return domain.Activity{}, credentialError(err)
	}
	already, _, err := s.workbuddy.Checkin(ctx, &credential)
	if err != nil {
		return domain.Activity{}, err
	}
	if _, err := s.connectWorkBuddyWithID(ctx, account.ID, account.ProviderID, account.Alias, account.AuthMethod, credential); err != nil {
		return domain.Activity{}, fmt.Errorf("签到成功，但刷新积分失败: %w", err)
	}
	description := "签到成功，积分余额已刷新"
	if already {
		description = "今日已签到，积分余额已刷新"
	}
	return domain.Activity{ID: activityID, AccountID: account.ID, AccountAlias: account.Alias, ProviderID: account.ProviderID, Provider: account.ProviderName, Title: "每日签到", Description: description, Status: "completed", Action: "checkin"}, nil
}

func (s *Accounts) connectAliyunWithID(ctx context.Context, id, alias string, credential aliyunbss.Credential) (domain.AccountSummary, error) {
	balance, err := s.aliyun.FetchBalance(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实阿里云账户余额: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = "阿里云费用账户"
	}
	windows := []domain.QuotaSignal{{ID: "aliyun-live-balance", Provider: "阿里云百炼 / 费用中心", Label: "账户可用额度", Kind: "balance", Value: balance.Available, Unit: balance.Currency, Status: "healthy", Source: "阿里云 BSS QueryAccountBalance", Confidence: "live"}}
	if balance.Cash != nil {
		windows = append(windows, domain.QuotaSignal{ID: "aliyun-live-cash", Provider: "阿里云百炼 / 费用中心", Label: "现金余额", Kind: "balance", Value: *balance.Cash, Unit: balance.Currency, Status: "healthy", Source: "阿里云 BSS QueryAccountBalance", Confidence: "live"})
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "bailian", alias, "", "阿里云账户余额", "access_key", "阿里云官方 BSS OpenAPI", windows, now)
	return s.persist(ctx, account, credential, "阿里云")
}

func (s *Accounts) connectQoderWithID(ctx context.Context, id, providerID, alias, authMethod string, credential qoder.Credential) (domain.AccountSummary, error) {
	quota, _, err := s.qoder.FetchQuota(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 Qoder 额度: %w", err)
	}
	name, _, _ := providerMeta(providerID)
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(credential.Nickname, name+" 账号")
	}
	windows := make([]domain.QuotaSignal, 0, len(quota.Windows))
	for index, window := range quota.Windows {
		unit := strings.TrimSpace(window.Unit)
		if unit == "" {
			unit = "credits"
		}
		signal := domain.QuotaSignal{ID: fmt.Sprintf("qoder-live-%d", index), Provider: name, Label: window.Label, Kind: "credits", Value: window.Remaining, Unit: unit, ExpiresAt: window.ExpiresAt, Status: "healthy", Source: "Qoder /api/v2/quota/usage", Confidence: "live"}
		if window.Total > 0 {
			total := window.Total
			percent := window.Remaining / total * 100
			signal.Total = &total
			signal.RemainingPercent = &percent
			signal.Status = quotaStatus(percent)
		}
		windows = append(windows, signal)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, providerID, alias, "", firstNonEmpty(quota.Plan, "Credits"), authMethod, "Qoder 官方 OpenAPI", windows, now)
	return s.persist(ctx, account, credential, name)
}

func (s *Accounts) connectCursorWithID(ctx context.Context, id, alias, authMethod string, credential cursor.Credential) (domain.AccountSummary, error) {
	usage, _, err := s.cursor.FetchUsage(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 Cursor 额度: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(credential.Email, "Cursor 账号")
	}
	remaining, total := usage.Remaining, 100.0
	windows := []domain.QuotaSignal{{ID: "cursor-live-plan", Provider: "Cursor", Label: "订阅周期额度", Kind: "rate_window", Value: remaining, Total: &total, Unit: "%", RemainingPercent: &remaining, ResetAt: usage.CycleEnd, Status: quotaStatus(remaining), Source: "Cursor /api/usage-summary", Confidence: "live"}}
	if usage.OnDemand != nil {
		signal := domain.QuotaSignal{ID: "cursor-live-ondemand", Provider: "Cursor", Label: "按量额度", Kind: "balance", Value: *usage.OnDemand, Unit: "USD", ResetAt: usage.CycleEnd, Status: "healthy", Source: "Cursor /api/usage-summary", Confidence: "live"}
		if usage.OnDemandMax != nil && *usage.OnDemandMax > 0 {
			signal.Total = usage.OnDemandMax
			percent := *usage.OnDemand / *usage.OnDemandMax * 100
			signal.RemainingPercent = &percent
			signal.Status = quotaStatus(percent)
		}
		windows = append(windows, signal)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "cursor", alias, credential.Email, firstNonEmpty(usage.Plan, "Subscription"), authMethod, "Cursor 官方用量接口", windows, now)
	return s.persist(ctx, account, credential, "Cursor")
}

func (s *Accounts) connectCozeWithID(ctx context.Context, id, alias string, credential coze.Credential) (domain.AccountSummary, error) {
	balance, err := s.coze.FetchBalance(ctx, credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实扣子积分: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(balance.Nickname, "扣子账号")
	}
	signal := domain.QuotaSignal{ID: "coze-live-credits", Provider: "扣子 Coze", Label: "积分余额", Kind: "credits", Value: balance.Remaining, Unit: "credits", ExpiresAt: balance.ExpiresAt, Status: "healthy", Source: "Coze /credit/balance", Confidence: "live"}
	if balance.Total != nil && *balance.Total > 0 {
		signal.Total = balance.Total
		percent := balance.Remaining / *balance.Total * 100
		signal.RemainingPercent = &percent
		signal.Status = quotaStatus(percent)
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "coze-cn", alias, "", "Credits", "cookie", "扣子官方站点积分接口", []domain.QuotaSignal{signal}, now)
	return s.persist(ctx, account, credential, "扣子 Coze")
}

func (s *Accounts) connectKiroWithID(ctx context.Context, id, alias, authMethod string, credential kiro.Credential) (domain.AccountSummary, error) {
	usage, _, err := s.kiro.FetchUsage(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 Kiro 额度: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(credential.Email, "Kiro 账号")
	}
	windows := make([]domain.QuotaSignal, 0, 2)
	if usage.Total > 0 {
		remaining := usage.Total - usage.Used
		if remaining < 0 {
			remaining = 0
		}
		percent := remaining / usage.Total * 100
		total := usage.Total
		windows = append(windows, domain.QuotaSignal{ID: "kiro-live-plan", Provider: "Kiro", Label: "Agentic Requests", Kind: "credits", Value: remaining, Total: &total, Unit: "credits", RemainingPercent: &percent, ResetAt: usage.ResetAt, Status: quotaStatus(percent), Source: "Kiro getUsageLimits", Confidence: "live"})
	}
	if usage.BonusTotal > 0 {
		remaining := usage.BonusTotal - usage.BonusUsed
		if remaining < 0 {
			remaining = 0
		}
		percent := remaining / usage.BonusTotal * 100
		total := usage.BonusTotal
		windows = append(windows, domain.QuotaSignal{ID: "kiro-live-bonus", Provider: "Kiro", Label: "Bonus Credits", Kind: "credits", Value: remaining, Total: &total, Unit: "credits", RemainingPercent: &percent, Status: quotaStatus(percent), Source: "Kiro getUsageLimits", Confidence: "live"})
	}
	if len(windows) == 0 {
		return domain.AccountSummary{}, fmt.Errorf("Kiro 官方接口未返回可显示额度")
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "kiro", alias, credential.Email, firstNonEmpty(usage.Plan, usage.Tier, "Subscription"), authMethod, "Kiro 官方 Runtime API", windows, now)
	return s.persist(ctx, account, credential, "Kiro")
}

func (s *Accounts) connectTraeWithID(ctx context.Context, id, alias, authMethod string, credential trae.Credential) (domain.AccountSummary, error) {
	usage, _, err := s.trae.FetchUsage(ctx, &credential)
	if err != nil {
		return domain.AccountSummary{}, fmt.Errorf("无法读取真实 TRAE 额度: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		alias = firstNonEmpty(credential.Nickname, "TRAE CN 账号")
	}
	windows := make([]domain.QuotaSignal, 0, len(usage.Credits))
	for index, item := range usage.Credits {
		total := item.Total
		percent := 100.0
		if total > 0 {
			percent = item.Remaining / total * 100
		}
		windows = append(windows, domain.QuotaSignal{ID: fmt.Sprintf("trae-live-%d", index), Provider: "TRAE CN / TraeCode / TraeWork", Label: item.Label, Kind: "credits", Value: item.Remaining, Total: &total, Unit: "credits", RemainingPercent: &percent, ExpiresAt: item.ExpiresAt, Status: quotaStatus(percent), Source: "TRAE ide_user_ent_usage", Confidence: "live"})
	}
	now := time.Now().UTC().Truncate(time.Second)
	account := newConnected(id, "trae-cn", alias, "", "Credits", authMethod, "TRAE 官方权益接口", windows, now)
	return s.persist(ctx, account, credential, "TRAE")
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
	return refreshAccountIDs(ctx, ids, defaultAccountRefreshConcurrency, defaultPerAccountRefreshTimeout, func(accountCtx context.Context, id string) error {
		_, refreshErr := s.RefreshOne(accountCtx, id)
		return refreshErr
	})
}

func (s *Accounts) connectCodexWithID(ctx context.Context, id, alias, authMethod string, credential codex.Credential) (domain.AccountSummary, error) {
	usage, refreshed, err := s.codex.FetchUsage(ctx, &credential)
	if err != nil {
		if refreshed {
			if existing, _, loadErr := s.store.ConnectedAccount(ctx, id); loadErr == nil {
				_, _ = s.persist(ctx, existing, credential, "Codex")
			}
		}
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
		alias = "智谱开放平台"
	}
	windows := make([]domain.QuotaSignal, 0, len(usage.Windows))
	for index, window := range usage.Windows {
		status := "healthy"
		if window.Kind == "balance" {
			status = balanceStatus(window.Remaining)
		}
		signal := domain.QuotaSignal{ID: fmt.Sprintf("zhipu-live-%d", index), Provider: "智谱 AI", Label: window.Label, Kind: window.Kind, Value: window.Remaining, Unit: window.Unit, ResetAt: window.ResetAt, Status: status, Source: usage.Source, Confidence: "live"}
		if window.HasRemainingPercent {
			remaining := window.RemainingPercent
			signal.RemainingPercent = &remaining
			signal.Status = quotaStatus(remaining)
		}
		if window.Total > 0 {
			total := window.Total
			signal.Total = &total
		}
		windows = append(windows, signal)
	}
	now := time.Now().UTC().Truncate(time.Second)
	source := firstNonEmpty(usage.Source, "智谱官方开放平台")
	account := newConnected(id, "zhipu", alias, "", firstNonEmpty(usage.Plan, "开放平台 API Key"), "api_key", source, windows, now)
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
	summary, err := s.persist(ctx, account, credential, "基元律动")
	if err != nil {
		return domain.AccountSummary{}, err
	}
	s.syncTokenRhythmUsage(ctx, id, credential, now)
	return summary, nil
}

func (s *Accounts) syncTokenRhythmUsage(ctx context.Context, accountID string, credential tokenrhythm.Credential, now time.Time) {
	syncErrors := syncTokenRhythmUsageSnapshots(ctx, s.tokenrhythm, s.store, accountID, credential)
	if len(syncErrors) > 0 {
		s.events.Publish(Event{Type: "usage.sync_failed", Message: "基元律动余额已更新，但部分 Token 用量同步失败", Timestamp: now})
	}
}

func tokenRhythmUsageReadings(panel tokenrhythm.UsagePanel) []domain.UsageModelReading {
	total := domain.UsageCounters{
		InputTokens: panel.InputTokens, OutputTokens: panel.OutputTokens,
		CacheTokens: panel.CacheReadTokens + panel.CacheWriteTokens, Requests: panel.Calls,
	}
	readings := make([]domain.UsageModelReading, 0, len(panel.Models)+1)
	attributed := domain.UsageCounters{}
	for _, model := range panel.Models {
		counters := domain.UsageCounters{
			InputTokens: model.InputTokens, OutputTokens: model.OutputTokens,
			CacheTokens: model.CacheReadTokens + model.CacheWriteTokens, Requests: model.Calls,
		}
		readings = append(readings, domain.UsageModelReading{Model: model.Model, Counters: counters})
		attributed.InputTokens += counters.InputTokens
		attributed.OutputTokens += counters.OutputTokens
		attributed.CacheTokens += counters.CacheTokens
		attributed.Requests += counters.Requests
	}
	remaining := domain.UsageCounters{
		InputTokens:  positiveDifference(total.InputTokens, attributed.InputTokens),
		OutputTokens: positiveDifference(total.OutputTokens, attributed.OutputTokens),
		CacheTokens:  positiveDifference(total.CacheTokens, attributed.CacheTokens),
		Requests:     positiveDifference(total.Requests, attributed.Requests),
	}
	if remaining != (domain.UsageCounters{}) {
		readings = append(readings, domain.UsageModelReading{Counters: remaining})
	}
	return readings
}

func positiveDifference(total, attributed int64) int64 {
	if total <= attributed {
		return 0
	}
	return total - attributed
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
	return domain.ConnectedAccount{ID: id, ProviderID: providerID, ProviderName: name, Region: region, Alias: alias, Email: email, Plan: plan, AuthMethod: authMethod, Status: status, Source: source, LastRefreshedAt: now, NextRefreshAt: now.Add(DefaultAccountSyncInterval), QuotaWindows: windows}
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

func balanceStatus(remaining float64) string {
	if remaining <= 0 {
		return "critical"
	}
	return "healthy"
}
func summaryFromConnected(account domain.ConnectedAccount) domain.AccountSummary {
	primary, secondary := "等待额度数据", ""
	if len(account.QuotaWindows) > 0 {
		primary = signalSummary(account.QuotaWindows[0])
	} else if strings.TrimSpace(account.Plan) != "" {
		primary = account.Plan
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
		"bailian": {"阿里云百炼 / 阿里云余额", "CN"}, "mimo": {"小米 MiMo", "CN"}, "tokenrhythm": {"基元律动 TokenRhythm", "CN"},
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
