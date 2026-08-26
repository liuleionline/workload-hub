package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportProjectsAllowsIncompleteProfile(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "projects.csv")
	csvData := "项目编号,项目名称,项目简称,项目类型,项目负责人（总设计师）,专业负责人1,专业负责人2,执行专业负责人,创建人,开始日期,预计结束日期\n" +
		",,测试项目,超大,外部总师,测试负责人,,测试负责人,测试管理员,,2026-12-31\n"
	if err := os.WriteFile(csvPath, []byte(csvData), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := readImportedProjects(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(filepath.Join(dir, "workload.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.createInitialAdmin(context.Background(), "测试管理员", "admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	passwordHash, err := hashTemporaryPassword("Temp123456")
	if err != nil {
		t.Fatal(err)
	}
	var departmentID int64
	if err := db.QueryRow("SELECT id FROM departments LIMIT 1").Scan(&departmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role,must_change_password) VALUES (?,?,?,?,?,1)", departmentID, "测试负责人", "lead@test.local", passwordHash, "lead"); err != nil {
		t.Fatal(err)
	}
	if err := db.importProjects(context.Background(), projects); err != nil {
		t.Fatal(err)
	}
	var code, name, shortName, startDate, endDate string
	if err := db.QueryRow("SELECT code,name,short_name,start_date,expected_end_date FROM projects LIMIT 1").Scan(&code, &name, &shortName, &startDate, &endDate); err != nil {
		t.Fatal(err)
	}
	if code != "INIT-001" || name != "" || shortName != "测试项目" || startDate != "" || endDate != "2026-12-31" {
		t.Fatalf("unexpected imported project: code=%q name=%q short=%q start=%q end=%q", code, name, shortName, startDate, endDate)
	}
	var leadCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM project_leads").Scan(&leadCount); err != nil {
		t.Fatal(err)
	}
	if leadCount != 1 {
		t.Fatalf("lead count=%d, want 1", leadCount)
	}
	if !projectNeedsCompletion(Project{Name: name, StartDate: startDate, ExpectedEndDate: endDate}) {
		t.Fatal("incomplete imported project was not marked as needing completion")
	}
}
