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

func setupProjectFeatureTest(t *testing.T) (*DB, *App, User, User) {
	t.Helper()
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "系统管理员", "admin@project.test", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var admin User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&admin.ID, &admin.DepartmentID, &admin.Name, &admin.Email, &admin.Role, &admin.IsSystemAdmin, &admin.Active, &admin.MustChangePassword); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role,must_change_password) VALUES (?,?,?,?,?,0)",
		admin.DepartmentID, "项目创建人", "lead@project.test", "unused", "lead")
	if err != nil {
		t.Fatal(err)
	}
	leadID, _ := result.LastInsertId()
	lead := User{ID: leadID, DepartmentID: admin.DepartmentID, Name: "项目创建人", Email: "lead@project.test", Role: "lead", Active: true}
	app, err := NewApp(Config{Location: time.FixedZone("CST", 8*3600)}, db)
	if err != nil {
		t.Fatal(err)
	}
	return db, app, admin, lead
}

func insertFeatureProject(t *testing.T, db *DB, creatorID int64, code string) int64 {
	t.Helper()
	result, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date,"+
		"intro_address,intro_type,intro_scale,intro_components) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
		code, code+"项目", code, "大", "总设计师", creatorID, creatorID, "2026-01-01", "2026-12-31", "重庆", "工业", "10000平方米", "101厂房")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}

func projectRequest(method, target string, user *User, id int64) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.SetPathValue("id", strconv.FormatInt(id, 10))
	ctx := context.WithValue(request.Context(), userContextKey, user)
	ctx = context.WithValue(ctx, permissionsContextKey, map[string]bool{})
	return request.WithContext(ctx)
}

func TestProjectDeletePermissionAndUsageGuard(t *testing.T) {
	db, app, admin, lead := setupProjectFeatureTest(t)
	defer db.Close()

	unusedID := insertFeatureProject(t, db, lead.ID, "DELETE-UNUSED")
	recorder := httptest.NewRecorder()
	app.handleProjectDelete(recorder, projectRequest(http.MethodPost, "/projects/1/delete", &lead, unusedID))
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("creator delete status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", unusedID).Scan(&count)
	if count != 0 {
		t.Fatal("unused project should be deleted")
	}

	usedID := insertFeatureProject(t, db, lead.ID, "DELETE-USED")
	if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)",
		"2026-08-14", lead.ID, usedID, 8, "101厂房设计"); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	app.handleProjectDelete(recorder, projectRequest(http.MethodPost, "/projects/2/delete", &lead, usedID))
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("used project delete status=%d", recorder.Code)
	}
	_ = db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", usedID).Scan(&count)
	if count != 1 {
		t.Fatal("project with historical work must remain")
	}

	otherLead := lead
	otherLead.ID = lead.ID + 1000
	deniedID := insertFeatureProject(t, db, lead.ID, "DELETE-DENIED")
	recorder = httptest.NewRecorder()
	app.handleProjectDelete(recorder, projectRequest(http.MethodPost, "/projects/denied/delete", &otherLead, deniedID))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-creator delete status=%d, want 403", recorder.Code)
	}

	adminDeleteID := insertFeatureProject(t, db, lead.ID, "DELETE-ADMIN")
	recorder = httptest.NewRecorder()
	app.handleProjectDelete(recorder, projectRequest(http.MethodPost, "/projects/3/delete", &admin, adminDeleteID))
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("admin delete status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProjectSubitemsStayAdditiveAndWorklogSnapshotRemains(t *testing.T) {
	db, app, _, lead := setupProjectFeatureTest(t)
	defer db.Close()
	projectID := insertFeatureProject(t, db, lead.ID, "SUBITEM-1")

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	items := []ProjectSubitem{
		{Name: "101生产厂房", Area: 15600, Structure: "钢筋混凝土框架结构", Notes: "一期"},
		{Name: "102动力站", Area: 3200, Structure: "钢结构"},
	}
	if err = app.saveProjectSubitems(context.Background(), tx, projectID, items); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	saved, err := app.projectSubitems(context.Background(), projectID, true)
	if err != nil || len(saved) != 2 {
		t.Fatalf("saved subitems=%v err=%v", saved, err)
	}
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,project_subitem_id,hours,work_content,work_subitem,work_area,work_structure,work_role) "+
		"VALUES (?,?,?,?,?,?,?,?,?,?)", "2026-08-14", lead.ID, projectID, saved[0].ID, 12, "101生产厂房设计", saved[0].Name, saved[0].Area, saved[0].Structure, "独立设计"); err != nil {
		t.Fatal(err)
	}
	tx, _ = db.Begin()
	if err = app.saveProjectSubitems(context.Background(), tx, projectID, []ProjectSubitem{
		{ID: saved[0].ID, Name: "101生产厂房（调整）", Area: 16000, Structure: "钢筋混凝土框架结构", Notes: "更新"},
	}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	active, _ := app.projectSubitems(context.Background(), projectID, true)
	if len(active) != 1 || active[0].Name != "101生产厂房（调整）" {
		t.Fatalf("active subitems=%v", active)
	}
	var snapshot string
	if err = db.QueryRow("SELECT work_subitem FROM actual_work_entries WHERE project_id=?", projectID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot != "101生产厂房" {
		t.Fatalf("historical snapshot changed to %q", snapshot)
	}
}

func TestSubmittedWorklogRendersSavedEntriesForEditing(t *testing.T) {
	db, app, admin, _ := setupProjectFeatureTest(t)
	defer db.Close()
	now := time.Now().In(app.cfg.Location)
	weekEnd := isoDate(app.worklogWeekEnd(context.Background(), now))
	if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours,other_description) VALUES (?,?,?,?)", weekEnd, admin.ID, 6.5, "已保存的部门协调"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/worklog", nil)
	ctx := context.WithValue(request.Context(), userContextKey, &admin)
	ctx = context.WithValue(ctx, permissionsContextKey, map[string]bool{"worklog.submit": true})
	ctx = context.WithValue(ctx, csrfContextKey, "test-csrf")
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()
	app.handleWorklogPage(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("worklog page status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{"本周已提交", "已保存的部门协调", "6.5", "你可以继续修改、删除或新增"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("submitted worklog page missing %q", expected)
		}
	}
}

func TestConfigurableReportSchedule(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	schedule := ReportSchedule{OpenWeekday: time.Thursday, OpenMinute: 15 * 60, CloseWeekday: time.Friday, CloseMinute: 18 * 60}
	if !schedule.Valid() {
		t.Fatal("Thursday to Friday schedule should be valid")
	}
	if !reportWindowOpenWithSchedule(time.Date(2026, 8, 13, 15, 0, 0, 0, loc), schedule) {
		t.Fatal("custom window should open at configured time")
	}
	if reportWindowOpenWithSchedule(time.Date(2026, 8, 14, 18, 0, 0, 0, loc), schedule) {
		t.Fatal("custom window should close at configured time")
	}
}
