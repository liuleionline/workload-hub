package main

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestAssignedProjectLeadCanViewAndEditButNotArchive(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "部门领导", "manager@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}

	var departmentID, creatorID int64
	if err = db.QueryRow("SELECT department_id,id FROM users LIMIT 1").Scan(&departmentID, &creatorID); err != nil {
		t.Fatal(err)
	}
	execResult, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role) VALUES (?,?,?,?,?)", departmentID, "执行负责人", "exec@test.local", "unused", "lead")
	if err != nil {
		t.Fatal(err)
	}
	execID, _ := execResult.LastInsertId()
	leadResult, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role) VALUES (?,?,?,?,?)", departmentID, "第二负责人", "lead@test.local", "unused", "lead")
	if err != nil {
		t.Fatal(err)
	}
	leadID, _ := leadResult.LastInsertId()
	projectResult, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date) VALUES (?,?,?,?,?,?,?,?,?)", "INIT-1", "", "预设项目", "超大", "总设计师", creatorID, execID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := projectResult.LastInsertId()
	if _, err = db.Exec("INSERT INTO project_leads(project_id,user_id,is_execution) VALUES (?,?,0),(?,?,1)", projectID, leadID, projectID, execID); err != nil {
		t.Fatal(err)
	}

	app := &App{db: db}
	projects, err := app.visibleProjects(ctx, User{ID: leadID, Role: "lead"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != projectID {
		t.Fatalf("assigned lead projects=%v, want project %d", projects, projectID)
	}
	if got := app.incompleteProjectCount(ctx, leadID); got != 1 {
		t.Fatalf("incomplete project reminders=%d, want 1", got)
	}

	request := httptest.NewRequest("GET", "/projects/1", nil)
	requestContext := context.WithValue(request.Context(), userContextKey, &User{ID: leadID, Role: "lead"})
	requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{"projects.edit_own": true, "projects.archive_own": true})
	request = request.WithContext(requestContext)
	if !app.canEditProject(request, projects[0]) {
		t.Fatal("assigned professional lead should be able to edit project information")
	}
	if app.canArchiveProject(request, projects[0]) {
		t.Fatal("non-executing professional lead must not be able to complete or archive the project")
	}
}
