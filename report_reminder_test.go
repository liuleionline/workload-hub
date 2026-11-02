package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReportReminderReflectsSavedWorklog(t *testing.T) {
	app, db, user, _ := newWorklogBugfixApp(t)
	now := time.Now().In(app.cfg.Location)
	setWindow := func(openDay time.Weekday) {
		t.Helper()
		for key, value := range map[string]int{
			"report_open_weekday": int(openDay), "report_open_minute": 0,
			"report_close_weekday": (int(openDay) + 1) % 7, "report_close_minute": 0,
		} {
			if _, err := db.Exec("UPDATE settings SET value=? WHERE key=?", strconv.Itoa(value), key); err != nil {
				t.Fatal(err)
			}
		}
	}
	setWindow(now.Weekday())
	weekEnd := app.worklogWeekEnd(context.Background(), now)
	check := func(viewer User, wantSubmitted, wantReminder bool) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/reports", nil)
		ctx := context.WithValue(r.Context(), userContextKey, &viewer)
		ctx = context.WithValue(ctx, permissionsContextKey, map[string]bool{"worklog.submit": true})
		w := httptest.NewRecorder()
		app.render(w, r.WithContext(ctx), http.StatusOK, "reports.html", PageData{Data: ReportsData{}})
		body := w.Body.String()
		if strings.Contains(body, "data-report-status") != viewer.IsSystemAdmin {
			t.Fatalf("administrator submission list visibility is incorrect for role=%s admin=%v", viewer.Role, viewer.IsSystemAdmin)
		}
		if w.Code != http.StatusOK || strings.Contains(body, `<strong>已填报</strong>`) != (wantReminder && wantSubmitted) ||
			strings.Contains(body, `href="/worklog">修改工时</a>`) != (wantReminder && wantSubmitted) ||
			strings.Contains(body, `href="/worklog">立即填写</a>`) != (wantReminder && !wantSubmitted) {
			t.Fatalf("role=%s submitted=%v reminder=%v status=%d body=%s", viewer.Role, wantSubmitted, wantReminder, w.Code, body)
		}
	}
	if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours,other_description) VALUES (?,?,?,?)",
		isoDate(weekEnd.AddDate(0, 0, -7)), user.ID, 8, "上周工作"); err != nil {
		t.Fatal(err)
	}
	check(user, false, true)
	// A successful zero-hour submission is still a submission, for every role.
	response := postWorklogForTest(app, &user, url.Values{"leave_days": {"0"}, "work_entries_json": {"[]"}})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("save status=%d body=%s", response.Code, response.Body.String())
	}
	for _, role := range []string{"designer", "lead", "manager"} {
		viewer := user
		viewer.Role, viewer.IsSystemAdmin = role, false
		check(viewer, true, true)
	}
	other := user
	other.ID++
	check(other, false, true)
	if _, err := db.Exec("DELETE FROM leave_records WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID); err != nil {
		t.Fatal(err)
	}
	check(user, false, true)
	// Older submissions with actual entries but no leave row are also recognized.
	if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours,other_description) VALUES (?,?,?,?)",
		isoDate(weekEnd), user.ID, 8, "本周工作"); err != nil {
		t.Fatal(err)
	}
	check(user, true, true)
	setWindow((now.Weekday() + 1) % 7)
	check(user, true, false)
}
