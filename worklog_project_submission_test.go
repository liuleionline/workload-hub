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

func TestWorklogAcceptsVisibleProjectChoiceAndPreservesInvalidSubmission(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试管理员", "project-choice@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Role, &user.IsSystemAdmin, &user.Active, &user.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		code := "CHOICE-" + strconv.Itoa(i)
		name := "项目" + strconv.Itoa(i)
		if i == 3 {
			code = "PRJ-2026-01B-022"
			name = "210厂房"
		}
		if _, err = db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", code, name, name, "大", "总师", user.ID, user.ID, "2026-03-01", "2026-09-30"); err != nil {
			t.Fatal(err)
		}
	}
	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	post := func(values url.Values) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/worklog", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request = request.WithContext(context.WithValue(request.Context(), userContextKey, &user))
		recorder := httptest.NewRecorder()
		app.handleWorklogSave(recorder, request)
		return recorder
	}
	valid := url.Values{
		"leave_days":          {"0"},
		"entry_type[]":        {"project"},
		"project_id[]":        {""},
		"project_choice[]":    {"3"},
		"hours[]":             {"8"},
		"work_subitem[]":      {"210厂房子项"},
		"work_area[]":         {"49796"},
		"work_structure[]":    {"钢筋混凝土结构"},
		"work_role[]":         {"独立设计"},
		"other_description[]": {""},
		"end_participation[]": {"0"},
	}
	recorder := post(valid)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("visible project choice status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var saved int
	if err = db.QueryRow("SELECT COUNT(*) FROM actual_work_entries WHERE project_id=3").Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != 1 {
		t.Fatalf("saved project 3 entries=%d, want 1", saved)
	}
	structured := valid
	structured["project_id[]"] = []string{""}
	structured["project_choice[]"] = []string{""}
	structured["work_entries_json"] = []string{`[{"entry_type":"project","project_id":"","project_code":"PRJ-2026-01B-022","hours":"1","work_subitem":"210厂房子项","work_area":"49796","work_structure":"钢筋混凝土结构","work_role":"独立设计","other_description":"","end_participation":false}]`}
	recorder = post(structured)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("structured project submission status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	invalid := valid
	delete(invalid, "work_entries_json")
	invalid["project_choice[]"] = []string{""}
	invalid["project_id[]"] = []string{""}
	recorder = post(invalid)
	body := recorder.Body.String()
	if recorder.Code != http.StatusBadRequest || !strings.Contains(body, "第1项工作未选择有效项目") {
		t.Fatalf("invalid project status=%d body=%s", recorder.Code, body)
	}
	if !strings.Contains(body, "210厂房子项") || !strings.Contains(body, "value=\"8\"") || !strings.Contains(body, "value=\"project\" selected") {
		t.Fatalf("invalid submission was not preserved: %s", body)
	}
}
