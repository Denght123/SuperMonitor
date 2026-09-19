package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Denght123/SuperMonitor/internal/domain"
)

func (s *Store) SaveNotificationChannel(ctx context.Context, channel domain.NotificationChannel, encrypted []byte) error {
	now := time.Now().UTC().Truncate(time.Second)
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_channels
		(id, kind, name, target, enabled, encrypted_payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, name=excluded.name, target=excluded.target,
		enabled=excluded.enabled, encrypted_payload=excluded.encrypted_payload, updated_at=excluded.updated_at`,
		channel.ID, channel.Kind, channel.Name, channel.Target, channel.Enabled, encrypted,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}

func (s *Store) NotificationChannels(ctx context.Context) ([]domain.NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, name, target, enabled, created_at, updated_at
		FROM notification_channels ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.NotificationChannel, 0)
	for rows.Next() {
		var channel domain.NotificationChannel
		var enabled bool
		var created, updated string
		if err := rows.Scan(&channel.ID, &channel.Kind, &channel.Name, &channel.Target, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		channel.Enabled = enabled
		channel.CreatedAt, _ = time.Parse(time.RFC3339, created)
		channel.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		result = append(result, channel)
	}
	return result, rows.Err()
}

func (s *Store) NotificationChannel(ctx context.Context, id string) (domain.NotificationChannel, []byte, error) {
	var channel domain.NotificationChannel
	var encrypted []byte
	var enabled bool
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, name, target, enabled, encrypted_payload, created_at, updated_at
		FROM notification_channels WHERE id=?`, id).Scan(&channel.ID, &channel.Kind, &channel.Name, &channel.Target,
		&enabled, &encrypted, &created, &updated)
	if err != nil {
		return domain.NotificationChannel{}, nil, err
	}
	channel.Enabled = enabled
	channel.CreatedAt, _ = time.Parse(time.RFC3339, created)
	channel.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return channel, encrypted, nil
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM notification_channels WHERE id=?", id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) NotificationDelivered(ctx context.Context, dedupeKey, channelID string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_deliveries
		WHERE dedupe_key=? AND channel_id=?)`, dedupeKey, channelID).Scan(&exists)
	return exists == 1, err
}

func (s *Store) SaveNotificationEvent(ctx context.Context, dedupeKey, accountID, alertID, eventType string, createdAt time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notification_events
		(dedupe_key, account_id, alert_id, event_type, created_at) VALUES (?, ?, ?, ?, ?)`,
		dedupeKey, accountID, alertID, eventType, createdAt.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// SaveNotificationEventAndAlert stores the account-scoped event and its
// dashboard alert in one transaction. This prevents account deletion from
// interleaving between the event insert and alert insert and leaving an alert
// that can no longer be associated with the deleted account.
func (s *Store) SaveNotificationEventAndAlert(ctx context.Context, dedupeKey, accountID, alertID, eventType string, createdAt time.Time, alert *domain.Alert) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notification_events
		(dedupe_key, account_id, alert_id, event_type, created_at) VALUES (?, ?, ?, ?, ?)`,
		dedupeKey, accountID, alertID, eventType, createdAt.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	if alert != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alerts(id, severity, title, message, provider, created_at, recovery)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET severity=excluded.severity,
			title=excluded.title, message=excluded.message, provider=excluded.provider,
			recovery=excluded.recovery`, alert.ID, alert.Severity, alert.Title, alert.Message, alert.Provider,
			alert.CreatedAt.UTC().Format(time.RFC3339), alert.Recovery); err != nil {
			return false, err
		}
	}
	created, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return created > 0, nil
}

func (s *Store) RecordNotificationDelivery(ctx context.Context, dedupeKey, channelID, eventType string, sentAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notification_deliveries
		(dedupe_key, channel_id, event_type, sent_at) VALUES (?, ?, ?, ?)`,
		dedupeKey, channelID, eventType, sentAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) NotificationState(ctx context.Context, stateKey string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT state_value FROM notification_states WHERE state_key=?", stateKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) SaveNotificationState(ctx context.Context, stateKey, accountID, value string, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_states(state_key, account_id, state_value, updated_at)
		VALUES (?, ?, ?, ?) ON CONFLICT(state_key) DO UPDATE SET state_value=excluded.state_value,
		updated_at=excluded.updated_at`, stateKey, accountID, value, updatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) NotificationStates(ctx context.Context, accountID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state_key, state_value FROM notification_states
		WHERE account_id=?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		states[key] = value
	}
	return states, rows.Err()
}

func (s *Store) DeleteNotificationState(ctx context.Context, stateKey string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM notification_states WHERE state_key=?", stateKey)
	return err
}

func (s *Store) DeleteAlert(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM alerts WHERE id=?", id)
	return err
}

func (s *Store) SaveAlert(ctx context.Context, alert domain.Alert) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO alerts(id, severity, title, message, provider, created_at, recovery)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET severity=excluded.severity,
		title=excluded.title, message=excluded.message, provider=excluded.provider,
		recovery=excluded.recovery`, alert.ID, alert.Severity, alert.Title,
		alert.Message, alert.Provider, alert.CreatedAt.UTC().Format(time.RFC3339), alert.Recovery)
	return err
}
