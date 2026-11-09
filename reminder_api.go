package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const reminderAPIPath = "/api/v1/worklog-reminders"

type ReminderRecipient struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
}

type ReminderCounts struct {
	Eligible  int `json:"eligible"`
	Submitted int `json:"submitted"`
	Missing   int `json:"missing"`
}

type WorklogReminderResponse struct {
	SchemaVersion int                 `json:"schema_version"`
	CheckedAt     string              `json:"checked_at"`
	Timezone      string              `json:"timezone"`
	WeekEnd       string              `json:"week_end"`
	WindowStart   string              `json:"window_start"`
	WindowEnd     string              `json:"window_end"`
	ReminderAt    string              `json:"reminder_at,omitempty"`
	WindowOpen    bool                `json:"window_open"`
	ShouldRemind  bool                `json:"should_remind"`
	Reason        string              `json:"reason"`
	Counts        *ReminderCounts     `json:"counts,omitempty"`
	Missing       []ReminderRecipient `json:"missing"`
}

func reminderJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *App) handleWorklogReminders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Vary", "Authorization")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		reminderJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	expected, err := base64.RawURLEncoding.DecodeString(a.cfg.ReminderTokenHash)
	if err != nil || len(expected) != 32 {
		reminderJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "integration_disabled"})
		return
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 128 ||
		subtle.ConstantTimeCompare([]byte(hashToken(parts[1])), []byte(a.cfg.ReminderTokenHash)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="worklog-reminders"`)
		reminderJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := a.worklogReminderSnapshot(ctx, time.Now().In(a.cfg.Location))
	if err != nil {
		slog.Error("提醒接口读取失败", "error", err)
		reminderJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "status_unavailable"})
		return
	}
	reminderJSON(w, http.StatusOK, response)
}

func (a *App) worklogReminderSnapshot(ctx context.Context, now time.Time) (*WorklogReminderResponse, error) {
	now = now.In(a.cfg.Location)
	window, err := a.reportingWindowAt(ctx, now)
	if err != nil {
		return nil, err
	}
	weekEnd, start, end := window.WeekEnd, window.Start, window.End
	reminderAt := start.Add(reminderDelay)
	response := &WorklogReminderResponse{
		SchemaVersion: 1, CheckedAt: now.Format(time.RFC3339), Timezone: a.cfg.Location.String(),
		WeekEnd: isoDate(weekEnd), WindowStart: start.Format(time.RFC3339), WindowEnd: end.Format(time.RFC3339),
		WindowOpen: window.Open(now), Reason: "window_closed", Missing: []ReminderRecipient{},
	}
	if !window.Required {
		response.Reason = "holiday_week"
		return response, nil
	}
	response.ReminderAt = reminderAt.Format(time.RFC3339)
	if !response.WindowOpen {
		return response, nil
	}
	status, err := a.worklogSubmissionStatusForWeek(ctx, response.WeekEnd)
	if err != nil {
		return nil, err
	}
	response.Counts = &ReminderCounts{Eligible: status.Total, Submitted: status.Submitted, Missing: len(status.Missing)}
	response.Reason = "all_submitted"
	for _, user := range status.Missing {
		response.Missing = append(response.Missing, ReminderRecipient{UserID: user.ID, Name: user.Name})
	}
	if len(response.Missing) > 0 {
		// The scheduler checks once per minute. Only this minute is eligible:
		// no early send, no late catch-up after a restart or a failed batch.
		response.Reason = "reminder_not_due"
		if !now.Before(reminderAt) {
			response.Reason = "reminder_time_passed"
			if now.Before(reminderAt.Add(time.Minute)) {
				response.ShouldRemind = true
				response.Reason = "missing_submissions"
			}
		}
	}
	return response, nil
}

// Unlike interactive pages, automation must fail closed on unreadable/invalid settings.
func (a *App) reminderSchedule(ctx context.Context) (ReportSchedule, error) {
	var schedule ReportSchedule
	var openDay int
	fields := []struct {
		key string
		out *int
	}{
		{"report_open_weekday", &openDay}, {"report_open_minute", &schedule.OpenMinute},
	}
	for _, field := range fields {
		var value string
		if err := a.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=?", field.key).Scan(&value); err != nil {
			return schedule, err
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return schedule, err
		}
		*field.out = n
	}
	schedule = reportScheduleFromStart(time.Weekday(openDay), schedule.OpenMinute)
	if !schedule.Valid() {
		return schedule, fmt.Errorf("invalid reporting schedule")
	}
	return schedule, nil
}
