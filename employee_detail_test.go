package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEmployeeDetailAggregatesProjectsAndProtectsBias(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "员工甲", "employee-detail@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow("SELECT id,department_id,name,email,mobile,qualification,professional_title,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Mobile, &user.Qualification, &user.ProfessionalTitle,
		&user.Role, &user.IsSystemAdmin, &user.Active, &user.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	createProject := func(code, shortName string) int64 {
		result, createErr := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", code, shortName+"全名", shortName, "中", "总师", user.ID, user.ID, "2026-01-01", "2026-12-31")
		if createErr != nil {
			t.Fatal(createErr)
		}
		id, _ := result.LastInsertId()
		return id
	}
	actualProjectID := createProject("DETAIL-1", "实际项目")
	forecastOnlyID := createProject("DETAIL-2", "仅预估项目")
	if _, err = db.Exec("INSERT INTO project_stages(project_id,stage) VALUES (?,?),(?,?)", actualProjectID, "施工图设计", forecastOnlyID, "方案设计"); err != nil {
		t.Fatal(err)
	}
	week := "2026-08-07"
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content,other_description) VALUES (?,?,?,?,?,?),(?,?,?,?,?,?),(?,?,?,?,?,?)",
		week, user.ID, actualProjectID, 16, "A子项（15600平米混凝土框架结构）设计", "",
		week, user.ID, actualProjectID, 8, "B子项校对", "",
		week, user.ID, nil, 4, "", "临时协调"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?),(?,?,?,?,?)",
		week, actualProjectID, user.ID, 20, user.ID,
		week, forecastOnlyID, user.ID, 10, user.ID); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(Config{Location: time.FixedZone("CST", 8*3600)}, db)
	if err != nil {
		t.Fatal(err)
	}
	projects, other, err := app.employeeProjectWork(ctx, user.ID, week)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || len(other) != 1 {
		t.Fatalf("projects=%d other=%d, want 2/1", len(projects), len(other))
	}
	var actualProject, forecastOnly EmployeeProjectWork
	for _, project := range projects {
		if project.ProjectID == actualProjectID {
			actualProject = project
		}
		if project.ProjectID == forecastOnlyID {
			forecastOnly = project
		}
	}
	if actualProject.ActualHours != 24 || actualProject.ForecastHours != 20 || len(actualProject.Contents) != 2 || !actualProject.HasBias || actualProject.Bias < 1.199 || actualProject.Bias > 1.201 {
		t.Fatalf("actual project=%+v", actualProject)
	}
	if forecastOnly.HasActual || !forecastOnly.HasForecast || forecastOnly.ForecastHours != 10 {
		t.Fatalf("forecast-only project=%+v", forecastOnly)
	}

	render := func(biasAllowed bool) string {
		request := httptest.NewRequest(http.MethodGet, "/employees/1?week_end="+week, nil)
		request.SetPathValue("id", strconv.FormatInt(user.ID, 10))
		permissions := map[string]bool{"dashboard.department": true}
		if biasAllowed {
			permissions["dashboard.bias"] = true
		}
		requestContext := context.WithValue(request.Context(), userContextKey, &user)
		requestContext = context.WithValue(requestContext, permissionsContextKey, permissions)
		requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
		request = request.WithContext(requestContext)
		recorder := httptest.NewRecorder()
		app.handleEmployeeDetail(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	withoutBias := render(false)
	if !strings.Contains(withoutBias, "A子项（15600平米混凝土框架结构）设计") || strings.Contains(withoutBias, "偏差与修正") || strings.Contains(withoutBias, "1.20") {
		t.Fatalf("detail without bias permission leaked or missed content: %s", withoutBias)
	}
	withBias := render(true)
	if !strings.Contains(withBias, "偏差与修正") || !strings.Contains(withBias, "1.20") || !strings.Contains(withBias, "仅有预估") {
		t.Fatalf("detail with bias permission missing data: %s", withBias)
	}

	rawTrend := []WeekMetric{{Bias: 1.2, AdjustedHours: 32, HasBias: true, HasAdjusted: true}}
	sanitized := trendWithoutBias(rawTrend)
	if sanitized[0].Bias != 0 || sanitized[0].AdjustedHours != 0 || sanitized[0].HasBias || sanitized[0].HasAdjusted || rawTrend[0].Bias != 1.2 {
		t.Fatalf("sanitized=%+v original=%+v", sanitized[0], rawTrend[0])
	}
}
