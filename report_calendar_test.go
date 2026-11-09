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

func TestHolidayReportWindowsAndStableWeekKeys(t *testing.T) {
	app, db, _, _ := newWorklogBugfixApp(t)
	ctx := context.Background()
	// Old deployments may have a 48-hour close setting. The new rule always uses 24h.
	if _, err := db.Exec("UPDATE settings SET value='0' WHERE key='report_close_weekday'"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		week, start string
		required    bool
	}{
		{"2026-11-13", "2026-11-13 12:00", true},
		{"2026-06-19", "2026-06-18 12:00", true},
		{"2026-10-02", "2026-09-30 12:00", true},
		{"2026-02-20", "2026-02-14 12:00", true}, // Saturday make-up work counts.
		{"2027-01-01", "2026-12-31 12:00", true},
		{"2027-02-05", "2027-02-04 12:00", true},
		{"2027-02-12", "2027-02-12 12:00", false}, // Saturday-Friday all holiday.
	} {
		t.Run(test.week, func(t *testing.T) {
			week, _ := parseISODate(test.week, app.cfg.Location)
			window, err := app.reportingWindowForWeek(ctx, week)
			if err != nil || window.Required != test.required || window.Start.Format("2006-01-02 15:04") != test.start || window.End.Sub(window.Start) != 24*time.Hour {
				t.Fatalf("window=%+v err=%v", window, err)
			}
			if window.Open(window.Start) != test.required || window.Open(window.Start.Add(-time.Second)) || window.Open(window.End) {
				t.Fatal("incorrect left-closed/right-open window")
			}
			for _, now := range []time.Time{window.Start, window.Start.Add(4 * time.Hour), window.End.Add(-time.Second)} {
				actual, err := app.reportingWindowAt(ctx, now)
				if err != nil || isoDate(actual.WeekEnd) != test.week || actual.Required != test.required {
					t.Fatalf("week key shifted: now=%v got=%+v err=%v", now, actual, err)
				}
			}
		})
	}
	week, _ := parseISODate("2027-02-19", app.cfg.Location)
	if got := isoDate(app.previousRequiredReportWeek(ctx, week)); got != "2027-02-05" {
		t.Fatalf("carry-forward week=%s", got)
	}
}

func TestHolidayRemindersOnlyAtOpeningPlusFourHours(t *testing.T) {
	app, _, _, _ := newWorklogBugfixApp(t)
	t.Setenv("APP_SMTP_USERNAME", "sender@example.invalid")
	app.cfg.BaseURL = "https://example.invalid"
	state, err := openReminderMailState(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	for _, test := range []struct {
		at, reason string
		wantSend   int
	}{
		{"2026-06-18 11:59:59", "window_closed", 0},
		{"2026-06-18 12:00:00", "reminder_not_due", 0},
		{"2026-06-18 15:59:59", "reminder_not_due", 0},
		{"2026-06-18 16:00:00", "complete", 1},
		{"2026-06-18 16:00:30", "complete", 0}, // same week is deduplicated.
		{"2026-06-18 16:01:00", "reminder_time_passed", 0},
		{"2026-06-19 11:59:59", "reminder_time_passed", 0},
		{"2026-06-19 12:00:00", "window_closed", 0},
		{"2027-02-12 16:00:00", "holiday_week", 0},
	} {
		now, _ := time.ParseInLocation("2006-01-02 15:04:05", test.at, app.cfg.Location)
		sends := 0
		result, err := app.executeMailReminders(context.Background(), state, true, func() time.Time { return now }, func(context.Context, string, []byte) error { sends++; return nil })
		if err != nil || sends != test.wantSend || result.Status != test.reason {
			t.Fatalf("at=%s sends=%d result=%+v err=%v", test.at, sends, result, err)
		}
	}
}

func TestHolidayExemptionAndCalendarChanges(t *testing.T) {
	app, db, user, _ := newWorklogBugfixApp(t)
	ctx := context.Background()
	// Existing data on a holiday week is retained, but no one is marked missing.
	if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours) VALUES ('2027-02-12',?,8)", user.ID); err != nil {
		t.Fatal(err)
	}
	status, err := app.worklogSubmissionStatusForWeek(ctx, "2027-02-12")
	if err != nil || !status.Exempt || len(status.Missing) != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	var hours float64
	if err := db.QueryRow("SELECT hours FROM actual_work_entries WHERE week_end='2027-02-12' AND user_id=?", user.ID).Scan(&hours); err != nil || hours != 8 {
		t.Fatal("historical worklog changed")
	}
	if _, err := db.Exec("UPDATE work_calendar SET work_hours=8,source='admin' WHERE work_date='2027-02-12'"); err != nil {
		t.Fatal(err)
	}
	if err := db.seedOfficialCalendar(); err != nil {
		t.Fatal(err)
	}
	week, _ := parseISODate("2027-02-12", app.cfg.Location)
	w, err := app.reportingWindowForWeek(ctx, week)
	if err != nil || !w.Required || isoDate(w.Start) != "2027-02-12" {
		t.Fatal("admin calendar override lost")
	}
	if _, err := db.Exec("DROP TABLE work_calendar"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.worklogReminderSnapshot(ctx, time.Date(2027, 2, 12, 16, 0, 0, 0, app.cfg.Location)); err == nil {
		t.Fatal("unreadable calendar must fail closed")
	}
}

func TestOverlappingReportWindowsFailClosed(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	week := time.Date(2027, 2, 12, 0, 0, 0, 0, loc)
	calendar := reportingCalendar{}
	// Monday opening for this week shifts into the prior week's Monday window;
	// Friday still has work, so it must not be silently exempted or merged.
	for day := week.AddDate(0, 0, -10); day.Before(week); day = day.AddDate(0, 0, 1) {
		calendar[isoDate(day)] = 0
	}
	_, err := checkedCalendarWindow(week, reportScheduleFromStart(time.Monday, 720), calendar)
	if err == nil {
		t.Fatal("overlapping weeks accepted")
	}
}

func TestHolidayReportingNoticesAndSettings(t *testing.T) {
	app, db, user, _ := newWorklogBugfixApp(t)
	now := time.Now().In(app.cfg.Location)
	week := currentWeekEnd(now)
	for day := week.AddDate(0, 0, -6); !day.After(week); day = day.AddDate(0, 0, 1) {
		if _, err := db.Exec("INSERT INTO work_calendar(work_date,work_hours,label,source) VALUES (?,0,'test holiday','test') ON CONFLICT(work_date) DO UPDATE SET work_hours=0", isoDate(day)); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"report_open_weekday": strconv.Itoa(int(now.Weekday())), "report_open_minute": "0"} {
		if _, err := db.Exec("UPDATE settings SET value=? WHERE key=?", value, key); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/reports", nil)
	ctx := context.WithValue(r.Context(), userContextKey, &user)
	ctx = context.WithValue(ctx, permissionsContextKey, map[string]bool{"worklog.submit": true})
	w := httptest.NewRecorder()
	app.render(w, r.WithContext(ctx), http.StatusOK, "reports.html", PageData{Data: ReportsData{}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "本统计周无需填报") || strings.Contains(w.Body.String(), "未填报：") || strings.Contains(w.Body.String(), "全部已填报") {
		t.Fatalf("wrong exempt banner: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	app.handleSettings(w, r.WithContext(ctx))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "24小时（固定）") || !strings.Contains(w.Body.String(), "开启后4小时") || strings.Contains(w.Body.String(), `name="report_close_weekday"`) {
		t.Fatal("settings still permit conflicting close time")
	}
}
