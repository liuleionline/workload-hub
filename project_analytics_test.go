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

func TestProjectUsageCorrectionDetailAndPermissionIsolation(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "项目负责人", "project-analytics@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow("SELECT id,department_id,name,email,mobile,qualification,professional_title,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Mobile, &user.Qualification, &user.ProfessionalTitle,
		&user.Role, &user.IsSystemAdmin, &user.Active, &user.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)",
		"ANALYTICS-1", "项目分析测试全名", "项目分析", "中", "总师", user.ID, user.ID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	if _, err = db.Exec("INSERT INTO project_leads(project_id,user_id,is_execution) VALUES (?,?,1)", projectID, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO project_stages(project_id,stage) VALUES (?,?)", projectID, "施工图设计"); err != nil {
		t.Fatal(err)
	}
	content := "A子项（15600平米混凝土框架结构）设计"
	if _, err = db.Exec("INSERT INTO project_participations(project_id,user_id,latest_work_content,status) VALUES (?,?,?,'active')", projectID, user.ID, content); err != nil {
		t.Fatal(err)
	}

	location := time.FixedZone("CST", 8*3600)
	current := time.Date(2026, 4, 10, 0, 0, 0, 0, location)
	for offset := 1; offset <= 5; offset++ {
		week := isoDate(current.AddDate(0, 0, -7*offset))
		if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", week, user.ID, projectID, 50, content); err != nil {
			t.Fatal(err)
		}
		if offset <= 4 {
			if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", week, projectID, user.ID, 40, user.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	currentWeek := isoDate(current)
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", currentWeek, user.ID, projectID, 50, content); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,other_description) VALUES (?,?,?,?,?)", currentWeek, user.ID, nil, 10, "部门协调"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", currentWeek, projectID, user.ID, 40, user.ID); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	cache := map[string]correctionFactorResult{}
	usages, err := app.projectUsagesForWeek(ctx, current, projectID, cache, false)
	if err != nil {
		t.Fatal(err)
	}
	usage := usages[projectID]
	if usage.RawHours != 50 || usage.EffectiveHours < 39.99 || usage.EffectiveHours > 40.01 || usage.ForecastHours != 40 || !usage.HasCorrection || usage.ParticipantCount != 1 {
		t.Fatalf("usage=%+v", usage)
	}
	if usage.WorkShare < .832 || usage.WorkShare > .834 {
		t.Fatalf("work share=%v, want about .833", usage.WorkShare)
	}
	rawUsage := projectUsageWithoutCorrection(usage)
	if rawUsage.EffectiveHours != 50 || rawUsage.HasCorrection {
		t.Fatalf("raw usage=%+v", rawUsage)
	}
	participants, err := app.projectParticipantDetails(ctx, projectID, current, usage, cache, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 1 || participants[0].CurrentEffectiveHours < 39.99 || participants[0].CurrentEffectiveHours > 40.01 || !participants[0].HasCorrection || len(participants[0].Contents) != 1 {
		t.Fatalf("participants=%+v", participants)
	}
	rawParticipants, err := app.projectParticipantDetails(ctx, projectID, current, rawUsage, cache, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if rawParticipants[0].CurrentEffectiveHours != 50 || rawParticipants[0].HasCorrection {
		t.Fatalf("raw participants=%+v", rawParticipants)
	}

	render := func(withBias bool) string {
		request := httptest.NewRequest(http.MethodGet, "/projects/"+strconv.FormatInt(projectID, 10)+"?week_end="+currentWeek, nil)
		request.SetPathValue("id", strconv.FormatInt(projectID, 10))
		permissions := map[string]bool{"projects.view_all": true, "projects.edit_all": true, "dashboard.department": true}
		if withBias {
			permissions["dashboard.bias"] = true
		}
		requestContext := context.WithValue(request.Context(), userContextKey, &user)
		requestContext = context.WithValue(requestContext, permissionsContextKey, permissions)
		requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
		request = request.WithContext(requestContext)
		recorder := httptest.NewRecorder()
		app.handleProjectDetail(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	managerBody := render(true)
	if !strings.Contains(managerBody, "已含偏差修正") || !strings.Contains(managerBody, content) || !strings.Contains(managerBody, "近12周部门资源占用明细") {
		t.Fatalf("manager project detail missing data: %s", managerBody)
	}
	leadBody := render(false)
	if strings.Contains(leadBody, "已含偏差修正") || strings.Contains(leadBody, "已修正") {
		t.Fatalf("project detail leaked correction data without dashboard.bias: %s", leadBody)
	}
	hiddenTrend := projectTrendWithoutCorrection([]WeekMetric{{ActualHours: 50, AdjustedHours: 40, Available: 32, LoadRate: 1.25, HasAdjusted: true}})
	if hiddenTrend[0].AdjustedHours != 50 || hiddenTrend[0].LoadRate != 1.5625 || hiddenTrend[0].HasAdjusted {
		t.Fatalf("hidden trend=%+v", hiddenTrend[0])
	}
}
