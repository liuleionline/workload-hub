package main

import (
	"context"
	"net/http"
	"sort"
	"time"
)

type ProjectTypeHours struct {
	Type  string
	Hours float64
}
type DepartmentData struct {
	WeekEnd                                             string
	Trend                                               []WeekMetric
	Employees                                           []EmployeeMetric
	ProjectTypes                                        []ProjectTypeHours
	CanViewForecast                                     bool
	IsForecastView                                      bool
	ActualWeekEnd                                       string
	ForecastWeekEnd                                     string
	CurrentActual, CurrentCapacity, CurrentRate         float64
	CurrentSiteHours                                    float64
	MonthActual, MonthCapacity, MonthAverage, MonthPeak float64
	MonthPeakWeek                                       string
	AlertCount                                          int
	ForecastEmployeeCount                               int
	ForecastProjectCount                                int
}

func (a *App) handleDepartment(w http.ResponseWriter, r *http.Request) {
	now := time.Now().In(a.cfg.Location)
	actualWeekEnd := a.worklogWeekEnd(r.Context(), now)
	forecastWeekEnd := currentWeekEnd(now)
	user := currentUser(r)
	canViewForecast := user != nil && user.Role == "manager"
	isForecastView := canViewForecast && r.URL.Query().Get("view") == "forecast"
	weekEnd := actualWeekEnd
	if isForecastView {
		weekEnd = forecastWeekEnd
	}
	data := DepartmentData{WeekEnd: isoDate(weekEnd), ActualWeekEnd: isoDate(actualWeekEnd), ForecastWeekEnd: isoDate(forecastWeekEnd), CanViewForecast: canViewForecast, IsForecastView: isForecastView}
	data.Trend = a.departmentTrend(r.Context(), weekEnd, 12)
	if isForecastView {
		data.Employees, _ = a.listForecastEmployeeMetrics(r.Context(), weekEnd)
		_ = a.db.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT f.project_id) FROM forecast_entries f JOIN users u ON u.id=f.user_id JOIN users fc ON fc.id=f.created_by JOIN projects p ON p.id=f.project_id JOIN users pc ON pc.id=p.creator_user_id WHERE f.target_week_end=? AND f.hours>0 AND u.is_test_user=0 AND fc.is_test_user=0 AND pc.is_test_user=0`, isoDate(weekEnd)).Scan(&data.ForecastProjectCount)
	} else {
		data.Employees, _ = a.listEmployeeMetrics(r.Context(), weekEnd)
	}
	for _, m := range data.Employees {
		if isForecastView {
			data.CurrentActual += m.ForecastHours
			if m.ForecastHours > 0 {
				data.ForecastEmployeeCount++
			}
		} else {
			data.CurrentActual += m.ActualHours
			data.CurrentSiteHours += m.SiteHours
		}
		data.CurrentCapacity += m.AvailableHours
		if m.Alert {
			data.AlertCount++
		}
	}
	data.CurrentRate = loadRate(data.CurrentActual, data.CurrentCapacity)
	monthStart := time.Date(weekEnd.Year(), weekEnd.Month(), 1, 0, 0, 0, 0, a.cfg.Location)
	monthEnd := monthStart.AddDate(0, 1, 0)
	for _, point := range data.Trend {
		date, err := parseISODate(point.WeekEnd, a.cfg.Location)
		if err == nil && !date.Before(monthStart) && date.Before(monthEnd) {
			data.MonthActual += point.ActualHours
			data.MonthCapacity += point.Available
			if point.LoadRate > data.MonthPeak {
				data.MonthPeak = point.LoadRate
				data.MonthPeakWeek = point.WeekEnd
			}
		}
	}
	data.MonthAverage = loadRate(data.MonthActual, data.MonthCapacity)
	data.ProjectTypes, _ = a.projectTypeHours(r.Context(), isoDate(monthStart), isoDate(monthEnd))
	a.render(w, r, http.StatusOK, "department.html", PageData{Title: "部门人力分析", Data: data})
}

func (a *App) listForecastEmployeeMetrics(ctx context.Context, weekEnd time.Time) ([]EmployeeMetric, error) {
	users, err := a.listStatUsers(ctx)
	if err != nil {
		return nil, err
	}
	metrics := make([]EmployeeMetric, 0, len(users))
	for _, user := range users {
		metric := EmployeeMetric{UserID: user.ID, Name: user.Name, Role: user.Role, AvailableHours: a.availableHours(ctx, user.ID, weekEnd)}
		err = a.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(f.hours),0),COUNT(DISTINCT f.project_id)
			FROM forecast_entries f JOIN users fc ON fc.id=f.created_by JOIN projects p ON p.id=f.project_id JOIN users pc ON pc.id=p.creator_user_id
			WHERE f.user_id=? AND f.target_week_end=? AND f.hours>0 AND fc.is_test_user=0 AND pc.is_test_user=0`, user.ID, isoDate(weekEnd)).Scan(&metric.ForecastHours, &metric.ProjectCount)
		if err != nil {
			return nil, err
		}
		metric.LoadRate = loadRate(metric.ForecastHours, metric.AvailableHours)
		metric.LoadBand = a.loadBand(ctx, metric.LoadRate)
		metrics = append(metrics, metric)
	}
	sort.SliceStable(metrics, func(i, j int) bool { return metrics[i].LoadRate > metrics[j].LoadRate })
	return metrics, nil
}

func (a *App) departmentTrend(ctx context.Context, weekEnd time.Time, count int) []WeekMetric {
	trend := make([]WeekMetric, 0, count)
	for offset := count - 1; offset >= 0; offset-- {
		date := weekEnd.AddDate(0, 0, -7*offset)
		users, _ := a.listStatUsers(ctx)
		point := WeekMetric{WeekEnd: isoDate(date), WeekLabel: weekLabel(isoDate(date), a.cfg.Location)}
		for _, u := range users {
			actual, forecast := a.weekTotals(ctx, u.ID, isoDate(date))
			point.ActualHours += actual
			point.ForecastHours += forecast
			point.Available += a.availableHours(ctx, u.ID, date)
		}
		point.LoadRate = loadRate(point.ActualHours, point.Available)
		trend = append(trend, point)
	}
	return trend
}

func (a *App) projectTypeHours(ctx context.Context, start, end string) ([]ProjectTypeHours, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT p.size,COALESCE(SUM(a.hours),0)
		FROM projects p
		JOIN users pc ON pc.id=p.creator_user_id
		JOIN actual_work_entries a ON a.project_id=p.id
		JOIN users u ON u.id=a.user_id
		WHERE a.week_end>=? AND a.week_end<? AND pc.is_test_user=0 AND u.is_test_user=0
		GROUP BY p.size ORDER BY CASE p.size WHEN '超大' THEN 1 WHEN '大' THEN 2 WHEN '中' THEN 3 ELSE 4 END`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ProjectTypeHours{}
	for rows.Next() {
		var item ProjectTypeHours
		if err := rows.Scan(&item.Type, &item.Hours); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
