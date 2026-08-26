package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCorrectionRequiresFiveConsecutiveSubmissionsAndFourImmediateValidBiases(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试部门领导", "bias-rule@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err = db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", "CORR-1", "修正测试项目", "修正测试", "中", "总师", userID, userID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	location := time.FixedZone("CST", 8*3600)
	current := time.Date(2026, 4, 10, 0, 0, 0, 0, location)
	for offset := 1; offset <= 5; offset++ {
		week := isoDate(current.AddDate(0, 0, -7*offset))
		if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", week, userID, projectID, 50, "测试"); err != nil {
			t.Fatal(err)
		}
		if offset <= 4 {
			if _, err = db.Exec("INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)", week, projectID, userID, 40, userID); err != nil {
				t.Fatal(err)
			}
		}
	}
	app := &App{cfg: Config{Location: location}, db: db}
	factor, ok := app.correctionFactorForWeek(ctx, userID, current)
	if !ok || factor < 1.249 || factor > 1.251 {
		t.Fatalf("factor=%v ok=%v, want 1.25", factor, ok)
	}

	gapWeek := isoDate(current.AddDate(0, 0, -35))
	if _, err = db.Exec("DELETE FROM actual_work_entries WHERE user_id=? AND week_end=?", userID, gapWeek); err != nil {
		t.Fatal(err)
	}
	if _, ok = app.correctionFactorForWeek(ctx, userID, current); ok {
		t.Fatal("a gap in the immediately preceding five submissions must disable correction")
	}
	if _, err = db.Exec("INSERT INTO actual_work_entries(week_end,user_id,project_id,hours,work_content) VALUES (?,?,?,?,?)", gapWeek, userID, projectID, 50, "测试"); err != nil {
		t.Fatal(err)
	}

	invalidBiasWeek := isoDate(current.AddDate(0, 0, -14))
	if _, err = db.Exec("DELETE FROM forecast_entries WHERE user_id=? AND target_week_end=?", userID, invalidBiasWeek); err != nil {
		t.Fatal(err)
	}
	if _, ok = app.correctionFactorForWeek(ctx, userID, current); ok {
		t.Fatal("a missing coefficient in the immediately preceding four weeks must disable correction")
	}
}
