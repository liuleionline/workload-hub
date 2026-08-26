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

func TestWorklogReusesLatestProjectContentAndRequiresItOnlyFirstTime(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试员工", "work-content@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Role, &user.IsSystemAdmin, &user.Active, &user.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	createProject := func(code, name string) int64 {
		result, createErr := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", code, name, name, "中", "总师", user.ID, user.ID, "2026-01-01", "2026-12-31")
		if createErr != nil {
			t.Fatal(createErr)
		}
		id, _ := result.LastInsertId()
		return id
	}
	knownProjectID := createProject("CONTENT-1", "已有内容项目")
	newProjectID := createProject("CONTENT-2", "首次参与项目")
	savedContent := "A子项（15600平米混凝土框架结构）设计"
	if _, err = db.Exec("INSERT INTO project_participations(project_id,user_id,latest_work_content,status) VALUES (?,?,?,'active')", knownProjectID, user.ID, savedContent); err != nil {
		t.Fatal(err)
	}

	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	post := func(projectID int64) *httptest.ResponseRecorder {
		values := url.Values{
			"leave_days":          {"0"},
			"entry_type[]":        {"project"},
			"project_id[]":        {strconv.FormatInt(projectID, 10)},
			"hours[]":             {"8"},
			"work_content[]":      {""},
			"other_description[]": {""},
			"end_participation[]": {"0"},
		}
		request := httptest.NewRequest(http.MethodPost, "/worklog", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		requestContext := context.WithValue(request.Context(), userContextKey, &user)
		requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{"worklog.submit": true})
		requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
		request = request.WithContext(requestContext)
		recorder := httptest.NewRecorder()
		app.handleWorklogSave(recorder, request)
		return recorder
	}

	recorder := post(knownProjectID)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("existing project blank content status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var stored string
	if err = db.QueryRow("SELECT work_content FROM actual_work_entries WHERE user_id=? AND project_id=? ORDER BY id DESC LIMIT 1", user.ID, knownProjectID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != savedContent {
		t.Fatalf("stored content=%q, want %q", stored, savedContent)
	}

	recorder = post(newProjectID)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "第一次填写参与某项目时") {
		t.Fatalf("first project blank content status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
