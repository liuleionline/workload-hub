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

func newWorklogBugfixApp(t *testing.T) (*App, *DB, User, time.Time) {
	t.Helper()
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试管理员", "worklog-bugfix@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,is_test_user,active,must_change_password FROM users LIMIT 1").Scan(
		&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Role, &user.IsSystemAdmin, &user.IsTestUser, &user.Active, &user.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	weekEnd := app.worklogWeekEnd(ctx, time.Now().In(location))
	for day := weekEnd.AddDate(0, 0, -6); !day.After(weekEnd); day = day.AddDate(0, 0, 1) {
		hours := 0.0
		if day.Weekday() >= time.Monday && day.Weekday() <= time.Friday {
			hours = 8
		}
		if _, err = db.Exec("INSERT INTO work_calendar(work_date,work_hours) VALUES (?,?) ON CONFLICT(work_date) DO UPDATE SET work_hours=excluded.work_hours", isoDate(day), hours); err != nil {
			t.Fatal(err)
		}
	}
	return app, db, user, weekEnd
}

func postWorklogForTest(app *App, user *User, values url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/worklog", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, user))
	recorder := httptest.NewRecorder()
	app.handleWorklogSave(recorder, request)
	return recorder
}

func TestFullWeekLeaveCanSubmitWithoutWorkEntries(t *testing.T) {
	app, db, user, weekEnd := newWorklogBugfixApp(t)
	recorder := postWorklogForTest(app, &user, url.Values{
		"leave_days":        {"5"},
		"work_entries_json": {"[]"},
	})
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("full-week leave status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var leave float64
	if err := db.QueryRow("SELECT leave_days FROM leave_records WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID).Scan(&leave); err != nil {
		t.Fatal(err)
	}
	if leave != 5 {
		t.Fatalf("leave=%v, want 5", leave)
	}
	var entries int
	if err := db.QueryRow("SELECT COUNT(*) FROM actual_work_entries WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("actual entries=%d, want 0", entries)
	}

	request := httptest.NewRequest(http.MethodGet, "/worklog", nil)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, &user))
	page := httptest.NewRecorder()
	app.handleWorklogPage(page, request)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "本周已提交") || strings.Contains(body, "已带入上周内容") {
		t.Fatalf("leave-only page status=%d body=%s", page.Code, body)
	}
}

func TestZeroHoursCanBeSubmittedWithAnyLeaveDays(t *testing.T) {
	for _, leaveDays := range []string{"0", "4"} {
		t.Run("leave_"+leaveDays, func(t *testing.T) {
			app, db, user, weekEnd := newWorklogBugfixApp(t)
			recorder := postWorklogForTest(app, &user, url.Values{
				"leave_days":        {leaveDays},
				"work_entries_json": {"[]"},
			})
			if recorder.Code != http.StatusSeeOther {
				t.Fatalf("zero-hour status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			wantLeave := 0.0
			if leaveDays == "4" {
				wantLeave = 4
			}
			var savedLeave float64
			if err := db.QueryRow("SELECT leave_days FROM leave_records WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID).Scan(&savedLeave); err != nil {
				t.Fatal(err)
			}
			if savedLeave != wantLeave {
				t.Fatalf("saved leave=%v, want %v", savedLeave, wantLeave)
			}
		})
	}
}

func TestVisibleWorkDetailsOverrideCorruptedJSONFields(t *testing.T) {
	values := url.Values{
		"work_entries_json":    {`[{"entry_type":"project","project_id":"3","project_subitem_id":"3","hours":"50","work_subitem":"3","work_area":"3","work_structure":"3","work_role":"3"}]`},
		"entry_type[]":         {"project"},
		"work_category[]":      {"regular"},
		"project_choice[]":     {"3"},
		"project_id[]":         {""},
		"project_subitem_id[]": {""},
		"hours[]":              {"50"},
		"work_subitem[]":       {"倒班宿舍1、倒班宿舍2、全厂装配式"},
		"work_area[]":          {"33000"},
		"work_structure[]":     {"混凝土框剪结构"},
		"work_role[]":          {"独立设计"},
		"other_description[]":  {""},
		"end_participation[]":  {"0"},
	}
	request := httptest.NewRequest(http.MethodPost, "/worklog", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	entries, err := submittedWorklogEntries(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.WorkSubitem != "倒班宿舍1、倒班宿舍2、全厂装配式" || entry.WorkArea != "33000" || entry.WorkStructure != "混凝土框剪结构" || entry.WorkRole != "独立设计" {
		t.Fatalf("visible details were not preserved: %+v", entry)
	}
}
