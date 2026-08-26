package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ReportRow struct {
	WeekEnd, UserName, ProjectCode, ProjectName, ProjectShortName, WorkContent, WorkCategory, OtherDescription string
	Hours                                                                                                      float64
	Completed                                                                                                  bool
}
type ReportsData struct {
	Year, Quarter int
	Month         int
	WeekEnd       string
	Period        string
	PeriodLabel   string
	StartDate     string
	EndDate       string
	SelectedUser  int64
	Rows          []ReportRow
	TotalHours    float64
	ProjectCount  int
	UserCount     int
	CanViewAll    bool
	Users         []User
}

func (a *App) handleReports(w http.ResponseWriter, r *http.Request) {
	data, err := a.reportData(r)
	if err != nil {
		http.Error(w, "报表读取失败", 500)
		return
	}
	a.render(w, r, http.StatusOK, "reports.html", PageData{Title: "统计与导出", Data: data})
}

func (a *App) reportData(r *http.Request) (ReportsData, error) {
	now := time.Now().In(a.cfg.Location)
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if year < 2020 || year > now.Year()+1 {
		year = now.Year()
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	quarter, _ := strconv.Atoi(r.URL.Query().Get("quarter"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	weekEnd := a.worklogWeekEnd(r.Context(), now)
	if parsed, parseErr := parseISODate(strings.TrimSpace(r.URL.Query().Get("week_end")), a.cfg.Location); parseErr == nil {
		weekEnd = currentWeekEnd(parsed)
	}
	if period == "" {
		if strings.TrimSpace(r.URL.Query().Get("week_end")) != "" {
			period = "week"
		} else if month >= 1 && month <= 12 {
			period = "month"
		} else if quarter >= 1 && quarter <= 4 {
			period = "quarter"
		} else {
			period = "year"
		}
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, a.cfg.Location)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, a.cfg.Location)
	label := fmt.Sprintf("%d年度", year)
	switch period {
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
		period = "year"
		quarter = 0
		month = 0
	}
	user := currentUser(r)
	canAll := user.IsSystemAdmin
	selectedUser, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	if !canAll {
		selectedUser = user.ID
	}
	query := `SELECT a.week_end,u.name,COALESCE(p.code,'其它'),COALESCE(p.name,'其它工作'),COALESCE(p.short_name,'其它'),a.work_content,a.work_category,a.other_description,a.hours,a.end_participation
		FROM actual_work_entries a
		JOIN users u ON u.id=a.user_id
		LEFT JOIN projects p ON p.id=a.project_id
		LEFT JOIN users pc ON pc.id=p.creator_user_id
		WHERE a.week_end>=? AND a.week_end<?
			AND ((?<>0 AND a.user_id=?) OR (?=0 AND u.is_test_user=0 AND (a.project_id IS NULL OR pc.is_test_user=0)))
		ORDER BY a.week_end DESC,u.name,p.short_name`
	rows, err := a.db.QueryContext(r.Context(), query, isoDate(start), isoDate(end), selectedUser, selectedUser, selectedUser)
	if err != nil {
		return ReportsData{}, err
	}
	defer rows.Close()
	data := ReportsData{Year: year, Quarter: quarter, Month: month, WeekEnd: isoDate(weekEnd), Period: period, PeriodLabel: label,
		StartDate: isoDate(start), EndDate: isoDate(end), SelectedUser: selectedUser, CanViewAll: canAll}
	projects := map[string]bool{}
	users := map[string]bool{}
	for rows.Next() {
		var row ReportRow
		if err := rows.Scan(&row.WeekEnd, &row.UserName, &row.ProjectCode, &row.ProjectName, &row.ProjectShortName, &row.WorkContent, &row.WorkCategory, &row.OtherDescription, &row.Hours, &row.Completed); err != nil {
			return data, err
		}
		data.Rows = append(data.Rows, row)
		data.TotalHours += row.Hours
		projects[row.ProjectCode] = true
		users[row.UserName] = true
	}
	data.ProjectCount = len(projects)
	data.UserCount = len(users)
	if canAll {
		data.Users, _ = a.listActiveUsers(r.Context())
	}
	return data, rows.Err()
}
