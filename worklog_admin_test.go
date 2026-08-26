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

func TestSystemAdminCanClearOwnCurrentWorklog(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试管理员", "worklog-admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var admin User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&admin.ID, &admin.DepartmentID, &admin.Name, &admin.Email, &admin.Role, &admin.IsSystemAdmin, &admin.Active, &admin.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CST", 8*3600)
	weekEnd := isoDate(worklogWeekEnd(time.Now().In(location)))
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours,other_description) VALUES (?,?,?,?)", weekEnd, admin.ID, 8, "测试工时"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO leave_records(week_end,user_id,leave_days) VALUES (?,?,?)", weekEnd, admin.ID, 0.5); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"action": {"clear"}}
	request := httptest.NewRequest(http.MethodPost, "/worklog", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	requestContext := context.WithValue(request.Context(), userContextKey, &admin)
	request = request.WithContext(requestContext)
	recorder := httptest.NewRecorder()
	app.handleWorklogSave(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("clear status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var entries, leaves, audits int
	_ = db.QueryRow("SELECT COUNT(*) FROM actual_work_entries WHERE week_end=? AND user_id=?", weekEnd, admin.ID).Scan(&entries)
	_ = db.QueryRow("SELECT COUNT(*) FROM leave_records WHERE week_end=? AND user_id=?", weekEnd, admin.ID).Scan(&leaves)
	_ = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action='worklog_clear' AND actor_user_id=?", admin.ID).Scan(&audits)
	if entries != 0 || leaves != 0 || audits != 1 {
		t.Fatalf("after clear entries=%d leaves=%d audits=%d", entries, leaves, audits)
	}
}
