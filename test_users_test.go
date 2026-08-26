package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTestUserCreationPasswordAndStatisticsIsolation(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "系统管理员", "admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var admin User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users WHERE email='admin@test.local'").Scan(
		&admin.ID, &admin.DepartmentID, &admin.Name, &admin.Email, &admin.Role, &admin.IsSystemAdmin, &admin.Active, &admin.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{Location: time.FixedZone("CST", 8*3600)}, db)
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"name": {"测试负责人"}, "email": {"test-lead@test.local"}, "role": {"lead"},
		"initial_password": {"123456"}, "is_test_user": {"1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, &admin))
	recorder := httptest.NewRecorder()
	app.handleUserCreate(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("create test user status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var testLead User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,is_test_user,active,must_change_password FROM users WHERE email='test-lead@test.local'").Scan(
		&testLead.ID, &testLead.DepartmentID, &testLead.Name, &testLead.Email, &testLead.Role, &testLead.IsSystemAdmin, &testLead.IsTestUser, &testLead.Active, &testLead.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	if !testLead.IsTestUser || testLead.IsSystemAdmin || testLead.MustChangePassword || testLead.Role != "lead" {
		t.Fatalf("unexpected test user flags: %+v", testLead)
	}
	permissions, err := app.effectivePermissions(ctx, testLead)
	if err != nil || !permissions["projects.create"] || !permissions["forecast.manage_own"] || !permissions["worklog.submit"] {
		t.Fatalf("test lead did not inherit lead permissions: err=%v permissions=%v", err, permissions)
	}

	formalResult, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role,must_change_password) VALUES (?,?,?,?,?,0)", admin.DepartmentID, "正式设计师", "formal@test.local", "unused", "designer")
	if err != nil {
		t.Fatal(err)
	}
	formalID, _ := formalResult.LastInsertId()
	formalProjectResult, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?)", "FORMAL-1", "正式项目", "正式项目", "中", "总师", admin.ID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	formalProjectID, _ := formalProjectResult.LastInsertId()
	testProjectResult, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?)", "TEST-1", "测试项目", "测试项目", "中", "总师", testLead.ID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	testProjectID, _ := testProjectResult.LastInsertId()
	weekEnd := currentWeekEnd(time.Now().In(app.cfg.Location))
	week := isoDate(weekEnd)
	for _, entry := range []struct {
		userID, projectID int64
		hours             float64
	}{{formalID, formalProjectID, 10}, {testLead.ID, formalProjectID, 20}, {formalID, testProjectID, 30}} {
		if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", week, entry.userID, entry.projectID, entry.hours, "测试内容"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", week, formalProjectID, formalID, 8, testLead.ID); err != nil {
		t.Fatal(err)
	}

	statUsers, err := app.listStatUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range statUsers {
		if user.ID == testLead.ID {
			t.Fatal("test user appeared in department employee statistics")
		}
	}
	officialUsage, err := app.projectUsagesForWeek(ctx, weekEnd, 0, map[string]correctionFactorResult{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := officialUsage[formalProjectID]; got.RawHours != 10 || got.ForecastHours != 0 {
		t.Fatalf("official formal project usage=%+v, want raw=10 forecast=0", got)
	}
	if _, exists := officialUsage[testProjectID]; exists {
		t.Fatalf("test project appeared in official usage: %+v", officialUsage[testProjectID])
	}
	testUsage, err := app.projectUsagesForWeek(ctx, weekEnd, 0, map[string]correctionFactorResult{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if testUsage[formalProjectID].RawHours != 30 || testUsage[formalProjectID].ForecastHours != 8 || testUsage[testProjectID].RawHours != 30 {
		t.Fatalf("test view did not include test data: %+v", testUsage)
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/reports?year="+week[:4], nil)
	adminRequest = adminRequest.WithContext(context.WithValue(adminRequest.Context(), userContextKey, &admin))
	adminReport, err := app.reportData(adminRequest)
	if err != nil || adminReport.TotalHours != 10 {
		t.Fatalf("official report total=%v err=%v, want 10", adminReport.TotalHours, err)
	}
	testRequest := httptest.NewRequest(http.MethodGet, "/reports?year="+week[:4], nil)
	testRequest = testRequest.WithContext(context.WithValue(testRequest.Context(), userContextKey, &testLead))
	testReport, err := app.reportData(testRequest)
	if err != nil || testReport.TotalHours != 20 {
		t.Fatalf("test user's personal report total=%v err=%v, want 20", testReport.TotalHours, err)
	}
}

func TestTestUserBypassesForcedPasswordChange(t *testing.T) {
	app := &App{}
	user := &User{ID: 1, Role: "designer", IsTestUser: true, MustChangePassword: true, Active: true}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, user))
	recorder := httptest.NewRecorder()
	app.withAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("test user was redirected to forced password change: status=%d location=%s", recorder.Code, recorder.Header().Get("Location"))
	}
}
