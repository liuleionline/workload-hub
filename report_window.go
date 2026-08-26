package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type ReportSchedule struct {
	OpenWeekday  time.Weekday
	OpenMinute   int
	CloseWeekday time.Weekday
	CloseMinute  int
}

func defaultReportSchedule() ReportSchedule {
	return ReportSchedule{OpenWeekday: time.Friday, OpenMinute: 12 * 60, CloseWeekday: time.Saturday, CloseMinute: 12 * 60}
}

func reportDayOffset(day time.Weekday) int {
	if day == time.Sunday {
		return 2
	}
	return int(day) - int(time.Friday)
}

func reportWindowWithSchedule(weekEnd time.Time, schedule ReportSchedule) (time.Time, time.Time) {
	openDay := weekEnd.AddDate(0, 0, reportDayOffset(schedule.OpenWeekday))
	closeDay := weekEnd.AddDate(0, 0, reportDayOffset(schedule.CloseWeekday))
	start := time.Date(openDay.Year(), openDay.Month(), openDay.Day(), schedule.OpenMinute/60, schedule.OpenMinute%60, 0, 0, weekEnd.Location())
	end := time.Date(closeDay.Year(), closeDay.Month(), closeDay.Day(), schedule.CloseMinute/60, schedule.CloseMinute%60, 0, 0, weekEnd.Location())
	if !end.After(start) {
		end = end.AddDate(0, 0, 7)
	}
	return start, end
}

func reportWindowOpenWithSchedule(now time.Time, schedule ReportSchedule) bool {
	current := currentWeekEnd(now)
	for _, weekEnd := range []time.Time{current, current.AddDate(0, 0, -7)} {
		start, end := reportWindowWithSchedule(weekEnd, schedule)
		if !now.Before(start) && now.Before(end) {
			return true
		}
	}
	return false
}

func reportWindowOpen(now time.Time) bool {
	return reportWindowOpenWithSchedule(now, defaultReportSchedule())
}

func worklogWeekEndWithSchedule(now time.Time, schedule ReportSchedule) time.Time {
	weekEnd := currentWeekEnd(now)
	start, _ := reportWindowWithSchedule(weekEnd, schedule)
	if now.Before(start) {
		return weekEnd.AddDate(0, 0, -7)
	}
	return weekEnd
}

func weekdayName(day time.Weekday) string {
	names := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	if int(day) < 0 || int(day) >= len(names) {
		return ""
	}
	return names[day]
}

func minuteLabel(value int) string {
	return fmt.Sprintf("%02d:%02d", value/60, value%60)
}

func parseClockMinute(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func (s ReportSchedule) Label() string {
	return fmt.Sprintf("每%s%s至%s%s", weekdayName(s.OpenWeekday), minuteLabel(s.OpenMinute), weekdayName(s.CloseWeekday), minuteLabel(s.CloseMinute))
}

func (s ReportSchedule) Valid() bool {
	if s.OpenWeekday < time.Sunday || s.OpenWeekday > time.Saturday || s.CloseWeekday < time.Sunday || s.CloseWeekday > time.Saturday ||
		s.OpenMinute < 0 || s.OpenMinute >= 24*60 || s.CloseMinute < 0 || s.CloseMinute >= 24*60 {
		return false
	}
	weekEnd := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	start, end := reportWindowWithSchedule(weekEnd, s)
	return end.After(start) && end.Sub(start) < 7*24*time.Hour
}

func (a *App) reportSchedule(ctx context.Context) ReportSchedule {
	schedule := defaultReportSchedule()
	if value, err := strconv.Atoi(a.setting(ctx, "report_open_weekday", "5")); err == nil {
		schedule.OpenWeekday = time.Weekday(value)
	}
	if value, err := strconv.Atoi(a.setting(ctx, "report_open_minute", "720")); err == nil {
		schedule.OpenMinute = value
	}
	if value, err := strconv.Atoi(a.setting(ctx, "report_close_weekday", "6")); err == nil {
		schedule.CloseWeekday = time.Weekday(value)
	}
	if value, err := strconv.Atoi(a.setting(ctx, "report_close_minute", "720")); err == nil {
		schedule.CloseMinute = value
	}
	if !schedule.Valid() {
		return defaultReportSchedule()
	}
	return schedule
}

func (a *App) reportWindowOpen(ctx context.Context, now time.Time) bool {
	return reportWindowOpenWithSchedule(now, a.reportSchedule(ctx))
}

func (a *App) reportWindow(ctx context.Context, weekEnd time.Time) (time.Time, time.Time) {
	return reportWindowWithSchedule(weekEnd, a.reportSchedule(ctx))
}

func (a *App) worklogWeekEnd(ctx context.Context, now time.Time) time.Time {
	return worklogWeekEndWithSchedule(now, a.reportSchedule(ctx))
}

func (a *App) forecastTargetWeekEnd(ctx context.Context, now time.Time) time.Time {
	return a.worklogWeekEnd(ctx, now).AddDate(0, 0, 7)
}

func (a *App) withReportWindow(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().In(a.cfg.Location)
		if !a.reportEditAllowed(r.Context(), now, currentUser(r)) {
			http.Error(w, "本周填报窗口尚未开放或已经截止。当前设置为"+a.reportSchedule(r.Context()).Label()+"。", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func reportEditAllowed(now time.Time, user *User) bool {
	return reportWindowOpen(now) || (user != nil && user.IsTestUser)
}

func worklogEditAllowed(now time.Time, user *User) bool {
	return reportEditAllowed(now, user) || (user != nil && user.IsSystemAdmin)
}

func (a *App) reportEditAllowed(ctx context.Context, now time.Time, user *User) bool {
	return a.reportWindowOpen(ctx, now) || (user != nil && user.IsTestUser)
}

func (a *App) worklogEditAllowed(ctx context.Context, now time.Time, user *User) bool {
	return a.reportEditAllowed(ctx, now, user) || (user != nil && user.IsSystemAdmin)
}

func (a *App) withWorklogWindow(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().In(a.cfg.Location)
		if !a.worklogEditAllowed(r.Context(), now, currentUser(r)) {
			http.Error(w, "本周填报窗口尚未开放或已经截止。当前设置为"+a.reportSchedule(r.Context()).Label()+"。", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
