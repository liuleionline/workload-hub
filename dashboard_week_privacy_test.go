package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesignerWeeklyDashboardHidesLeadForecast(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "dashboard-week.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试管理员", "dashboard-admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var departmentID int64
	if err = db.QueryRow("SELECT department_id FROM users LIMIT 1").Scan(&departmentID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO users(department_id,name,email,password_hash,role,must_change_password)
		VALUES (?,?,?,?,?,0)`, departmentID, "设计师甲", "dashboard-designer@test.local", "unused", "designer")
	if err != nil {
		t.Fatal(err)
	}
	designerID, _ := result.LastInsertId()
	designer, err := (&App{db: db}).getUser(ctx, designerID)
	if err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/?period=week&week_end=2026-08-14", nil)
	requestContext := context.WithValue(request.Context(), userContextKey, designer)
	requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{"dashboard.self": true})
	requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
	request = request.WithContext(requestContext)
	recorder := httptest.NewRecorder()
	app.handleDashboard(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "截至2026年8月14日一周") || !strings.Contains(body, `value="2026-08-14"`) {
		t.Fatalf("weekly selection missing from dashboard: %s", body)
	}
	if strings.Contains(body, "负责人预估") || strings.Contains(body, "实际 / 预计") {
		t.Fatalf("designer dashboard exposed lead forecast: %s", body)
	}
}
