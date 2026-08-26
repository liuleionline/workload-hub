package main

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestFullReportScopeRequiresSystemAdmin(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "系统管理员", "admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var departmentID int64
	if err = db.QueryRow("SELECT id FROM departments LIMIT 1").Scan(&departmentID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO users(department_id,name,email,password_hash,role,is_system_admin,must_change_password)
		VALUES (?,?,?,?, 'manager',0,0)`, departmentID, "普通部门领导", "manager@test.local", "unused")
	if err != nil {
		t.Fatal(err)
	}
	managerID, _ := result.LastInsertId()
	app := &App{db: db, cfg: Config{Location: time.FixedZone("CST", 8*3600)}}
	manager := &User{ID: managerID, Name: "普通部门领导", Role: "manager"}
	request := httptest.NewRequest("GET", "/reports?year=2026&user=0", nil)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, manager))
	request = request.WithContext(context.WithValue(request.Context(), permissionsContextKey, map[string]bool{"exports.all": true}))
	data, err := app.reportData(request)
	if err != nil {
		t.Fatal(err)
	}
	if data.CanViewAll || data.SelectedUser != managerID {
		t.Fatalf("non-admin full scope leaked: canAll=%v selected=%d", data.CanViewAll, data.SelectedUser)
	}

	var admin User
	if err = db.QueryRow(`SELECT id,name,role,is_system_admin FROM users WHERE email='admin@test.local'`).Scan(&admin.ID, &admin.Name, &admin.Role, &admin.IsSystemAdmin); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest("GET", "/reports?year=2026&user=0", nil)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, &admin))
	data, err = app.reportData(request)
	if err != nil {
		t.Fatal(err)
	}
	if !data.CanViewAll || data.SelectedUser != 0 {
		t.Fatalf("system admin should have full scope: canAll=%v selected=%d", data.CanViewAll, data.SelectedUser)
	}
}

func TestMonthPeriodSelection(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	app := &App{cfg: Config{Location: loc}}
	request := httptest.NewRequest("GET", "/period/employees?year=2026&period=month&month=2", nil)
	period := app.periodSelection(request)
	if period.Type != "month" || period.Month != 2 || period.StartDate != "2026-02-01" || period.End.Format("2006-01-02") != "2026-03-01" {
		t.Fatalf("unexpected month selection: %+v", period)
	}
}
