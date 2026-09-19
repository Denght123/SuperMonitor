package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
	"github.com/Denght123/SuperMonitor/internal/netutil"
	"github.com/Denght123/SuperMonitor/internal/secure"
	"github.com/Denght123/SuperMonitor/internal/store/sqlite"
	"github.com/google/uuid"
)

const (
	NotificationKindFeishu = "feishu"
	NotificationKindQQMail = "qq_mail"

	defaultSchedulerInterval = 10 * time.Minute
	initialSchedulerDelay    = 15 * time.Second
)

var (
	ErrNotificationChannelNotFound = errors.New("通知渠道不存在")
	ErrNotificationEvaluationBusy  = errors.New("通知评估正在执行")
)

type NotificationChannelInput struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	WebhookURL string `json:"webhookUrl"`
	Sender     string `json:"sender"`
	AuthCode   string `json:"authCode"`
	Recipient  string `json:"recipient"`
}

type NotificationEvaluation struct {
	CheckedAccounts     int `json:"checkedAccounts"`
	TriggeredAlerts     int `json:"triggeredAlerts"`
	DeliveredMessages   int `json:"deliveredMessages"`
	FailedMessages      int `json:"failedMessages"`
	AvailableActivities int `json:"availableActivities"`
	CompletedActivities int `json:"completedActivities"`
}

type feishuCredential struct {
	WebhookURL string `json:"webhookUrl"`
}

type qqMailCredential struct {
	Sender    string `json:"sender"`
	AuthCode  string `json:"authCode"`
	Recipient string `json:"recipient"`
}

type notificationEvent struct {
	DedupeKey string
	AccountID string
	EventType string
	SkipAlert bool
	Severity  string
	Title     string
	Message   string
	Provider  string
	Recovery  string
}

type Notifications struct {
	store    *sqlite.Store
	vault    *secure.Vault
	accounts *Accounts
	events   *EventHub
	logger   *slog.Logger
	http     *http.Client
	now      func() time.Time
	sendMail func(context.Context, qqMailCredential, string, string) error
	mu       sync.Mutex
}

func NewNotifications(store *sqlite.Store, vault *secure.Vault, accounts *Accounts, events *EventHub, logger *slog.Logger) *Notifications {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Notifications{
		store: store, vault: vault, accounts: accounts, events: events, logger: logger,
		http: netutil.NewHTTPClient(20 * time.Second), now: time.Now,
	}
	service.http.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	service.sendMail = service.sendQQMail
	return service
}

func (s *Notifications) Policy() domain.NotificationPolicy {
	return domain.NotificationPolicy{
		LowQuotaPercent:  15,
		ResetReminders:   []int{3, 1},
		SchedulerMinutes: int(defaultSchedulerInterval / time.Minute),
		AutoActivities:   true,
	}
}

func (s *Notifications) Channels(ctx context.Context) ([]domain.NotificationChannel, error) {
	channels, err := s.store.NotificationChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取通知渠道失败: %w", err)
	}
	return channels, nil
}

func (s *Notifications) CreateChannel(ctx context.Context, input NotificationChannelInput) (domain.NotificationChannel, error) {
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	name := strings.TrimSpace(input.Name)
	var credential any
	var target string
	switch kind {
	case NotificationKindFeishu:
		webhook, err := normalizeFeishuWebhook(input.WebhookURL)
		if err != nil {
			return domain.NotificationChannel{}, err
		}
		if name == "" {
			name = "飞书机器人"
		}
		credential = feishuCredential{WebhookURL: webhook}
		target = "飞书自定义机器人 · Webhook 已加密"
	case NotificationKindQQMail:
		sender, err := normalizeMailbox(input.Sender)
		if err != nil || !strings.HasSuffix(strings.ToLower(sender), "@qq.com") {
			return domain.NotificationChannel{}, fmt.Errorf("发件人必须是有效的 QQ 邮箱地址")
		}
		recipient, err := normalizeMailbox(input.Recipient)
		if err != nil {
			return domain.NotificationChannel{}, fmt.Errorf("收件邮箱地址无效")
		}
		authCode := strings.TrimSpace(input.AuthCode)
		if authCode == "" {
			return domain.NotificationChannel{}, fmt.Errorf("请输入 QQ 邮箱 SMTP 授权码")
		}
		if len(authCode) > 128 {
			return domain.NotificationChannel{}, fmt.Errorf("SMTP 授权码长度无效")
		}
		if name == "" {
			name = "QQ 邮箱"
		}
		credential = qqMailCredential{Sender: sender, AuthCode: authCode, Recipient: recipient}
		target = maskMailbox(sender) + " → " + maskMailbox(recipient)
	default:
		return domain.NotificationChannel{}, fmt.Errorf("不支持的通知渠道")
	}
	if len([]rune(name)) > 80 {
		return domain.NotificationChannel{}, fmt.Errorf("渠道名称不能超过 80 个字符")
	}
	payload, err := json.Marshal(credential)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("保存通知渠道失败: %w", err)
	}
	encrypted, err := s.vault.Encrypt(payload)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("加密通知凭据失败: %w", err)
	}
	channel := domain.NotificationChannel{
		ID: uuid.NewString(), Kind: kind, Name: name, Target: target, Enabled: true,
	}
	if err := s.store.SaveNotificationChannel(ctx, channel, encrypted); err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("保存通知渠道失败: %w", err)
	}
	stored, _, err := s.store.NotificationChannel(ctx, channel.ID)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("读取通知渠道失败: %w", err)
	}
	return stored, nil
}

func (s *Notifications) DeleteChannel(ctx context.Context, id string) error {
	if err := s.store.DeleteNotificationChannel(ctx, strings.TrimSpace(id)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotificationChannelNotFound
		}
		return fmt.Errorf("删除通知渠道失败: %w", err)
	}
	return nil
}

func (s *Notifications) TestChannel(ctx context.Context, id string) error {
	channel, encrypted, err := s.store.NotificationChannel(ctx, strings.TrimSpace(id))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotificationChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("读取通知渠道失败: %w", err)
	}
	message := "SuperMonitor 测试通知\n通知渠道已连接成功。低额度、重置时间与真实活动执行结果将通过此渠道发送。"
	if err := s.sendChannel(ctx, channel, encrypted, "SuperMonitor 通知测试", message); err != nil {
		return fmt.Errorf("测试通知发送失败: %w", err)
	}
	return nil
}

func (s *Notifications) Evaluate(ctx context.Context) (NotificationEvaluation, error) {
	if !s.mu.TryLock() {
		return NotificationEvaluation{}, ErrNotificationEvaluationBusy
	}
	defer s.mu.Unlock()

	result := NotificationEvaluation{}
	accounts, err := s.store.ConnectedAccounts(ctx)
	if err != nil {
		return result, fmt.Errorf("读取账号额度失败: %w", err)
	}
	channels, err := s.store.NotificationChannels(ctx)
	if err != nil {
		return result, fmt.Errorf("读取通知渠道失败: %w", err)
	}
	enabled := make([]domain.NotificationChannel, 0, len(channels))
	for _, channel := range channels {
		if channel.Enabled {
			enabled = append(enabled, channel)
		}
	}
	now := s.now().UTC()
	result.CheckedAccounts = len(accounts)
	var evaluationErrors []error

	for _, account := range accounts {
		observedStates := make(map[string]struct{}, len(account.QuotaWindows)*2)
		for _, signal := range account.QuotaWindows {
			identity := notificationSignalKey(signal)
			observedStates[strings.Join([]string{"low_quota_state", account.ID, identity}, ":")] = struct{}{}
			observedStates[strings.Join([]string{"quota_reset_state", account.ID, identity}, ":")] = struct{}{}
			if err := s.evaluateLowQuota(ctx, enabled, account, signal, identity, now, &result); err != nil {
				evaluationErrors = append(evaluationErrors, err)
			}
			if err := s.evaluateResetReminder(ctx, enabled, account, signal, identity, now, &result); err != nil {
				evaluationErrors = append(evaluationErrors, err)
			}
		}
		if err := s.reconcileSignalStates(ctx, account.ID, observedStates); err != nil {
			evaluationErrors = append(evaluationErrors, err)
		}
	}

	if s.accounts != nil && s.Policy().AutoActivities {
		activities, activityErr := s.accounts.ListActivities(ctx)
		if activityErr != nil {
			evaluationErrors = append(evaluationErrors, fmt.Errorf("读取真实活动失败: %w", activityErr))
		}
		for _, activity := range activities {
			if activity.Status != "available" {
				continue
			}
			result.AvailableActivities++
			attemptKey := strings.Join([]string{"activity_attempt", activity.AccountID, activity.ID, activityDate(now)}, ":")
			attemptState, found, stateErr := s.store.NotificationState(ctx, attemptKey)
			if stateErr != nil {
				evaluationErrors = append(evaluationErrors, fmt.Errorf("读取活动执行状态失败: %w", stateErr))
				continue
			}
			if found && !activityAttemptReady(attemptState, now) {
				continue
			}
			if err := s.store.SaveNotificationState(ctx, attemptKey, activity.AccountID, "running:"+now.Format(time.RFC3339), now); err != nil {
				evaluationErrors = append(evaluationErrors, fmt.Errorf("保存活动执行状态失败: %w", err))
				continue
			}
			completed, runErr := s.accounts.RunActivity(ctx, activity.AccountID, activity.ID)
			dateKey := activityDate(now)
			if runErr != nil {
				if err := s.store.SaveNotificationState(ctx, attemptKey, activity.AccountID, "failed:"+now.Format(time.RFC3339), now); err != nil {
					evaluationErrors = append(evaluationErrors, fmt.Errorf("保存活动失败退避状态失败: %w", err))
				}
				event := notificationEvent{
					DedupeKey: strings.Join([]string{"activity_failed", activity.AccountID, activity.ID, dateKey}, ":"),
					AccountID: activity.AccountID, EventType: "activity_failed", Severity: "warning", Provider: activity.Provider,
					Title:    activity.Provider + " 自动活动执行失败",
					Message:  fmt.Sprintf("账号「%s」的%s执行失败：%s", activity.AccountAlias, activity.Title, safeError(runErr)),
					Recovery: "检查账号认证状态后在活动中心手动重试",
				}
				delivered, failed, created, sendErr := s.emit(ctx, enabled, event, now)
				result.DeliveredMessages += delivered
				result.FailedMessages += failed
				if created {
					result.TriggeredAlerts++
				}
				if sendErr != nil {
					evaluationErrors = append(evaluationErrors, sendErr)
				}
				continue
			}
			if err := s.store.SaveNotificationState(ctx, attemptKey, activity.AccountID, "completed:"+now.Format(time.RFC3339), now); err != nil {
				evaluationErrors = append(evaluationErrors, fmt.Errorf("保存活动完成状态失败: %w", err))
			}
			failedEventKey := strings.Join([]string{"activity_failed", activity.AccountID, activity.ID, dateKey}, ":")
			if err := s.store.DeleteAlert(ctx, alertID(failedEventKey)); err != nil {
				evaluationErrors = append(evaluationErrors, fmt.Errorf("清理活动失败告警失败: %w", err))
			}
			result.CompletedActivities++
			event := notificationEvent{
				DedupeKey: strings.Join([]string{"activity_completed", activity.AccountID, activity.ID, dateKey}, ":"),
				AccountID: activity.AccountID, EventType: "activity_completed", SkipAlert: true, Severity: "info", Provider: activity.Provider,
				Title:    activity.Provider + " 免费额度活动已完成",
				Message:  fmt.Sprintf("账号「%s」的%s已自动执行：%s", activity.AccountAlias, activity.Title, completed.Description),
				Recovery: "无需操作；最新积分或额度已刷新",
			}
			delivered, failed, _, sendErr := s.emit(ctx, enabled, event, now)
			result.DeliveredMessages += delivered
			result.FailedMessages += failed
			if sendErr != nil {
				evaluationErrors = append(evaluationErrors, sendErr)
			}
		}
	}

	if len(evaluationErrors) > 0 {
		return result, errors.Join(evaluationErrors...)
	}
	return result, nil
}

func (s *Notifications) evaluateLowQuota(ctx context.Context, channels []domain.NotificationChannel, account domain.ConnectedAccount, signal domain.QuotaSignal, identity string, now time.Time, result *NotificationEvaluation) error {
	stateKey := strings.Join([]string{"low_quota_state", account.ID, identity}, ":")
	state, found, err := s.store.NotificationState(ctx, stateKey)
	if err != nil {
		return fmt.Errorf("读取低额度告警状态失败: %w", err)
	}
	belowThreshold := signal.RemainingPercent != nil && *signal.RemainingPercent <= float64(s.Policy().LowQuotaPercent)
	if !belowThreshold {
		if found && strings.HasPrefix(state, "active:") {
			dedupeKey := strings.TrimPrefix(state, "active:")
			if err := s.store.DeleteAlert(ctx, alertID(dedupeKey)); err != nil {
				return fmt.Errorf("恢复低额度告警失败: %w", err)
			}
			if err := s.store.SaveNotificationState(ctx, stateKey, account.ID, "clear", now); err != nil {
				return fmt.Errorf("保存低额度恢复状态失败: %w", err)
			}
		}
		return nil
	}

	dedupeKey := ""
	if found && strings.HasPrefix(state, "active:") {
		dedupeKey = strings.TrimPrefix(state, "active:")
	}
	if dedupeKey == "" {
		dedupeKey = strings.Join([]string{"low_quota", account.ID, identity, now.Format(time.RFC3339Nano)}, ":")
		if err := s.store.SaveNotificationState(ctx, stateKey, account.ID, "active:"+dedupeKey, now); err != nil {
			return fmt.Errorf("保存低额度告警状态失败: %w", err)
		}
	}
	percent := max(0, min(100, *signal.RemainingPercent))
	event := notificationEvent{
		DedupeKey: dedupeKey, AccountID: account.ID, EventType: "low_quota", Severity: "critical", Provider: account.ProviderName,
		Title:    fmt.Sprintf("%s 额度已低于 15%%", account.ProviderName),
		Message:  fmt.Sprintf("账号「%s」的%s仅剩 %.1f%%，请及时切换账号或补充额度。", accountDisplayName(account), signal.Label, percent),
		Recovery: "刷新额度并检查套餐、余额或账号池中的备用账号",
	}
	delivered, failed, created, emitErr := s.emit(ctx, channels, event, now)
	result.DeliveredMessages += delivered
	result.FailedMessages += failed
	if created {
		result.TriggeredAlerts++
	}
	return emitErr
}

func (s *Notifications) evaluateResetReminder(ctx context.Context, channels []domain.NotificationChannel, account domain.ConnectedAccount, signal domain.QuotaSignal, identity string, now time.Time, result *NotificationEvaluation) error {
	stateKey := strings.Join([]string{"quota_reset_state", account.ID, identity}, ":")
	state, found, err := s.store.NotificationState(ctx, stateKey)
	if err != nil {
		return fmt.Errorf("读取重置提醒状态失败: %w", err)
	}
	reminderDays := 0
	var untilReset time.Duration
	if signal.ResetAt != nil && supportsLongResetReminder(signal) {
		untilReset = signal.ResetAt.Sub(now)
		switch {
		case untilReset > 0 && untilReset <= 24*time.Hour:
			reminderDays = 1
		case untilReset > 24*time.Hour && untilReset <= 72*time.Hour:
			reminderDays = 3
		}
	}
	if reminderDays == 0 {
		if found && strings.HasPrefix(state, "active:") {
			dedupeKey := strings.TrimPrefix(state, "active:")
			if err := s.store.DeleteAlert(ctx, alertID(dedupeKey)); err != nil {
				return fmt.Errorf("清理重置提醒失败: %w", err)
			}
			if err := s.store.SaveNotificationState(ctx, stateKey, account.ID, "clear", now); err != nil {
				return fmt.Errorf("保存重置提醒恢复状态失败: %w", err)
			}
		}
		return nil
	}

	resetKey := signal.ResetAt.UTC().Format(time.RFC3339)
	dedupeKey := strings.Join([]string{"quota_reset", fmt.Sprint(reminderDays), account.ID, identity, resetKey}, ":")
	if found && strings.HasPrefix(state, "active:") {
		previousKey := strings.TrimPrefix(state, "active:")
		if previousKey != dedupeKey {
			if err := s.store.DeleteAlert(ctx, alertID(previousKey)); err != nil {
				return fmt.Errorf("更新重置提醒失败: %w", err)
			}
		}
	}
	if !found || state != "active:"+dedupeKey {
		if err := s.store.SaveNotificationState(ctx, stateKey, account.ID, "active:"+dedupeKey, now); err != nil {
			return fmt.Errorf("保存重置提醒状态失败: %w", err)
		}
	}
	event := notificationEvent{
		DedupeKey: dedupeKey, AccountID: account.ID, EventType: fmt.Sprintf("reset_%dd", reminderDays), Severity: "warning", Provider: account.ProviderName,
		Title:    fmt.Sprintf("%s 额度将在 %d 天内重置", account.ProviderName, reminderDays),
		Message:  fmt.Sprintf("账号「%s」的%s将在 %s 重置（%s）。", accountDisplayName(account), signal.Label, formatNotificationTime(*signal.ResetAt), humanDuration(untilReset)),
		Recovery: "无需操作；如需使用剩余额度，请在重置前安排任务",
	}
	delivered, failed, created, emitErr := s.emit(ctx, channels, event, now)
	result.DeliveredMessages += delivered
	result.FailedMessages += failed
	if created {
		result.TriggeredAlerts++
	}
	return emitErr
}

func (s *Notifications) reconcileSignalStates(ctx context.Context, accountID string, observed map[string]struct{}) error {
	states, err := s.store.NotificationStates(ctx, accountID)
	if err != nil {
		return fmt.Errorf("读取额度告警对账状态失败: %w", err)
	}

	var cleanupErrors []error
	for stateKey, state := range states {
		if !strings.HasPrefix(stateKey, "low_quota_state:") && !strings.HasPrefix(stateKey, "quota_reset_state:") {
			continue
		}
		if _, exists := observed[stateKey]; exists {
			continue
		}
		if strings.HasPrefix(state, "active:") {
			dedupeKey := strings.TrimPrefix(state, "active:")
			if err := s.store.DeleteAlert(ctx, alertID(dedupeKey)); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("清理已消失额度信号的告警失败: %w", err))
				continue
			}
		}
		if err := s.store.DeleteNotificationState(ctx, stateKey); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("清理已消失额度信号的状态失败: %w", err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func (s *Notifications) RunScheduler(ctx context.Context) {
	timer := time.NewTimer(initialSchedulerDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		s.evaluateScheduled(ctx)
	}
	ticker := time.NewTicker(defaultSchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluateScheduled(ctx)
		}
	}
}

func (s *Notifications) evaluateScheduled(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	result, err := s.Evaluate(ctx)
	if errors.Is(err, ErrNotificationEvaluationBusy) {
		return
	}
	if err != nil {
		s.logger.Warn("notification evaluation completed with errors", "error", err, "delivered", result.DeliveredMessages, "failed", result.FailedMessages)
		return
	}
	if result.TriggeredAlerts > 0 || result.CompletedActivities > 0 {
		s.logger.Info("notification evaluation completed", "alerts", result.TriggeredAlerts, "delivered", result.DeliveredMessages, "activities", result.CompletedActivities)
	}
}

func (s *Notifications) emit(ctx context.Context, channels []domain.NotificationChannel, event notificationEvent, now time.Time) (int, int, bool, error) {
	if strings.TrimSpace(event.AccountID) == "" {
		return 0, 0, false, fmt.Errorf("通知事件缺少账号标识")
	}
	resolvedAlertID := alertID(event.DedupeKey)
	var alert *domain.Alert
	if !event.SkipAlert {
		alert = &domain.Alert{
			ID: resolvedAlertID, Severity: event.Severity, Title: event.Title, Message: event.Message,
			Provider: event.Provider, CreatedAt: now, Recovery: event.Recovery,
		}
	}
	created, err := s.store.SaveNotificationEventAndAlert(ctx, event.DedupeKey, event.AccountID, resolvedAlertID, event.EventType, now, alert)
	if err != nil {
		return 0, 0, false, fmt.Errorf("保存通知事件与告警失败: %w", err)
	}
	delivered, failed := 0, 0
	var sendErrors []error
	for _, channel := range channels {
		alreadyDelivered, err := s.store.NotificationDelivered(ctx, event.DedupeKey, channel.ID)
		if err != nil {
			failed++
			sendErrors = append(sendErrors, fmt.Errorf("检查渠道 %s 投递状态失败: %w", channel.Name, err))
			continue
		}
		if alreadyDelivered {
			continue
		}
		_, encrypted, err := s.store.NotificationChannel(ctx, channel.ID)
		if err != nil {
			failed++
			sendErrors = append(sendErrors, fmt.Errorf("读取渠道 %s 失败: %w", channel.Name, err))
			continue
		}
		if err := s.sendChannel(ctx, channel, encrypted, event.Title, event.Message); err != nil {
			failed++
			sendErrors = append(sendErrors, fmt.Errorf("渠道 %s 投递失败: %w", channel.Name, err))
			continue
		}
		if err := s.store.RecordNotificationDelivery(ctx, event.DedupeKey, channel.ID, event.EventType, now); err != nil {
			failed++
			sendErrors = append(sendErrors, fmt.Errorf("记录渠道 %s 投递状态失败: %w", channel.Name, err))
			continue
		}
		delivered++
	}
	if s.events != nil && (created || delivered > 0) {
		s.events.Publish(Event{Type: "notification.triggered", Message: event.Title, Timestamp: now})
	}
	return delivered, failed, created, errors.Join(sendErrors...)
}

func (s *Notifications) sendChannel(ctx context.Context, channel domain.NotificationChannel, encrypted []byte, title, message string) error {
	plain, err := s.vault.Decrypt(encrypted)
	if err != nil {
		return fmt.Errorf("解密通知凭据失败: %w", err)
	}
	switch channel.Kind {
	case NotificationKindFeishu:
		var credential feishuCredential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return fmt.Errorf("飞书凭据格式无效")
		}
		return s.sendFeishu(ctx, credential, title, message)
	case NotificationKindQQMail:
		var credential qqMailCredential
		if err := json.Unmarshal(plain, &credential); err != nil {
			return fmt.Errorf("QQ 邮箱凭据格式无效")
		}
		return s.sendMail(ctx, credential, title, message)
	default:
		return fmt.Errorf("不支持的通知渠道")
	}
}

func (s *Notifications) sendFeishu(ctx context.Context, credential feishuCredential, title, message string) error {
	webhook, err := normalizeFeishuWebhook(credential.WebhookURL)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": title + "\n" + message},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建飞书请求失败")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "SuperMonitor/0.7.0")
	resp, err := s.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("连接飞书机器人失败：请求已取消或超时")
		}
		return fmt.Errorf("连接飞书机器人失败：网络连接异常")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("读取飞书响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("飞书机器人返回 HTTP %d", resp.StatusCode)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("飞书机器人返回空响应，无法确认投递成功")
	}
	var result struct {
		Code          *int   `json:"code"`
		Message       string `json:"msg"`
		StatusCode    *int   `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("飞书机器人响应格式无效")
	}
	if result.Code != nil && *result.Code != 0 {
		return fmt.Errorf("飞书机器人拒绝请求（业务状态码 %d）", *result.Code)
	}
	if result.StatusCode != nil && *result.StatusCode != 0 {
		return fmt.Errorf("飞书机器人拒绝请求（业务状态码 %d）", *result.StatusCode)
	}
	if result.Code == nil && result.StatusCode == nil {
		return fmt.Errorf("飞书机器人响应缺少成功状态")
	}
	return nil
}

func (s *Notifications) sendQQMail(ctx context.Context, credential qqMailCredential, title, message string) error {
	if _, err := normalizeMailbox(credential.Sender); err != nil || !strings.HasSuffix(strings.ToLower(credential.Sender), "@qq.com") {
		return fmt.Errorf("QQ 发件邮箱无效")
	}
	if _, err := normalizeMailbox(credential.Recipient); err != nil {
		return fmt.Errorf("收件邮箱无效")
	}
	if strings.TrimSpace(credential.AuthCode) == "" {
		return fmt.Errorf("SMTP 授权码为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	const smtpHost = "smtp.qq.com"
	const smtpAddress = smtpHost + ":465"
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	connection, err := tls.DialWithDialer(dialer, "tcp", smtpAddress, &tls.Config{ServerName: smtpHost, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("连接 QQ 邮箱 SMTP 失败: %w", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(20 * time.Second))
	client, err := smtp.NewClient(connection, smtpHost)
	if err != nil {
		return fmt.Errorf("初始化 QQ 邮箱 SMTP 失败: %w", err)
	}
	defer client.Close()
	if err := client.Auth(smtp.PlainAuth("", credential.Sender, credential.AuthCode, smtpHost)); err != nil {
		return fmt.Errorf("QQ 邮箱认证失败，请检查 SMTP 服务和授权码: %w", err)
	}
	if err := client.Mail(credential.Sender); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	if err := client.Rcpt(credential.Recipient); err != nil {
		return fmt.Errorf("设置收件人失败: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("创建邮件正文失败: %w", err)
	}
	mailBody := buildMailBody(credential.Sender, credential.Recipient, title, message)
	if _, err := io.WriteString(writer, mailBody); err != nil {
		_ = writer.Close()
		return fmt.Errorf("发送邮件正文失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	// DATA 被 QQ SMTP 接受后即视为投递成功。QUIT 只用于礼貌关闭，
	// 网络在此刻断开不应触发下一轮重复发送同一封邮件。
	_ = client.Quit()
	return nil
}

func normalizeFeishuWebhook(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Opaque != "" {
		return "", fmt.Errorf("请输入有效的飞书机器人 HTTPS Webhook")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "open.feishu.cn" && host != "open.larksuite.com" {
		return "", fmt.Errorf("Webhook 必须来自飞书或 Lark 官方机器人地址")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("飞书机器人 Webhook 只能使用 HTTPS 443 端口")
	}
	if parsed.RawQuery != "" {
		return "", fmt.Errorf("飞书机器人 Webhook 不应包含查询参数")
	}
	if !strings.HasPrefix(parsed.EscapedPath(), "/open-apis/bot/v2/hook/") || len(strings.TrimPrefix(parsed.EscapedPath(), "/open-apis/bot/v2/hook/")) < 8 {
		return "", fmt.Errorf("飞书机器人 Webhook 路径无效")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func normalizeMailbox(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("邮箱地址无效")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(address.Address, "@") {
		return "", fmt.Errorf("邮箱地址无效")
	}
	return address.Address, nil
}

func maskMailbox(address string) string {
	local, domainName, ok := strings.Cut(address, "@")
	if !ok || local == "" {
		return "邮箱已配置"
	}
	visible := string([]rune(local)[:1])
	return visible + "***@" + domainName
}

func buildMailBody(sender, recipient, title, message string) string {
	encodedTitle := mime.QEncoding.Encode("UTF-8", strings.ReplaceAll(strings.ReplaceAll(title, "\r", ""), "\n", " "))
	return strings.Join([]string{
		"From: SuperMonitor <" + sender + ">",
		"To: " + recipient,
		"Subject: " + encodedTitle,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		message,
	}, "\r\n")
}

func notificationSignalKey(signal domain.QuotaSignal) string {
	total := "none"
	if signal.Total != nil {
		total = strconv.FormatFloat(*signal.Total, 'g', -1, 64)
	}
	resetAt := "none"
	if signal.ResetAt != nil {
		resetAt = signal.ResetAt.UTC().Format(time.RFC3339)
	}
	expiresAt := "none"
	if signal.ExpiresAt != nil {
		expiresAt = signal.ExpiresAt.UTC().Format(time.RFC3339)
	}
	stableFields := []string{
		signal.Kind,
		signal.Label,
		signal.Unit,
		signal.Source,
		total,
		strconv.FormatInt(signal.WindowSeconds, 10),
		resetAt,
		expiresAt,
	}
	sum := sha256.Sum256([]byte(strings.Join(stableFields, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func supportsLongResetReminder(signal domain.QuotaSignal) bool {
	if signal.WindowSeconds > 0 {
		return signal.WindowSeconds >= int64(72*time.Hour/time.Second)
	}
	label := strings.ToLower(strings.TrimSpace(signal.Label))
	for _, shortWindow := range []string{"5h", "5 h", "5小时", "5 小时", "日限额", "每日", "daily", "hourly", "小时限额"} {
		if strings.Contains(label, shortWindow) {
			return false
		}
	}
	return true
}

func accountDisplayName(account domain.ConnectedAccount) string {
	return firstNonEmpty(account.Alias, account.Email, account.ID)
}

func activityDate(value time.Time) string {
	chinaStandardTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	return value.In(chinaStandardTime).Format("2006-01-02")
}

func activityAttemptReady(state string, now time.Time) bool {
	status, timestamp, ok := strings.Cut(state, ":")
	if !ok {
		return true
	}
	lastAttempt, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return true
	}
	switch status {
	case "completed":
		return false
	case "running":
		return now.Sub(lastAttempt) >= 30*time.Minute
	case "failed":
		return now.Sub(lastAttempt) >= 6*time.Hour
	default:
		return true
	}
}

func formatNotificationTime(value time.Time) string {
	return value.In(time.Local).Format("2006-01-02 15:04")
}

func humanDuration(value time.Duration) string {
	if value <= 0 {
		return "即将重置"
	}
	hours := int(value.Round(time.Minute) / time.Hour)
	if hours >= 24 {
		return fmt.Sprintf("约 %d 天 %d 小时后", hours/24, hours%24)
	}
	minutes := int(value.Round(time.Minute)/time.Minute) % 60
	return fmt.Sprintf("约 %d 小时 %d 分钟后", hours, minutes)
}

func safeError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len([]rune(message)) > 180 {
		return string([]rune(message)[:180]) + "…"
	}
	return message
}

func alertID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "notify-" + hex.EncodeToString(sum[:10])
}
