package main

import (
	"context"
	"testing"
	"time"
)

func TestWorklogSubmissionStatusTracksReportingWeekAndZeroHours(t *testing.T) {
	app, db, admin, _ := newWorklogBugfixApp(t)
	addUser := func(name string, active, testUser bool) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO users(department_id,name,email,password_hash,role,active,is_test_user)
			VALUES (?,?,?,'unused','designer',?,?)`, admin.DepartmentID, name, name+"@test.local", active, testUser)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	zero := addUser("zero", true, false)
	leave := addUser("leave", true, false)
	actual := addUser("actual", true, false)
	previous := addUser("previous", true, false)
	addUser("test-account", true, true)
	addUser("inactive", false, false)
	for _, item := range []struct {
		id   int64
		days int
	}{{zero, 0}, {leave, 5}} {
		if _, err := db.Exec("INSERT INTO leave_records(week_end,user_id,leave_days) VALUES ('2026-11-06',?,?)", item.id, item.days); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		id   int64
		week string
	}{{actual, "2026-11-06"}, {previous, "2026-10-30"}} {
		if _, err := db.Exec("INSERT INTO actual_work_entries(week_end,user_id,hours) VALUES (?,?,8)", item.week, item.id); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		at, week  string
		submitted int
	}{
		{"2026-11-06 13:00", "2026-11-06", 3},
		{"2026-11-07 11:00", "2026-11-06", 3},
		{"2026-11-07 13:00", "2026-11-06", 3},
		{"2026-11-09 09:00", "2026-11-06", 3},
		{"2026-11-13 13:00", "2026-11-13", 0},
	} {
		now, err := time.ParseInLocation("2006-01-02 15:04", item.at, app.cfg.Location)
		if err != nil {
			t.Fatal(err)
		}
		status, err := app.worklogSubmissionStatus(context.Background(), now)
		if err != nil {
			t.Fatal(err)
		}
		if status.WeekEnd != item.week || status.Total != 5 || status.Submitted != item.submitted || len(status.Missing) != 5-item.submitted {
			t.Fatalf("at=%s status=%+v", item.at, status)
		}
		if item.submitted == 3 {
			missing := map[int64]bool{}
			for _, user := range status.Missing {
				missing[user.ID] = true
			}
			if !missing[admin.ID] || !missing[previous] {
				t.Fatalf("wrong missing users at %s: %+v", item.at, status.Missing)
			}
		}
	}
}
