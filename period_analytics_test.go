package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuarterEmployeeAndProjectAnalytics(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "部门领导", "period-manager@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var manager User
	if err = db.QueryRow("SELECT id,department_id,name,email,mobile,qualification,professional_title,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&manager.ID, &manager.DepartmentID, &manager.Name, &manager.Email, &manager.Mobile, &manager.Qualification,
		&manager.ProfessionalTitle, &manager.Role, &manager.IsSystemAdmin, &manager.Active, &manager.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	designerResult, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role,must_change_password) VALUES (?,?,?,?,?,0)", manager.DepartmentID, "设计师甲", "period-designer@test.local", "unused", "designer")
	if err != nil {
		t.Fatal(err)
	}
	designerID, _ := designerResult.LastInsertId()
	projectResult, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)",
		"PERIOD-1", "季度分析测试项目", "季度项目", "中", "总师", manager.ID, manager.ID, "2026-04-01", "2026-06-30")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := projectResult.LastInsertId()
	if _, err = db.Exec("INSERT INTO project_stages(project_id,stage) VALUES (?,?)", projectID, "施工图设计"); err != nil {
		t.Fatal(err)
	}
	week := "2026-04-10"
	entries := []struct {
		userID    int64
		projectID any
		hours     float64
		other     string
	}{
		{manager.ID, projectID, 30, ""},
		{manager.ID, nil, 5, "部门协调"},
		{designerID, projectID, 15, ""},
	}
	for _, entry := range entries {
		if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content,other_description) VALUES (?,?,?,?,?,?)", week, entry.userID, entry.projectID, entry.hours, "A子项设计", entry.other); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", week, projectID, manager.ID, 25, manager.ID); err != nil {
		t.Fatal(err)
	}

	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	period := PeriodSelection{
		Year: 2026, Quarter: 2, Label: "2026年第2季度",
		Start: time.Date(2026, 4, 1, 0, 0, 0, 0, location), End: time.Date(2026, 7, 1, 0, 0, 0, 0, location),
		EffectiveEnd: time.Date(2026, 7, 1, 0, 0, 0, 0, location), StartDate: "2026-04-01", EndDate: "2026-06-26", WeekCount: 13,
	}
	employees, err := app.employeePeriodData(ctx, period, true)
	if err != nil {
		t.Fatal(err)
	}
	if employees.TotalActual != 50 || employees.ProjectCount != 1 || employees.SubmittedEmployeeCount != 2 || len(employees.Employees) != 2 {
		t.Fatalf("employee period data=%+v", employees)
	}
	var managerMetric EmployeePeriodMetric
	for _, metric := range employees.Employees {
		if metric.UserID == manager.ID {
			managerMetric = metric
		}
	}
	if managerMetric.ActualHours != 35 || managerMetric.ProjectHours != 30 || managerMetric.OtherHours != 5 || managerMetric.ProjectCount != 1 || managerMetric.SubmittedWeeks != 1 {
		t.Fatalf("manager metric=%+v", managerMetric)
	}

	projects, err := app.projectPeriodData(ctx, period, true)
	if err != nil {
		t.Fatal(err)
	}
	if projects.TotalRawHours != 45 || projects.ActiveProjectCount != 1 || projects.ParticipantCount != 2 || len(projects.Projects) != 1 {
		t.Fatalf("project period data=%+v", projects)
	}
	if projects.Projects[0].RawHours != 45 || projects.Projects[0].ParticipantCount != 2 || projects.Projects[0].ActiveWeeks != 1 || projects.Projects[0].HoursRank != 1 {
		t.Fatalf("project metric=%+v", projects.Projects[0])
	}

	render := func(page string, data any) string {
		request := httptest.NewRequest(http.MethodGet, "/period/test", nil)
		requestContext := context.WithValue(request.Context(), userContextKey, &manager)
		requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{"dashboard.department": true, "dashboard.bias": true})
		requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
		request = request.WithContext(requestContext)
		recorder := httptest.NewRecorder()
		app.render(recorder, request, http.StatusOK, page, PageData{Title: "周期看板", Data: data})
		if recorder.Code != http.StatusOK {
			t.Fatalf("render %s status=%d body=%s", page, recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	if body := render("period-employees.html", employees); !strings.Contains(body, "员工工时、负荷与项目参与排名") || !strings.Contains(body, "季度项目") {
		t.Fatalf("employee page missing analytics: %s", body)
	}
	if body := render("period-projects.html", projects); !strings.Contains(body, "项目工时与资源占用排名") || !strings.Contains(body, "45.0h") {
		t.Fatalf("project page missing analytics: %s", body)
	}
}

func TestPeriodWeekEnds(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	weeks := periodWeekEnds(time.Date(2026, 4, 1, 0, 0, 0, 0, location), time.Date(2026, 7, 1, 0, 0, 0, 0, location))
	if len(weeks) != 13 || isoDate(weeks[0]) != "2026-04-03" || isoDate(weeks[len(weeks)-1]) != "2026-06-26" {
		t.Fatalf("weeks=%v", weeks)
	}
}
