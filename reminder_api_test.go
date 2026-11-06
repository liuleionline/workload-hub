package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReminderAPIAuthorizationAndIsolation(t *testing.T) {
	app, db, user, _ := newWorklogBugfixApp(t)
	const token = "test-only-service-token-not-a-real-credential"
	app.cfg.ReminderTokenHash = hashToken(token)
	now := time.Now().In(app.cfg.Location)
	for key, value := range map[string]int{"report_open_weekday": int(now.Weekday()), "report_open_minute": 0,
		"report_close_weekday": (int(now.Weekday()) + 1) % 7, "report_close_minute": 0} {
		if _, err := db.Exec("UPDATE settings SET value=? WHERE key=?", strconv.Itoa(value), key); err != nil {
			t.Fatal(err)
		}
	}
	handler := app.Handler()
	for _, test := range []struct {
		name, method, path, auth string
		want                     int
	}{
		{"missing", "GET", reminderAPIPath, "", 401},
		{"wrong", "GET", reminderAPIPath, "Bearer wrong", 401},
		{"wrong scheme", "GET", reminderAPIPath, "Basic " + token, 401},
		{"query not accepted", "GET", reminderAPIPath + "?token=" + token, "", 401},
		{"valid", "GET", reminderAPIPath, "Bearer " + token, 200},
		{"read only", "POST", reminderAPIPath, "Bearer " + token, 405},
		{"no admin access", "GET", "/admin/users", "Bearer " + token, 303},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, test.path, nil)
			r.Header.Set("Authorization", test.auth)
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "invalid-browser-session"})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.HasPrefix(test.path, reminderAPIPath) {
				if len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "private, no-store" || !json.Valid(w.Body.Bytes()) {
					t.Fatalf("API must be cookie-independent, uncached JSON: %v", w.Header())
				}
				if w.Code == 200 {
					var result map[string]json.RawMessage
					_ = json.Unmarshal(w.Body.Bytes(), &result)
					var recipients []map[string]any
					_ = json.Unmarshal(result["missing"], &recipients)
					if len(recipients) != 1 || len(recipients[0]) != 2 || recipients[0]["name"] != user.Name || recipients[0]["user_id"] != float64(user.ID) {
						t.Fatalf("unexpected recipient fields: %v", recipients)
					}
				} else if strings.Contains(w.Body.String(), user.Name) {
					t.Fatal("error response leaked user data")
				}
			}
		})
	}
	for _, hash := range []string{"", "malformed", hashToken("replacement-token")} {
		app.cfg.ReminderTokenHash = hash
		r := httptest.NewRequest("GET", reminderAPIPath, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 503 && w.Code != 401 {
			t.Fatalf("disabled/rotated credential accepted: %d", w.Code)
		}
	}
}

func TestReminderSnapshotUsesReportingWindowAndSharedSubmissionRules(t *testing.T) {
	app, db, admin, _ := newWorklogBugfixApp(t)
	ctx := context.Background()
	addUser := func(name string, active, testUser bool) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO users(department_id,name,email,password_hash,role,active,is_test_user)
			VALUES (?,?,?,'unused','designer',?,?)`, admin.DepartmentID, name, name+"@example.invalid", active, testUser)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		return id
	}
	zero, leave, actual := addUser("zero", true, false), addUser("leave", true, false), addUser("actual", true, false)
	previous := addUser("previous", true, false)
	addUser("disabled", false, false)
	addUser("test-user", true, true)
	for _, row := range []struct {
		id   int64
		days int
	}{{zero, 0}, {leave, 5}} {
		if _, err := db.Exec("INSERT INTO leave_records(week_end,user_id,leave_days) VALUES ('2026-11-06',?,?)", row.id, row.days); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id   int64
		week string
	}{{actual, "2026-11-06"}, {previous, "2026-10-30"}} {
		if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours) VALUES (?,?,8)", row.week, row.id); err != nil {
			t.Fatal(err)
		}
	}
	at := func(value string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", value, app.cfg.Location)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, test := range []struct {
		at   string
		open bool
	}{
		{"2026-11-06 11:59", false}, {"2026-11-06 12:00", true}, {"2026-11-06 16:00", true},
		{"2026-11-07 11:59", true}, {"2026-11-07 12:00", false}, {"2026-11-09 16:00", false},
	} {
		result, err := app.worklogReminderSnapshot(ctx, at(test.at))
		if err != nil {
			t.Fatal(err)
		}
		if result.WindowOpen != test.open || result.ShouldRemind != test.open {
			t.Fatalf("at=%s result=%+v", test.at, result)
		}
		if !test.open {
			if len(result.Missing) != 0 || result.Counts != nil || result.Reason != "window_closed" {
				t.Fatalf("closed window leaked recipients: %+v", result)
			}
			continue
		}
		status, err := app.worklogSubmissionStatus(ctx, at(test.at))
		if err != nil {
			t.Fatal(err)
		}
		if result.WeekEnd != status.WeekEnd || result.Counts.Eligible != 5 || result.Counts.Submitted != 3 || result.Counts.Missing != len(status.Missing) {
			t.Fatalf("website/API mismatch: %+v %+v", result, status)
		}
		for i, recipient := range result.Missing {
			if recipient.UserID != status.Missing[i].ID || recipient.Name != status.Missing[i].Name {
				t.Fatal("website/API recipient mismatch")
			}
		}
	}
	for _, id := range []int64{admin.ID, previous} {
		if _, err := db.Exec("INSERT INTO leave_records(week_end,user_id,leave_days) VALUES ('2026-11-06',?,0)", id); err != nil {
			t.Fatal(err)
		}
	}
	result, err := app.worklogReminderSnapshot(ctx, at("2026-11-06 16:00"))
	if err != nil || result.ShouldRemind || result.Reason != "all_submitted" || len(result.Missing) != 0 {
		t.Fatalf("all submitted=%+v err=%v", result, err)
	}
	if _, err = db.Exec("UPDATE settings SET value='1020' WHERE key='report_open_minute'"); err != nil {
		t.Fatal(err)
	}
	result, err = app.worklogReminderSnapshot(ctx, at("2026-11-06 16:00"))
	if err != nil || result.WindowOpen || result.ShouldRemind {
		t.Fatalf("custom window ignored: %+v %v", result, err)
	}
	if _, err = db.Exec("DELETE FROM settings WHERE key='report_open_minute'"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.worklogReminderSnapshot(ctx, at("2026-11-06 16:00")); err == nil {
		t.Fatal("missing settings must fail closed")
	}
	app.cfg.ReminderTokenHash = hashToken("test-only-token")
	r := httptest.NewRequest("GET", reminderAPIPath, nil)
	r.Header.Set("Authorization", "Bearer test-only-token")
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), admin.Name) {
		t.Fatalf("invalid settings: %d %s", w.Code, w.Body.String())
	}
}
