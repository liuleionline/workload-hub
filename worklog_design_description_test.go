package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func createDesignDescriptionProject(t *testing.T, db *DB, user User) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date)
		VALUES (?,?,?,?,?,?,?,?,?)`, "DESIGN-CONTENT-1", "设计内容测试项目", "设计内容测试", "小", "总师", user.ID, user.ID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return projectID
}

func designDescriptionValues(projectID int64, role, description string) url.Values {
	return url.Values{
		"leave_days":           {"0"},
		"entry_type[]":         {"project"},
		"work_category[]":      {"regular"},
		"project_choice[]":     {strconv.FormatInt(projectID, 10)},
		"project_id[]":         {""},
		"hours[]":              {"8"},
		"work_subitem[]":       {"101生产厂房"},
		"work_area[]":          {"10000"},
		"work_structure[]":     {"混凝土框架结构"},
		"work_role[]":          {role},
		"design_description[]": {description},
		"other_description[]":  {""},
		"end_participation[]":  {"0"},
	}
}

func TestDesignRoleRequiresAndSavesWeeklyDescription(t *testing.T) {
	app, db, user, weekEnd := newWorklogBugfixApp(t)
	projectID := createDesignDescriptionProject(t, db, user)

	missing := postWorklogForTest(app, &user, designDescriptionValues(projectID, "独立设计", ""))
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "请简述本周设计内容") {
		t.Fatalf("missing design description status=%d body=%s", missing.Code, missing.Body.String())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM actual_work_entries WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("entries after rejected submission=%d, want 0", count)
	}

	audit := postWorklogForTest(app, &user, designDescriptionValues(projectID, "审核", ""))
	if audit.Code != http.StatusSeeOther {
		t.Fatalf("audit role status=%d body=%s", audit.Code, audit.Body.String())
	}
	var saved string
	if err := db.QueryRow("SELECT design_description FROM actual_work_entries WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != "" {
		t.Fatalf("audit role design description=%q, want empty", saved)
	}

	design := postWorklogForTest(app, &user, designDescriptionValues(projectID, "部分设计（请写明设计部分）", "基础设计"))
	if design.Code != http.StatusSeeOther {
		t.Fatalf("design role status=%d body=%s", design.Code, design.Body.String())
	}
	if err := db.QueryRow("SELECT design_description FROM actual_work_entries WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != "基础设计" {
		t.Fatalf("saved design description=%q, want 基础设计", saved)
	}
}

func TestCarriedWorkDoesNotCopyPreviousDesignDescription(t *testing.T) {
	app, db, user, weekEnd := newWorklogBugfixApp(t)
	projectID := createDesignDescriptionProject(t, db, user)
	if _, err := db.Exec(`INSERT INTO project_participations(
		project_id,user_id,latest_work_content,latest_work_subitem,latest_work_area,latest_work_structure,latest_work_role,status)
		VALUES (?,?,?,?,?,?,?,'active')`, projectID, user.ID, "101生产厂房设计", "101生产厂房", 10000, "混凝土框架结构", "独立设计"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO actual_work_entries(
		week_end,user_id,project_id,hours,work_content,work_subitem,work_area,work_structure,work_role,design_description)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, isoDate(weekEnd.AddDate(0, 0, -7)), user.ID, projectID, 8, "101生产厂房设计", "101生产厂房", 10000, "混凝土框架结构", "独立设计", "上周梁配筋设计"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/worklog", nil)
	requestContext := context.WithValue(request.Context(), userContextKey, &user)
	requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{})
	requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
	page := httptest.NewRecorder()
	app.handleWorklogPage(page, request.WithContext(requestContext))
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "已带入上周内容") {
		t.Fatalf("carried work page status=%d body=%s", page.Code, body)
	}
	if strings.Contains(body, "上周梁配筋设计") || !strings.Contains(body, `data-design-description=""`) {
		t.Fatalf("previous design description was carried: %s", body)
	}
}
