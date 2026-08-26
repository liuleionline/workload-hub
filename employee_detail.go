package main

import (
	"context"
	"net/http"
	"sort"
	"time"
)

type EmployeeProjectWork struct {
	ProjectID     int64
	Code          string
	Name          string
	ShortName     string
	Size          string
	Stages        []string
	Contents      []string
	ActualHours   float64
	SiteHours     float64
	ForecastHours float64
	Bias          float64
	HasActual     bool
	HasForecast   bool
	HasBias       bool
}

func (p EmployeeProjectWork) StageSummary() string {
	if len(p.Stages) == 0 {
		return "阶段待完善"
	}
	result := ""
	for _, stage := range p.Stages {
		if result != "" {
			result += "、"
		}
		result += stage
	}
	return result
}

type EmployeeDetailData struct {
	Employee  User
	WeekEnd   string
	Metric    EmployeeMetric
	Trend     []WeekMetric
	Projects  []EmployeeProjectWork
	Other     []WorkEntry
	LeaveDays float64
}

func (a *App) handleEmployeeDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	employee, err := a.getUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	weekEnd := a.worklogWeekEnd(r.Context(), time.Now().In(a.cfg.Location))
	if raw := r.URL.Query().Get("week_end"); raw != "" {
		if parsed, parseErr := parseISODate(raw, a.cfg.Location); parseErr == nil {
			weekEnd = currentWeekEnd(parsed)
		}
	}
	projects, other, err := a.employeeProjectWork(r.Context(), employee.ID, isoDate(weekEnd))
	if err != nil {
		http.Error(w, "员工负荷明细读取失败", http.StatusInternalServerError)
		return
	}
	data := EmployeeDetailData{
		Employee: *employee,
		WeekEnd:  isoDate(weekEnd),
		Metric:   a.employeeMetric(r.Context(), *employee, weekEnd),
		Trend:    a.userTrend(r.Context(), *employee, weekEnd, 12),
		Projects: projects,
		Other:    other,
	}
	_ = a.db.QueryRowContext(r.Context(), "SELECT leave_days FROM leave_records WHERE week_end=? AND user_id=?", data.WeekEnd, employee.ID).Scan(&data.LeaveDays)
	if !currentPermissions(r)["dashboard.bias"] {
		data.Trend = trendWithoutBias(data.Trend)
	}
	a.render(w, r, http.StatusOK, "employee-detail.html", PageData{Title: employee.Name + " · 工作负荷详情", Data: data})
}

func (a *App) employeeProjectWork(ctx context.Context, userID int64, weekEnd string) ([]EmployeeProjectWork, []WorkEntry, error) {
	entries, err := a.workEntries(ctx, userID, weekEnd, false)
	if err != nil {
		return nil, nil, err
	}
	byProject := map[int64]*EmployeeProjectWork{}
	other := []WorkEntry{}
	for _, entry := range entries {
		if entry.ProjectID == 0 {
			other = append(other, entry)
			continue
		}
		item := byProject[entry.ProjectID]
		if item == nil {
			item = &EmployeeProjectWork{
				ProjectID: entry.ProjectID, Code: entry.ProjectCode, Name: entry.ProjectName,
				ShortName: entry.ProjectShortName,
			}
			byProject[entry.ProjectID] = item
		}
		item.ActualHours += entry.Hours
		if entry.WorkCategory == "site" {
			item.SiteHours += entry.Hours
		}
		item.HasActual = true
		content := entry.WorkContent
		if content != "" {
			seen := false
			for _, existing := range item.Contents {
				if existing == content {
					seen = true
					break
				}
			}
			if !seen {
				item.Contents = append(item.Contents, content)
			}
		}
	}
	rows, err := a.db.QueryContext(ctx, `SELECT f.project_id,p.code,p.name,p.short_name,p.size,f.hours
		FROM forecast_entries f JOIN projects p ON p.id=f.project_id
		WHERE f.user_id=? AND f.target_week_end=? AND f.hours>0`, userID, weekEnd)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var projectID int64
		var code, name, shortName, size string
		var hours float64
		if err := rows.Scan(&projectID, &code, &name, &shortName, &size, &hours); err != nil {
			rows.Close()
			return nil, nil, err
		}
		item := byProject[projectID]
		if item == nil {
			item = &EmployeeProjectWork{ProjectID: projectID, Code: code, Name: name, ShortName: shortName, Size: size}
			byProject[projectID] = item
		}
		item.Size = size
		item.ForecastHours += hours
		item.HasForecast = true
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	projects := make([]EmployeeProjectWork, 0, len(byProject))
	for _, item := range byProject {
		item.Stages, _ = a.projectStages(ctx, item.ProjectID)
		if item.Size == "" {
			_ = a.db.QueryRowContext(ctx, "SELECT size FROM projects WHERE id=?", item.ProjectID).Scan(&item.Size)
		}
		if item.HasActual && item.HasForecast && item.ForecastHours > 0 {
			item.Bias = item.ActualHours / item.ForecastHours
			item.HasBias = true
		}
		projects = append(projects, *item)
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].ActualHours != projects[j].ActualHours {
			return projects[i].ActualHours > projects[j].ActualHours
		}
		return projects[i].ShortName < projects[j].ShortName
	})
	return projects, other, nil
}
