package main

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) setting(ctx context.Context, key, fallback string) string {
	var value string
	if err := a.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=?", key).Scan(&value); err != nil {
		return fallback
	}
	return value
}

func (a *App) settingFloat(ctx context.Context, key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(a.setting(ctx, key, ""), 64)
	if err != nil {
		return fallback
	}
	return value
}

func (a *App) settingInt(ctx context.Context, key string, fallback int) int {
	value, err := strconv.Atoi(a.setting(ctx, key, ""))
	if err != nil {
		return fallback
	}
	return value
}

func (a *App) scheduledHours(ctx context.Context, weekEnd time.Time) float64 {
	workdayHours := a.settingFloat(ctx, "workday_hours", 8)
	start := weekEnd.AddDate(0, 0, -6)
	overrides := map[string]float64{}
	rows, err := a.db.QueryContext(ctx, `SELECT work_date,work_hours FROM work_calendar WHERE work_date BETWEEN ? AND ?`, isoDate(start), isoDate(weekEnd))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var date string
			var hours float64
			if rows.Scan(&date, &hours) == nil {
				overrides[date] = hours
			}
		}
	}
	available := 0.0
	for day := start; !day.After(weekEnd); day = day.AddDate(0, 0, 1) {
		if hours, ok := overrides[isoDate(day)]; ok {
			available += hours
		} else if day.Weekday() >= time.Monday && day.Weekday() <= time.Friday {
			available += workdayHours
		}
	}
	return available
}

func (a *App) availableHours(ctx context.Context, userID int64, weekEnd time.Time) float64 {
	workdayHours := a.settingFloat(ctx, "workday_hours", 8)
	available := a.scheduledHours(ctx, weekEnd)
	var leave sql.NullFloat64
	_ = a.db.QueryRowContext(ctx, "SELECT leave_days FROM leave_records WHERE week_end=? AND user_id=?", isoDate(weekEnd), userID).Scan(&leave)
	if leave.Valid {
		available -= leave.Float64 * workdayHours
	}
	if available < 0 {
		return 0
	}
	return available
}

func loadRate(hours, available float64) float64 {
	if available <= 0 {
		return 0
	}
	return hours / available
}

func (a *App) loadBand(ctx context.Context, rate float64) string {
	parts := strings.Split(a.setting(ctx, "load_thresholds", "60,80,100,120"), ",")
	thresholds := []float64{.6, .8, 1, 1.2}
	if len(parts) == 4 {
		for i, part := range parts {
			if value, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil {
				thresholds[i] = value / 100
			}
		}
	}
	switch {
	case rate < thresholds[0]:
		return "idle"
	case rate < thresholds[1]:
		return "light"
	case rate <= thresholds[2]:
		return "normal"
	case rate <= thresholds[3]:
		return "busy"
	default:
		return "overload"
	}
}

func (a *App) weekTotals(ctx context.Context, userID int64, weekEnd string) (actual, forecast float64) {
	_ = a.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(hours),0) FROM actual_work_entries WHERE user_id=? AND week_end=?", userID, weekEnd).Scan(&actual)
	_ = a.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(hours),0) FROM forecast_entries WHERE user_id=? AND target_week_end=?", userID, weekEnd).Scan(&forecast)
	return
}

func (a *App) projectWeekTotals(ctx context.Context, userID int64, weekEnd string) (actual, forecast float64) {
	_ = a.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT SUM(a.hours) FROM actual_work_entries a WHERE a.user_id=? AND a.week_end=? AND a.project_id IS NOT NULL AND EXISTS (SELECT 1 FROM forecast_entries f WHERE f.target_week_end=? AND f.user_id=? AND f.project_id=a.project_id AND f.hours>0)),0), COALESCE((SELECT SUM(f.hours) FROM forecast_entries f WHERE f.user_id=? AND f.target_week_end=? AND f.hours>0 AND EXISTS (SELECT 1 FROM actual_work_entries a WHERE a.week_end=? AND a.user_id=? AND a.project_id=f.project_id)),0)", userID, weekEnd, weekEnd, userID, userID, weekEnd, weekEnd, userID).Scan(&actual, &forecast)
	return
}

func (a *App) adjustmentForWeek(ctx context.Context, userID int64, weekEnd time.Time, actual float64) (float64, bool) {
	factor, ok := a.correctionFactorForWeek(ctx, userID, weekEnd)
	if !ok {
		return 0, false
	}
	return actual / factor, true
}

func (a *App) correctionFactorForWeek(ctx context.Context, userID int64, weekEnd time.Time) (float64, bool) {
	// Correction starts in week six only when each of the immediately preceding
	// five weeks was submitted. Its factor is the mean of the immediately
	// preceding four valid, same-project bias coefficients.
	for offset := 1; offset <= 5; offset++ {
		var submitted bool
		_ = a.db.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM actual_work_entries WHERE user_id=? AND week_end=?
		)`, userID, isoDate(weekEnd.AddDate(0, 0, -7*offset))).Scan(&submitted)
		if !submitted {
			return 0, false
		}
	}
	biases := make([]float64, 0, 4)
	for offset := 1; offset <= 4; offset++ {
		date := isoDate(weekEnd.AddDate(0, 0, -7*offset))
		pastActual, pastForecast := a.projectWeekTotals(ctx, userID, date)
		if pastForecast <= 0 {
			return 0, false
		}
		biases = append(biases, pastActual/pastForecast)
	}
	avg := 0.0
	for _, bias := range biases {
		avg += bias
	}
	avg /= float64(len(biases))
	if avg <= 0 {
		return 0, false
	}
	return avg, true
}

func (a *App) employeeAlert(ctx context.Context, userID int64, weekEnd time.Time) bool {
	weeks := a.settingInt(ctx, "alert_consecutive_weeks", 3)
	threshold := a.settingFloat(ctx, "alert_hours_threshold", 48)
	if weeks <= 0 {
		return false
	}
	for offset := 0; offset < weeks; offset++ {
		actual, _ := a.weekTotals(ctx, userID, isoDate(weekEnd.AddDate(0, 0, -7*offset)))
		if actual <= threshold {
			return false
		}
	}
	return true
}

func (a *App) employeeMetric(ctx context.Context, user User, weekEnd time.Time) EmployeeMetric {
	actual, forecast := a.weekTotals(ctx, user.ID, isoDate(weekEnd))
	available := a.availableHours(ctx, user.ID, weekEnd)
	metric := EmployeeMetric{UserID: user.ID, Name: user.Name, Role: user.Role, ActualHours: actual,
		ForecastHours: forecast, AvailableHours: available, LoadRate: loadRate(actual, available)}
	metric.LoadBand = a.loadBand(ctx, metric.LoadRate)
	metric.Alert = a.employeeAlert(ctx, user.ID, weekEnd)
	_ = a.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(hours),0),COUNT(DISTINCT project_id),COALESCE(SUM(CASE WHEN work_category='site' THEN hours ELSE 0 END),0) FROM actual_work_entries WHERE user_id=? AND week_end=? AND project_id IS NOT NULL", user.ID, isoDate(weekEnd)).Scan(&metric.ProjectHours, &metric.ProjectCount, &metric.SiteHours)
	metric.OtherHours = actual - metric.ProjectHours
	projectActual, projectForecast := a.projectWeekTotals(ctx, user.ID, isoDate(weekEnd))
	metric.ProjectActualHours = projectActual
	metric.ProjectForecastHours = projectForecast
	if projectForecast > 0 {
		metric.Bias = projectActual / projectForecast
		metric.HasBias = true
	}
	if adjusted, ok := a.adjustmentForWeek(ctx, user.ID, weekEnd, projectActual); ok {
		otherActual := actual - projectActual
		metric.AdjustedHours = otherActual + adjusted
		metric.AdjustedRate = loadRate(metric.AdjustedHours, available)
		metric.HasAdjusted = true
		metric.CorrectionFactor, _ = a.correctionFactorForWeek(ctx, user.ID, weekEnd)
	}
	return metric
}

func (a *App) listActiveUsers(ctx context.Context) ([]User, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,department_id,name,email,role,is_system_admin,is_test_user,active,must_change_password
		FROM users WHERE active=1 ORDER BY CASE role WHEN 'manager' THEN 1 WHEN 'lead' THEN 2 ELSE 3 END,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DepartmentID, &u.Name, &u.Email, &u.Role, &u.IsSystemAdmin, &u.IsTestUser, &u.Active, &u.MustChangePassword); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (a *App) listStatUsers(ctx context.Context) ([]User, error) {
	users, err := a.listActiveUsers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]User, 0, len(users))
	for _, user := range users {
		if !user.IsTestUser {
			result = append(result, user)
		}
	}
	return result, nil
}

func (a *App) listEmployeeMetrics(ctx context.Context, weekEnd time.Time) ([]EmployeeMetric, error) {
	users, err := a.listStatUsers(ctx)
	if err != nil {
		return nil, err
	}
	metrics := make([]EmployeeMetric, 0, len(users))
	for _, user := range users {
		metrics = append(metrics, a.employeeMetric(ctx, user, weekEnd))
	}
	sort.SliceStable(metrics, func(i, j int) bool {
		if metrics[i].Alert != metrics[j].Alert {
			return metrics[i].Alert
		}
		return metrics[i].LoadRate > metrics[j].LoadRate
	})
	return metrics, nil
}

func (a *App) userTrend(ctx context.Context, user User, weekEnd time.Time, count int) []WeekMetric {
	trend := make([]WeekMetric, 0, count)
	for offset := count - 1; offset >= 0; offset-- {
		date := weekEnd.AddDate(0, 0, -7*offset)
		actual, forecast := a.weekTotals(ctx, user.ID, isoDate(date))
		available := a.availableHours(ctx, user.ID, date)
		metric := WeekMetric{WeekEnd: isoDate(date), WeekLabel: weekLabel(isoDate(date), a.cfg.Location), ActualHours: actual,
			ForecastHours: forecast, Available: available, LoadRate: loadRate(actual, available)}
		projectActual, projectForecast := a.projectWeekTotals(ctx, user.ID, isoDate(date))
		if projectForecast > 0 {
			metric.Bias = projectActual / projectForecast
			metric.HasBias = true
		}
		if adjusted, ok := a.adjustmentForWeek(ctx, user.ID, date, projectActual); ok {
			otherActual := actual - projectActual
			metric.AdjustedHours = otherActual + adjusted
			metric.HasAdjusted = true
		}
		trend = append(trend, metric)
	}
	return trend
}

func trendWithoutBias(trend []WeekMetric) []WeekMetric {
	result := make([]WeekMetric, len(trend))
	copy(result, trend)
	for i := range result {
		result[i].Bias = 0
		result[i].AdjustedHours = 0
		result[i].HasBias = false
		result[i].HasAdjusted = false
	}
	return result
}

func round1(value float64) float64 { return math.Round(value*10) / 10 }
