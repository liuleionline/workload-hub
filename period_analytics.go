package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PeriodSelection struct {
	Year          int
	Quarter       int
	Month         int
	WeekEnd       string
	Type          string
	ProjectSize   string
	ProjectStatus string
	Label         string
	Start         time.Time
	End           time.Time
	EffectiveEnd  time.Time
	StartDate     string
	EndDate       string
	WeekCount     int
	IsYearToDate  bool
}

type PeriodProjectShare struct {
	ProjectID int64
	Code      string
	ShortName string
	Hours     float64
}

type EmployeePeriodMetric struct {
	UserID               int64
	Name                 string
	Role                 string
	HoursRank            int
	LoadRank             int
	ActualHours          float64
	EffectiveHours       float64
	ProjectHours         float64
	SiteHours            float64
	OtherHours           float64
	AvailableHours       float64
	LoadRate             float64
	EffectiveRate        float64
	ProjectCount         int
	SubmittedWeeks       int
	BusyWeeks            int
	OverloadWeeks        int
	PeakHours            float64
	PeakWeek             string
	MatchedActualHours   float64
	MatchedForecastHours float64
	Bias                 float64
	HasBias              bool
	HasCorrection        bool
	CorrectionWeeks      int
	LoadBand             string
	TopProjects          []PeriodProjectShare
	MoreProjectCount     int
}

type EmployeePeriodData struct {
	Period                  PeriodSelection
	Sort                    string
	Employees               []EmployeePeriodMetric
	Trend                   []WeekMetric
	TotalActual             float64
	TotalSiteHours          float64
	TotalEffective          float64
	TotalCapacity           float64
	DepartmentRate          float64
	DepartmentEffectiveRate float64
	EmployeeCount           int
	ProjectCount            int
	SubmittedEmployeeCount  int
	BusyEmployeeCount       int
	OverloadEmployeeCount   int
	IdleCount               int
	LightCount              int
	NormalCount             int
	BusyCount               int
	OverloadCount           int
	HasCorrection           bool
}

type ProjectPeriodMetric struct {
	ProjectID         int64
	Code              string
	Name              string
	ShortName         string
	Size              string
	Stages            []string
	StageSummary      string
	Status            string
	ExecutingLeadName string
	HoursRank         int
	ParticipantRank   int
	RawHours          float64
	EffectiveHours    float64
	ForecastHours     float64
	SiteHours         float64
	ResourceRate      float64
	WorkShare         float64
	ParticipantCount  int
	ActiveWeeks       int
	PeakHours         float64
	PeakWeek          string
	HasCorrection     bool
}

type ProjectPeriodData struct {
	Period              PeriodSelection
	Sort                string
	Projects            []ProjectPeriodMetric
	Trend               []WeekMetric
	TotalRawHours       float64
	TotalEffectiveHours float64
	TotalForecastHours  float64
	TotalSiteHours      float64
	DepartmentCapacity  float64
	ProjectResourceRate float64
	ProjectCount        int
	ParticipantCount    int
	ActiveProjectCount  int
	HasCorrection       bool
}

func (a *App) handleEmployeePeriod(w http.ResponseWriter, r *http.Request) {
	period := a.periodSelection(r)
	data, err := a.employeePeriodData(r.Context(), period, currentPermissions(r)["dashboard.bias"])
	if err != nil {
		http.Error(w, "周期员工数据读取失败", http.StatusInternalServerError)
		return
	}
	data.Sort = employeePeriodSort(r.URL.Query().Get("sort"))
	sortEmployeePeriodMetrics(data.Employees, data.Sort)
	a.render(w, r, http.StatusOK, "period-employees.html", PageData{Title: "周、月、季、年度人力看板", Data: data})
}

func (a *App) handleProjectPeriod(w http.ResponseWriter, r *http.Request) {
	period := a.periodSelection(r)
	data, err := a.projectPeriodData(r.Context(), period, currentPermissions(r)["dashboard.bias"])
	if err != nil {
		http.Error(w, "周期项目数据读取失败", http.StatusInternalServerError)
		return
	}
	data.Sort = projectPeriodSort(r.URL.Query().Get("sort"))
	sortProjectPeriodMetrics(data.Projects, data.Sort)
	a.render(w, r, http.StatusOK, "period-projects.html", PageData{Title: "周、月、季、年度项目看板", Data: data})
}

func (a *App) periodSelection(r *http.Request) PeriodSelection {
	now := time.Now().In(a.cfg.Location)
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if year < 2020 || year > now.Year() {
		year = now.Year()
	}
	periodType := strings.TrimSpace(r.URL.Query().Get("period"))
	quarter, _ := strconv.Atoi(r.URL.Query().Get("quarter"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	weekEnd := worklogWeekEnd(now)
	if a.db != nil {
		weekEnd = a.worklogWeekEnd(r.Context(), now)
	}
	if parsed, err := parseISODate(strings.TrimSpace(r.URL.Query().Get("week_end")), a.cfg.Location); err == nil {
		weekEnd = currentWeekEnd(parsed)
	}
	if periodType == "" {
		if strings.TrimSpace(r.URL.Query().Get("week_end")) != "" {
			periodType = "week"
		} else if month >= 1 && month <= 12 {
			periodType = "month"
		} else if quarter >= 1 && quarter <= 4 {
			periodType = "quarter"
		} else {
			periodType = "week"
		}
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, a.cfg.Location)
	end := start.AddDate(1, 0, 0)
	label := fmt.Sprintf("%d年度", year)
	switch periodType {
	case "week":
		year = weekEnd.Year()
		quarter = 0
		month = 0
		start = weekEnd
		end = weekEnd.AddDate(0, 0, 1)
		label = fmt.Sprintf("截至%d年%d月%d日一周", weekEnd.Year(), weekEnd.Month(), weekEnd.Day())
	case "month":
		if month < 1 || month > 12 {
			month = int(now.Month())
		}
		quarter = 0
		start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, a.cfg.Location)
		end = start.AddDate(0, 1, 0)
		label = fmt.Sprintf("%d年%d月", year, month)
	case "quarter":
		if quarter < 1 || quarter > 4 {
			quarter = (int(now.Month())-1)/3 + 1
		}
		month = 0
		start = time.Date(year, time.Month((quarter-1)*3+1), 1, 0, 0, 0, 0, a.cfg.Location)
		end = start.AddDate(0, 3, 0)
		label = fmt.Sprintf("%d年第%d季度", year, quarter)
	default:
		periodType = "year"
		quarter = 0
		month = 0
	}
	effectiveEnd := end
	currentEnd := weekEnd.AddDate(0, 0, 1)
	if effectiveEnd.After(currentEnd) {
		effectiveEnd = currentEnd
	}
	if effectiveEnd.Before(start) {
		effectiveEnd = start
	}
	weeks := periodWeekEnds(start, effectiveEnd)
	endDate := "—"
	if len(weeks) > 0 {
		endDate = isoDate(weeks[len(weeks)-1])
	}
	size := strings.TrimSpace(r.URL.Query().Get("size"))
	if size != "超大" && size != "大" && size != "中" && size != "小" {
		size = ""
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "active" && status != "completed" && status != "archived" {
		status = ""
	}
	return PeriodSelection{
		Year: year, Quarter: quarter, Month: month, WeekEnd: isoDate(weekEnd), Type: periodType, ProjectSize: size, ProjectStatus: status,
		Label: label, Start: start, End: end, EffectiveEnd: effectiveEnd,
		StartDate: isoDate(start), EndDate: endDate, WeekCount: len(weeks), IsYearToDate: effectiveEnd.Before(end),
	}
}

func periodWeekEnds(start, end time.Time) []time.Time {
	if !start.Before(end) {
		return nil
	}
	delta := (int(time.Friday) - int(start.Weekday()) + 7) % 7
	first := start.AddDate(0, 0, delta)
	result := []time.Time{}
	for date := first; date.Before(end); date = date.AddDate(0, 0, 7) {
		result = append(result, date)
	}
	return result
}

func (a *App) employeePeriodData(ctx context.Context, period PeriodSelection, allowCorrection bool) (EmployeePeriodData, error) {
	users, err := a.listStatUsers(ctx)
	if err != nil {
		return EmployeePeriodData{}, err
	}
	return a.employeePeriodDataForUsers(ctx, period, allowCorrection, users)
}

func (a *App) employeePeriodDataForUsers(ctx context.Context, period PeriodSelection, allowCorrection bool, users []User) (EmployeePeriodData, error) {
	weeks := periodWeekEnds(period.Start, period.EffectiveEnd)
	data := EmployeePeriodData{Period: period, EmployeeCount: len(users)}
	projectSet := map[int64]bool{}
	departmentTrend := map[string]*WeekMetric{}
	factorCache := map[string]correctionFactorResult{}

	for _, user := range users {
		metric := EmployeePeriodMetric{UserID: user.ID, Name: user.Name, Role: user.Role}
		projects, err := a.employeeProjectShares(ctx, user.ID, period.Start, period.EffectiveEnd)
		if err != nil {
			return data, err
		}
		metric.ProjectCount = len(projects)
		if len(projects) > 3 {
			metric.TopProjects = projects[:3]
			metric.MoreProjectCount = len(projects) - 3
		} else {
			metric.TopProjects = projects
		}
		for _, project := range projects {
			projectSet[project.ProjectID] = true
		}

		for _, weekEnd := range weeks {
			week := isoDate(weekEnd)
			actual, forecast := a.weekTotals(ctx, user.ID, week)
			available := a.availableHours(ctx, user.ID, weekEnd)
			projectHours := 0.0
			_ = a.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(hours),0) FROM actual_work_entries WHERE user_id=? AND week_end=? AND project_id IS NOT NULL", user.ID, week).Scan(&projectHours)
			siteHours := 0.0
			_ = a.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(hours),0) FROM actual_work_entries WHERE user_id=? AND week_end=? AND project_id IS NOT NULL AND work_category='site'", user.ID, week).Scan(&siteHours)
			matchedActual, matchedForecast := a.projectWeekTotals(ctx, user.ID, week)
			effective := actual
			hasCorrection := false
			if allowCorrection && matchedActual > 0 {
				if factor, ok := a.cachedCorrectionFactor(ctx, user.ID, weekEnd, factorCache); ok {
					effective = actual - matchedActual + matchedActual/factor
					hasCorrection = true
					metric.CorrectionWeeks++
					metric.HasCorrection = true
					data.HasCorrection = true
				}
			}

			metric.ActualHours += actual
			metric.EffectiveHours += effective
			metric.ProjectHours += projectHours
			metric.SiteHours += siteHours
			metric.AvailableHours += available
			metric.MatchedActualHours += matchedActual
			metric.MatchedForecastHours += matchedForecast
			var submitted bool
			_ = a.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM actual_work_entries WHERE user_id=? AND week_end=?)", user.ID, week).Scan(&submitted)
			if submitted {
				metric.SubmittedWeeks++
			}
			if actual > metric.PeakHours {
				metric.PeakHours = actual
				metric.PeakWeek = week
			}
			band := a.loadBand(ctx, loadRate(actual, available))
			if band == "busy" {
				metric.BusyWeeks++
			}
			if band == "overload" {
				metric.OverloadWeeks++
			}

			point := departmentTrend[week]
			if point == nil {
				point = &WeekMetric{WeekEnd: week, WeekLabel: weekLabel(week, a.cfg.Location)}
				departmentTrend[week] = point
			}
			point.ActualHours += actual
			point.ForecastHours += forecast
			point.AdjustedHours += effective
			point.Available += available
			point.HasAdjusted = point.HasAdjusted || hasCorrection
		}

		metric.OtherHours = metric.ActualHours - metric.ProjectHours
		metric.LoadRate = loadRate(metric.ActualHours, metric.AvailableHours)
		metric.EffectiveRate = loadRate(metric.EffectiveHours, metric.AvailableHours)
		metric.LoadBand = a.loadBand(ctx, metric.LoadRate)
		if metric.MatchedForecastHours > 0 {
			metric.Bias = metric.MatchedActualHours / metric.MatchedForecastHours
			metric.HasBias = true
		}
		data.Employees = append(data.Employees, metric)
		data.TotalActual += metric.ActualHours
		data.TotalSiteHours += metric.SiteHours
		data.TotalEffective += metric.EffectiveHours
		data.TotalCapacity += metric.AvailableHours
		if metric.SubmittedWeeks > 0 {
			data.SubmittedEmployeeCount++
		}
		if metric.BusyWeeks > 0 {
			data.BusyEmployeeCount++
		}
		if metric.OverloadWeeks > 0 {
			data.OverloadEmployeeCount++
		}
		switch metric.LoadBand {
		case "idle":
			data.IdleCount++
		case "light":
			data.LightCount++
		case "normal":
			data.NormalCount++
		case "busy":
			data.BusyCount++
		case "overload":
			data.OverloadCount++
		}
	}

	data.ProjectCount = len(projectSet)
	data.DepartmentRate = loadRate(data.TotalActual, data.TotalCapacity)
	data.DepartmentEffectiveRate = loadRate(data.TotalEffective, data.TotalCapacity)
	for _, weekEnd := range weeks {
		point := departmentTrend[isoDate(weekEnd)]
		if point == nil {
			point = &WeekMetric{WeekEnd: isoDate(weekEnd), WeekLabel: weekLabel(isoDate(weekEnd), a.cfg.Location)}
		}
		point.LoadRate = loadRate(point.ActualHours, point.Available)
		data.Trend = append(data.Trend, *point)
	}
	assignEmployeePeriodRanks(data.Employees)
	return data, nil
}

func (a *App) employeeProjectShares(ctx context.Context, userID int64, start, end time.Time) ([]PeriodProjectShare, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT p.id,p.code,p.short_name,SUM(a.hours)
		FROM actual_work_entries a JOIN projects p ON p.id=a.project_id
		WHERE a.user_id=? AND a.week_end>=? AND a.week_end<?
		GROUP BY p.id,p.code,p.short_name ORDER BY SUM(a.hours) DESC,p.short_name`, userID, isoDate(start), isoDate(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PeriodProjectShare{}
	for rows.Next() {
		var item PeriodProjectShare
		if err := rows.Scan(&item.ProjectID, &item.Code, &item.ShortName, &item.Hours); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (a *App) projectPeriodData(ctx context.Context, period PeriodSelection, allowCorrection bool) (ProjectPeriodData, error) {
	projects, err := a.visibleProjects(ctx, User{}, true)
	if err != nil {
		return ProjectPeriodData{}, err
	}
	filteredProjects := make([]Project, 0, len(projects))
	for _, project := range projects {
		if project.IsTestData {
			continue
		}
		if period.ProjectSize != "" && project.Size != period.ProjectSize {
			continue
		}
		if period.ProjectStatus != "" && project.Status != period.ProjectStatus {
			continue
		}
		filteredProjects = append(filteredProjects, project)
	}
	projects = filteredProjects
	weeks := periodWeekEnds(period.Start, period.EffectiveEnd)
	data := ProjectPeriodData{Period: period, ProjectCount: len(projects)}
	metrics := map[int64]*ProjectPeriodMetric{}
	for _, project := range projects {
		metrics[project.ID] = &ProjectPeriodMetric{
			ProjectID: project.ID, Code: project.Code, Name: project.Name, ShortName: project.ShortName,
			Size: project.Size, Stages: project.Stages, StageSummary: project.StageSummary(), Status: project.Status,
			ExecutingLeadName: project.ExecutingLeadName,
		}
	}
	participants := map[int64]map[int64]bool{}
	departmentParticipants := map[int64]bool{}
	factorCache := map[string]correctionFactorResult{}
	activeUsers, err := a.listStatUsers(ctx)
	if err != nil {
		return data, err
	}

	for _, weekEnd := range weeks {
		usages, err := a.projectUsagesForWeek(ctx, weekEnd, 0, factorCache, false)
		if err != nil {
			return data, err
		}
		departmentActual, departmentEffective, departmentForecast := 0.0, 0.0, 0.0
		weekHasCorrection := false
		weekCapacity := 0.0
		for _, usage := range usages {
			weekCapacity = usage.DepartmentCapacity
			break
		}
		if len(usages) == 0 {
			for _, user := range activeUsers {
				weekCapacity += a.availableHours(ctx, user.ID, weekEnd)
			}
		}
		data.DepartmentCapacity += weekCapacity
		for projectID, usage := range usages {
			metric := metrics[projectID]
			if metric == nil {
				continue
			}
			effective := usage.RawHours
			if allowCorrection {
				effective = usage.EffectiveHours
				metric.HasCorrection = metric.HasCorrection || usage.HasCorrection
				weekHasCorrection = weekHasCorrection || usage.HasCorrection
				data.HasCorrection = data.HasCorrection || usage.HasCorrection
			}
			metric.RawHours += usage.RawHours
			metric.EffectiveHours += effective
			metric.ForecastHours += usage.ForecastHours
			metric.SiteHours += usage.SiteHours
			if usage.RawHours > 0 {
				metric.ActiveWeeks++
			}
			if usage.RawHours > metric.PeakHours {
				metric.PeakHours = usage.RawHours
				metric.PeakWeek = isoDate(weekEnd)
			}
			departmentActual += usage.RawHours
			departmentEffective += effective
			departmentForecast += usage.ForecastHours
		}
		data.Trend = append(data.Trend, WeekMetric{
			WeekEnd: isoDate(weekEnd), WeekLabel: weekLabel(isoDate(weekEnd), a.cfg.Location),
			ActualHours: departmentActual, AdjustedHours: departmentEffective, ForecastHours: departmentForecast,
			Available: weekCapacity, LoadRate: loadRate(departmentEffective, weekCapacity), HasAdjusted: weekHasCorrection,
		})
	}

	rows, err := a.db.QueryContext(ctx, `SELECT a.project_id,a.user_id
		FROM actual_work_entries a
		JOIN users u ON u.id=a.user_id
		JOIN projects p ON p.id=a.project_id
		JOIN users pc ON pc.id=p.creator_user_id
		WHERE a.project_id IS NOT NULL AND a.week_end>=? AND a.week_end<?
			AND u.is_test_user=0 AND pc.is_test_user=0
		GROUP BY a.project_id,a.user_id`, isoDate(period.Start), isoDate(period.EffectiveEnd))
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var projectID, userID int64
		if err := rows.Scan(&projectID, &userID); err != nil {
			rows.Close()
			return data, err
		}
		if participants[projectID] == nil {
			participants[projectID] = map[int64]bool{}
		}
		participants[projectID][userID] = true
		departmentParticipants[userID] = true
	}
	if err := rows.Close(); err != nil {
		return data, err
	}

	for _, project := range projects {
		metric := metrics[project.ID]
		metric.ParticipantCount = len(participants[project.ID])
		data.TotalRawHours += metric.RawHours
		data.TotalEffectiveHours += metric.EffectiveHours
		data.TotalForecastHours += metric.ForecastHours
		data.TotalSiteHours += metric.SiteHours
		if metric.RawHours > 0 {
			data.ActiveProjectCount++
		}
		data.Projects = append(data.Projects, *metric)
	}
	data.ParticipantCount = len(departmentParticipants)
	data.ProjectResourceRate = loadRate(data.TotalEffectiveHours, data.DepartmentCapacity)
	for i := range data.Projects {
		data.Projects[i].ResourceRate = loadRate(data.Projects[i].EffectiveHours, data.DepartmentCapacity)
		data.Projects[i].WorkShare = loadRate(data.Projects[i].RawHours, data.TotalRawHours)
	}
	assignProjectPeriodRanks(data.Projects)
	return data, nil
}

func employeePeriodSort(value string) string {
	switch value {
	case "hours", "projects", "name":
		return value
	default:
		return "load"
	}
}

func projectPeriodSort(value string) string {
	switch value {
	case "participants", "share", "name":
		return value
	default:
		return "hours"
	}
}

func assignEmployeePeriodRanks(metrics []EmployeePeriodMetric) {
	hoursOrder := append([]EmployeePeriodMetric(nil), metrics...)
	sort.SliceStable(hoursOrder, func(i, j int) bool { return hoursOrder[i].ActualHours > hoursOrder[j].ActualHours })
	loadOrder := append([]EmployeePeriodMetric(nil), metrics...)
	sort.SliceStable(loadOrder, func(i, j int) bool { return loadOrder[i].LoadRate > loadOrder[j].LoadRate })
	for rank, item := range hoursOrder {
		for i := range metrics {
			if metrics[i].UserID == item.UserID {
				metrics[i].HoursRank = rank + 1
			}
		}
	}
	for rank, item := range loadOrder {
		for i := range metrics {
			if metrics[i].UserID == item.UserID {
				metrics[i].LoadRank = rank + 1
			}
		}
	}
}

func sortEmployeePeriodMetrics(metrics []EmployeePeriodMetric, order string) {
	sort.SliceStable(metrics, func(i, j int) bool {
		switch order {
		case "hours":
			return metrics[i].ActualHours > metrics[j].ActualHours
		case "projects":
			if metrics[i].ProjectCount != metrics[j].ProjectCount {
				return metrics[i].ProjectCount > metrics[j].ProjectCount
			}
			return metrics[i].ActualHours > metrics[j].ActualHours
		case "name":
			return strings.Compare(metrics[i].Name, metrics[j].Name) < 0
		default:
			return metrics[i].LoadRate > metrics[j].LoadRate
		}
	})
}

func assignProjectPeriodRanks(metrics []ProjectPeriodMetric) {
	hoursOrder := append([]ProjectPeriodMetric(nil), metrics...)
	sort.SliceStable(hoursOrder, func(i, j int) bool { return hoursOrder[i].RawHours > hoursOrder[j].RawHours })
	participantsOrder := append([]ProjectPeriodMetric(nil), metrics...)
	sort.SliceStable(participantsOrder, func(i, j int) bool {
		return participantsOrder[i].ParticipantCount > participantsOrder[j].ParticipantCount
	})
	for rank, item := range hoursOrder {
		for i := range metrics {
			if metrics[i].ProjectID == item.ProjectID {
				metrics[i].HoursRank = rank + 1
			}
		}
	}
	for rank, item := range participantsOrder {
		for i := range metrics {
			if metrics[i].ProjectID == item.ProjectID {
				metrics[i].ParticipantRank = rank + 1
			}
		}
	}
}

func sortProjectPeriodMetrics(metrics []ProjectPeriodMetric, order string) {
	sort.SliceStable(metrics, func(i, j int) bool {
		switch order {
		case "participants":
			if metrics[i].ParticipantCount != metrics[j].ParticipantCount {
				return metrics[i].ParticipantCount > metrics[j].ParticipantCount
			}
			return metrics[i].RawHours > metrics[j].RawHours
		case "share":
			return metrics[i].ResourceRate > metrics[j].ResourceRate
		case "name":
			return strings.Compare(metrics[i].ShortName, metrics[j].ShortName) < 0
		default:
			return metrics[i].RawHours > metrics[j].RawHours
		}
	})
}
