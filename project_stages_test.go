package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectStagesAreOrderedAndPartOfCompletion(t *testing.T) {
	got, err := parseProjectStages([]string{"工地服务", "投标", "投标", "施工图设计"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"投标", "施工图设计", "工地服务"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stages=%v, want %v", got, want)
	}
	if _, err = parseProjectStages([]string{"无效阶段"}); err == nil {
		t.Fatal("invalid project stage should be rejected")
	}

	db, err := openDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "测试部门领导", "stages@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err = db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date,intro_address,intro_type,intro_scale,intro_components) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)", "STAGE-1", "阶段测试项目", "阶段测试", "中", "总师", userID, userID, "2026-01-01", "2026-12-31", "贵阳市", "公共建筑", "10000平方米", "主楼及附楼")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := result.LastInsertId()
	app := &App{db: db}
	project, err := app.getProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !project.IsIncomplete {
		t.Fatal("an existing project without a stage must be marked incomplete")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.saveProjectStages(ctx, tx, projectID, want); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	project, err = app.getProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if project.IsIncomplete || !reflect.DeepEqual(project.Stages, want) || project.StageSummary() != "投标、施工图设计、工地服务" {
		t.Fatalf("project after stage save=%+v", project)
	}
}
