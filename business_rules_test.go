package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCalendarAdjustmentAndRollingCorrection(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试管理员", "admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err = db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: Config{Location: time.FixedZone("CST", 8*3600)}, db: db}
	jan2 := time.Date(2026, 1, 2, 0, 0, 0, 0, app.cfg.Location)
	if got := app.availableHours(ctx, userID, jan2); got != 24 {
		t.Fatalf("holiday week capacity=%v, want 24", got)
	}
	if _, err = db.Exec("INSERT INTO leave_records(week_end,user_id,leave_days) VALUES (?,?,?)", isoDate(jan2), userID, .5); err != nil {
		t.Fatal(err)
	}
	if got := app.availableHours(ctx, userID, jan2); got != 20 {
		t.Fatalf("leave-adjusted capacity=%v, want 20", got)
	}
	result, err := db.Exec(`INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES ('T-1','测试项目','测试','中','总师',?,?, '2026-01-01','2026-12-31')`, userID, userID)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	current := time.Date(2026, 3, 13, 0, 0, 0, 0, app.cfg.Location)
	for offset := 5; offset >= 0; offset-- {
		date := isoDate(current.AddDate(0, 0, -7*offset))
		if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", date, userID, projectID, 50, "测试"); err != nil {
			t.Fatal(err)
		}
		if offset > 0 {
			if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", date, projectID, userID, 40, userID); err != nil {
				t.Fatal(err)
			}
		}
	}
	adjusted, ok := app.adjustmentForWeek(ctx, userID, current, 50)
	if !ok || adjusted < 39.99 || adjusted > 40.01 {
		t.Fatalf("adjusted=%v ok=%v, want 40", adjusted, ok)
	}
	if !app.employeeAlert(ctx, userID, current) {
		t.Fatal("expected consecutive overload alert")
	}
}

func TestBiasUsesProjectHoursAndExcludesOtherWork(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "Bias Admin", "bias@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err = db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", "BIAS-1", "Bias Project", "Bias", "中", "Chief", userID, userID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	result, err = db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", "BIAS-2", "Unforecast Project", "Unforecast", "小", "Chief", userID, userID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	unforecastProjectID, _ := result.LastInsertId()
	result, err = db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", "BIAS-3", "Forecast Only Project", "Forecast Only", "小", "Chief", userID, userID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	forecastOnlyProjectID, _ := result.LastInsertId()
	week := "2026-03-13"
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", week, userID, unforecastProjectID, 8, "unforecasted project"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", week, userID, projectID, 24, "project"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", week, userID, nil, 16, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", week, projectID, userID, 20, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", week, forecastOnlyProjectID, userID, 12, userID); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: Config{Location: time.FixedZone("CST", 8*3600)}, db: db}
	actual, forecast := app.projectWeekTotals(ctx, userID, week)
	if actual != 24 || forecast != 20 {
		t.Fatalf("project totals=%v/%v, want 24/20", actual, forecast)
	}
	metric := app.employeeMetric(ctx, User{ID: userID, Name: "Bias Admin", Role: "manager"}, time.Date(2026, 3, 13, 0, 0, 0, 0, app.cfg.Location))
	if !metric.HasBias || metric.Bias < 1.199 || metric.Bias > 1.201 {
		t.Fatalf("bias=%v has=%v, want 1.2", metric.Bias, metric.HasBias)
	}
	if metric.ActualHours != 48 {
		t.Fatalf("load actual=%v, want 48 including other and unforecasted project work", metric.ActualHours)
	}
}
