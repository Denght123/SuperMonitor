package domain

import "time"

type NotificationChannel struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type NotificationPolicy struct {
	LowQuotaPercent  int   `json:"lowQuotaPercent"`
	ResetReminders   []int `json:"resetReminderDays"`
	SchedulerMinutes int   `json:"schedulerMinutes"`
	AutoActivities   bool  `json:"autoActivities"`
}
