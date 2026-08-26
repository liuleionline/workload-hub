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

func TestBiasAndProjectStagePagesRender(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试部门领导", "render@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var user User
	if err = db.QueryRow("SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Role, &user.IsSystemAdmin, &user.Active, &user.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("CST", 8*3600)
	app, err := NewApp(Config{Location: location}, db)
	if err != nil {
		t.Fatal(err)
	}
	withUser := func(request *http.Request) *http.Request {
		requestContext := context.WithValue(request.Context(), userContextKey, &user)
		requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{"dashboard.bias": true, "projects.edit_all": true})
		requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
		return request.WithContext(requestContext)
	}

	biasRequest := withUser(httptest.NewRequest(http.MethodGet, "/bias?week_end=2026-04-10", nil))
	biasRecorder := httptest.NewRecorder()
	app.handleBiasDashboard(biasRecorder, biasRequest)
	if biasRecorder.Code != http.StatusOK || !strings.Contains(biasRecorder.Body.String(), "员工偏差系数与修正负荷") {
		t.Fatalf("bias page status=%d body=%s", biasRecorder.Code, biasRecorder.Body.String())
	}

	project := Project{
		ID: 1, Code: "STAGE-UI", Name: "阶段界面测试", ShortName: "阶段界面",
		Size: "中", ChiefDesigner: "总师", CreatorUserID: user.ID,
		ExecutingLeadUserID: user.ID, StartDate: "2026-01-01", ExpectedEndDate: "2026-12-31",
		Stages: []string{"方案设计", "施工图设计"},
	}
	projectRequest := withUser(httptest.NewRequest(http.MethodGet, "/projects/1", nil))
	projectRecorder := httptest.NewRecorder()
	app.render(projectRecorder, projectRequest, http.StatusOK, "project-form.html", PageData{
		Title: "编辑项目",
		Data:  ProjectFormData{Project: project, Candidates: []User{user}, CanManageResponsibilities: true, StageOptions: projectStageOptions},
	})
	body := projectRecorder.Body.String()
	if projectRecorder.Code != http.StatusOK || !strings.Contains(body, "项目阶段") || !strings.Contains(body, "value=\"方案设计\" checked") {
		t.Fatalf("project form status=%d body=%s", projectRecorder.Code, body)
	}
}
