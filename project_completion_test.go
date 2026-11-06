package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCompletionIssuesMatchFormAnchors(t *testing.T) {
	form, err := webFiles.ReadFile("web/templates/project-form.html")
	if err != nil {
		t.Fatal(err)
	}
	issues := (Project{}).CompletionIssues()
	if len(issues) != 9 {
		t.Fatalf("empty project issues=%v, want 9", issues)
	}
	for _, issue := range issues {
		if strings.Count(string(form), `id="`+issue.FieldID+`"`) != 1 {
			t.Errorf("missing or duplicate field target: %s", issue.FieldID)
		}
	}
	p := Project{Code: "P-001", Name: "完整项目", StartDate: "2026-01-01", ExpectedEndDate: "2026-12-31",
		IntroAddress: "地址", IntroType: "工业", IntroScale: "规模", IntroComponents: "子项", Stages: []string{"施工图设计"}}
	if projectNeedsCompletion(p) {
		t.Fatal("complete project should not require review; optional features and subitems are not required")
	}
	p.Code = "INIT-004"
	if got := p.CompletionIssues(); len(got) != 1 || got[0].FieldID != "project-code" || !strings.Contains(got[0].Message, "临时编号") {
		t.Fatalf("temporary code issues=%v", got)
	}
}

func TestProjectCompletionRemindersIdentifyProjectAndFields(t *testing.T) {
	app, db, user, _ := newWorklogBugfixApp(t)
	ctx := context.Background()
	addProject := func(code, name, status, address string, assigned bool) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,
			start_date,expected_end_date,intro_address,intro_type,intro_scale,intro_components,status)
			VALUES (?,?,?,'大','负责人',?,'2026-01-01','2026-12-31',?,'工业','规模','子项',?)`,
			code, name, name, user.ID, address, status)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		if _, err = db.Exec("INSERT INTO project_stages(project_id,stage) VALUES (?,'施工图设计')", id); err != nil {
			t.Fatal(err)
		}
		if assigned {
			if _, err = db.Exec("INSERT INTO project_leads(project_id,user_id,is_execution) VALUES (?,?,1)", id, user.ID); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	id := addProject("INIT-004", "示例待完善项目", "active", "", true)
	addProject("P-002", "完整项目", "active", "地址", true)
	addProject("INIT-003", "已归档项目", "archived", "", true)
	addProject("INIT-005", "已完成项目", "completed", "", true)
	addProject("INIT-006", "其他负责人项目", "active", "", false)
	notices, err := app.incompleteProjects(ctx, user.ID)
	if err != nil || len(notices) != 1 || notices[0].ID != id || len(notices[0].Issues) != 2 {
		t.Fatalf("notices=%+v err=%v", notices, err)
	}
	other, err := app.incompleteProjects(ctx, user.ID+100)
	if err != nil || len(other) != 0 {
		t.Fatalf("unrelated user notices=%v err=%v", other, err)
	}
	render := func(canEdit bool, page string, data any) string {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/forecasts", nil)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, &user))
		r = r.WithContext(context.WithValue(r.Context(), permissionsContextKey, map[string]bool{"projects.edit_own": canEdit}))
		w := httptest.NewRecorder()
		app.render(w, r, http.StatusOK, page, PageData{Data: data})
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "</html>") {
			t.Fatalf("render status=%d body=%s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	projects := make([]Project, 18)
	for i := range projects {
		projects[i] = Project{ID: int64(i + 1), ShortName: fmt.Sprintf("示例项目 %02d", i+1), Code: fmt.Sprintf("PRJ-2026-%03d", i+1), Size: "大"}
	}
	projects[4].ShortName = "较长名称示例：新建电子工业生产厂房及配套设施项目"
	projects[5].Code = strings.Repeat("LONG-CODE-", 8)
	data := ForecastPageData{Projects: projects, Selected: projects[0], TargetWeekEnd: "2026-11-13"}
	body := render(true, "forecasts.html", data)
	for _, issue := range notices[0].Issues {
		link := fmt.Sprintf(`href="/projects/%d/edit#%s"`, id, issue.FieldID)
		if !strings.Contains(body, link) || !strings.Contains(body, issue.Message) {
			t.Fatalf("missing actionable issue: %s", link)
		}
	}
	if !strings.Contains(body, "示例待完善项目") || !strings.Contains(body, "有 1 个项目待完善") {
		t.Fatal("reminder must identify the project and count")
	}
	if strings.Count(body, `href="/forecasts?project=`) != len(projects) || strings.Count(body, `aria-current="page"`) != 1 || !strings.Contains(body, "project-selector-grid") {
		t.Fatal("forecast grid must include every project and exactly one selected project")
	}
	readOnly := render(false, "forecasts.html", data)
	if strings.Contains(readOnly, fmt.Sprintf(`/projects/%d/edit#`, id)) || !strings.Contains(readOnly, "请联系项目执行负责人") {
		t.Fatal("read-only user should see issues without unusable edit links")
	}
	p, err := app.getProject(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	form := render(true, "project-form.html", ProjectFormData{Project: *p, StageOptions: projectStageOptions})
	// Optional local UI fixtures contain synthetic data only, never production records.
	if dir := os.Getenv("WORKLOAD_UI_REVIEW_DIR"); dir != "" {
		for name, content := range map[string]string{"forecasts.html": body, "project-edit.html": form} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec("UPDATE projects SET code='P-004',intro_address='已补全地址' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if notices, err = app.incompleteProjects(ctx, user.ID); err != nil || len(notices) != 0 {
		t.Fatalf("completed project should stop reminding: %v %v", notices, err)
	}
	if body := render(true, "forecasts.html", data); strings.Contains(body, `aria-label="待完善项目信息"`) {
		t.Fatal("completed projects still show a reminder")
	}
}
