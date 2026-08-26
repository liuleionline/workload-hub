package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSiteWorklogIsSavedAndAggregatedSeparately(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "site-worklog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试管理员", "site-worklog@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var admin User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&admin.ID, &admin.DepartmentID, &admin.Name, &admin.Email, &admin.Role, &admin.IsSystemAdmin, &admin.Active, &admin.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date)
		VALUES (?,?,?,?,?,?,?,?,?)`, "SITE-1", "驻场统计测试项目", "驻场项目", "大", "总师", admin.ID, admin.ID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()

	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"leave_days": {"0"}}
	values.Set("work_entries_json", `[{"entry_type":"project","work_category":"site","project_id":"`+
		strconv.FormatInt(projectID, 10)+`","project_code":"SITE-1","hours":"16","end_participation":false}]`)
	request := httptest.NewRequest(http.MethodPost, "/worklog", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, &admin))
	recorder := httptest.NewRecorder()
	app.handleWorklogSave(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("site worklog status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	weekEnd := app.worklogWeekEnd(ctx, time.Now().In(location))
	var category, content string
	var hours float64
	if err = db.QueryRow(`SELECT work_category,work_content,hours FROM actual_work_entries
		WHERE week_end=? AND user_id=? AND project_id=?`, isoDate(weekEnd), admin.ID, projectID).Scan(&category, &content, &hours); err != nil {
		t.Fatal(err)
	}
	if category != "site" || content != "工地驻场" || hours != 16 {
		t.Fatalf("saved site entry category=%q content=%q hours=%.1f", category, content, hours)
	}
	metric := app.employeeMetric(ctx, admin, weekEnd)
	if metric.ActualHours != 16 || metric.ProjectHours != 16 || metric.SiteHours != 16 {
		t.Fatalf("employee metric=%+v", metric)
	}
	usages, err := app.projectUsagesForWeek(ctx, weekEnd, projectID, map[string]correctionFactorResult{}, true)
	if err != nil {
		t.Fatal(err)
	}
	usage := usages[projectID]
	if usage.RawHours != 16 || usage.SiteHours != 16 {
		t.Fatalf("project usage=%+v", usage)
	}
	entries, err := app.workEntries(ctx, admin.ID, isoDate(weekEnd), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].WorkCategory != "site" {
		t.Fatalf("loaded entries=%+v", entries)
	}
}

func TestWeeklyPeriodSelection(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	app := &App{cfg: Config{Location: location}}
	request := httptest.NewRequest(http.MethodGet, "/period/employees?period=week&week_end=2026-08-14", nil)
	period := app.periodSelection(request)
	if period.Type != "week" || period.WeekEnd != "2026-08-14" || period.StartDate != "2026-08-14" || period.EndDate != "2026-08-14" || period.WeekCount != 1 {
		t.Fatalf("weekly period=%+v", period)
	}
	if period.Label != "截至2026年8月14日一周" {
		t.Fatalf("weekly period label=%q", period.Label)
	}
}
