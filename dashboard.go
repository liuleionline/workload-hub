package main

import (
	"context"
	"net/http"
	"sort"
	"time"
)

type DashboardData struct {
	WeekEnd            string
	WindowStart        string
	WindowEnd          string
	Self               EmployeeMetric
	SelfTrend          []WeekMetric
	Department         []EmployeeMetric
	Team               []EmployeeMetric
	Projects           []ProjectMetric
	DepartmentActual   float64
	DepartmentCapacity float64
	DepartmentRate     float64
	AlertCount         int
	BiasReadyCount     int
	Period             string
	PeriodLabel        string
	PeriodSelection    PeriodSelection
	ShowForecast       bool
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	now := time.Now().In(a.cfg.Location)
	weekEnd := a.worklogWeekEnd(r.Context(), now)
	periodType := r.URL.Query().Get("period")
	if periodType != "month" && periodType != "quarter" && periodType != "year" {
		periodType = "week"
	}
	selection := a.periodSelection(r)
	if periodType == "week" {
		weekEnd = selection.Start
	}
	start, end := a.reportWindow(r.Context(), weekEnd)

	data := DashboardData{
		WeekEnd: isoDate(weekEnd), WindowStart: start.Format("1月2日 15:04"), WindowEnd: end.Format("1月2日 15:04"),
		Period: periodType, PeriodLabel: selection.Label, PeriodSelection: selection, ShowForecast: user.Role != "designer",
	}
	perms := currentPermissions(r)
	currentDepartment, _ := a.listEmployeeMetrics(r.Context(), weekEnd)
	if periodType == "week" {
		data.Self = a.employeeMetric(r.Context(), *user, weekEnd)
		data.Department = currentDepartment
	} else {
		data.PeriodLabel = selection.Label
		periodData, err := a.employeePeriodData(r.Context(), selection, perms["dashboard.bias"])
		if err != nil {
			http.Error(w, "总览周期数据读取失败", http.StatusInternalServerError)
			return
		}
		for _, metric := range periodData.Employees {
			item := employeeMetricFromPeriod(metric)
			for _, current := range currentDepartment {
				if current.UserID == item.UserID {
					item.Alert = current.Alert
					break
				}
			}
			if item.UserID == user.ID {
				data.Self = item
			}
			data.Department = append(data.Department, item)
		}
		if user.IsTestUser {
			personalPeriod, personalErr := a.employeePeriodDataForUsers(r.Context(), selection, perms["dashboard.bias"], []User{*user})
			if personalErr != nil {
				http.Error(w, "测试账号个人周期数据读取失败", http.StatusInternalServerError)
				return
			}
			if len(personalPeriod.Employees) == 1 {
				data.Self = employeeMetricFromPeriod(personalPeriod.Employees[0])
			}
		}
	}
	data.SelfTrend = a.userTrend(r.Context(), *user, weekEnd, 8)
	if !perms["dashboard.bias"] {
		data.SelfTrend = trendWithoutBias(data.SelfTrend)
	}
	if !data.ShowForecast {
		for i := range data.SelfTrend {
			data.SelfTrend[i].ForecastHours = 0
		}
		data.Self.ForecastHours = 0
	}
	if perms["dashboard.department"] || perms["dashboard.bias"] {
		for _, m := range data.Department {
			data.DepartmentActual += m.ActualHours
			data.DepartmentCapacity += m.AvailableHours
			if m.Alert {
				data.AlertCount++
			}
			if m.HasAdjusted {
				data.BiasReadyCount++
			}
		}
		data.DepartmentRate = loadRate(data.DepartmentActual, data.DepartmentCapacity)
	} else {
		data.Department = nil
	}
	if perms["dashboard.team"] {
		teamSource := data.Department
		if len(teamSource) == 0 {
			if periodType == "week" {
				teamSource = currentDepartment
			} else {
				periodData, _ := a.employeePeriodData(r.Context(), data.PeriodSelection, false)
				for _, metric := range periodData.Employees {
					teamSource = append(teamSource, EmployeeMetric{UserID: metric.UserID, Name: metric.Name, Role: metric.Role, ActualHours: metric.ActualHours, AvailableHours: metric.AvailableHours, LoadRate: metric.LoadRate, LoadBand: metric.LoadBand})
				}
			}
		}
		data.Team = a.teamMetrics(r.Context(), *user, weekEnd, teamSource)
		data.Projects, _ = a.visibleProjectMetrics(r.Context(), *user, weekEnd, perms["projects.view_all"])
	}
	a.render(w, r, http.StatusOK, "dashboard.html", PageData{Title: "工作负荷看板", Data: data, Flash: r.URL.Query().Get("ok")})
}

func employeeMetricFromPeriod(metric EmployeePeriodMetric) EmployeeMetric {
	return EmployeeMetric{
		UserID: metric.UserID, Name: metric.Name, Role: metric.Role,
		ActualHours: metric.ActualHours, AvailableHours: metric.AvailableHours,
		ProjectHours: metric.ProjectHours, SiteHours: metric.SiteHours, OtherHours: metric.OtherHours,
		ProjectCount: metric.ProjectCount,
		LoadRate:     metric.LoadRate, LoadBand: metric.LoadBand,
		ForecastHours: metric.MatchedForecastHours, Bias: metric.Bias, HasBias: metric.HasBias,
		AdjustedHours: metric.EffectiveHours, AdjustedRate: metric.EffectiveRate, HasAdjusted: metric.HasCorrection,
		ProjectActualHours: metric.MatchedActualHours, ProjectForecastHours: metric.MatchedForecastHours,
	}
}

func (a *App) teamMetrics(ctx context.Context, user User, weekEnd time.Time, all []EmployeeMetric) []EmployeeMetric {
	if len(all) == 0 {
		all, _ = a.listEmployeeMetrics(ctx, weekEnd)
	}
	if user.Role == "manager" {
		return all
	}
	rows, err := a.db.QueryContext(ctx, `SELECT DISTINCT pp.user_id FROM project_participations pp
		JOIN projects p ON p.id=pp.project_id
		WHERE pp.status='active' AND (p.creator_user_id=? OR p.executing_lead_user_id=?)`, user.ID, user.ID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ids := map[int64]bool{user.ID: true}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids[id] = true
		}
	}
	team := []EmployeeMetric{}
	for _, m := range all {
		if ids[m.UserID] {
			team = append(team, m)
		}
	}
	return team
}

func (a *App) visibleProjectMetrics(ctx context.Context, user User, weekEnd time.Time, viewAll bool) ([]ProjectMetric, error) {
	query := `SELECT p.id,p.code,p.short_name,p.size,
		COALESCE((SELECT SUM(a.hours) FROM actual_work_entries a JOIN users au ON au.id=a.user_id WHERE a.project_id=p.id AND a.week_end=? AND au.is_test_user=0),0),
		COALESCE((SELECT SUM(f.hours) FROM forecast_entries f JOIN users fu ON fu.id=f.user_id JOIN users fc ON fc.id=f.created_by WHERE f.project_id=p.id AND f.target_week_end=? AND fu.is_test_user=0 AND fc.is_test_user=0),0),
		COALESCE((SELECT COUNT(DISTINCT pp.user_id) FROM project_participations pp JOIN users pu ON pu.id=pp.user_id WHERE pp.project_id=p.id AND pp.status='active' AND pu.is_test_user=0),0)
		FROM projects p JOIN users pc ON pc.id=p.creator_user_id WHERE p.status='active' AND pc.is_test_user=0`
	args := []any{isoDate(weekEnd), isoDate(weekEnd.AddDate(0, 0, 7))}
	if !viewAll {
		query += ` AND (p.creator_user_id=? OR p.executing_lead_user_id=?)`
		args = append(args, user.ID, user.ID)
	}
	query += ` ORDER BY p.short_name`
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics := []ProjectMetric{}
	for rows.Next() {
		var m ProjectMetric
		if err := rows.Scan(&m.ProjectID, &m.Code, &m.ShortName, &m.Size, &m.ActualHours, &m.ForecastHours, &m.MemberCount); err != nil {
			return nil, err
		}
		m.LoadRate = loadRate(m.ActualHours, float64(m.MemberCount)*40)
		metrics = append(metrics, m)
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].ActualHours > metrics[j].ActualHours })
	return metrics, rows.Err()
}
