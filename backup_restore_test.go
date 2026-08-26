package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreDatabaseReplacesBusinessDataAndKeepsSchemaCurrent(t *testing.T) {
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source, err := openDatabase(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.createInitialAdmin(ctx, "来源管理员", "source@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var sourceUserID int64
	if err = source.QueryRow("SELECT id FROM users WHERE email='source@test.local'").Scan(&sourceUserID); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Exec(`INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date,
		intro_address,intro_type,intro_scale,intro_components) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"RESTORE-1", "恢复来源项目", "恢复项目", "中", "总师", sourceUserID, sourceUserID,
		"2026-01-01", "2026-12-31", "成都", "工业", "10000平方米", "主厂房"); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(t.TempDir(), "target.db")
	target, err := openDatabase(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err = target.createInitialAdmin(ctx, "目标管理员", "target@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	app := &App{db: target, cfg: Config{Location: time.FixedZone("CST", 8*3600)}}
	if err = app.restoreDatabase(ctx, sourcePath); err != nil {
		t.Fatal(err)
	}
	var sourceCount, targetCount, projectCount, backgroundCount int
	_ = target.QueryRow("SELECT COUNT(*) FROM users WHERE email='source@test.local'").Scan(&sourceCount)
	_ = target.QueryRow("SELECT COUNT(*) FROM users WHERE email='target@test.local'").Scan(&targetCount)
	_ = target.QueryRow("SELECT COUNT(*) FROM projects WHERE code='RESTORE-1' AND intro_address='成都'").Scan(&projectCount)
	_ = target.QueryRow("SELECT COUNT(*) FROM login_backgrounds").Scan(&backgroundCount)
	if sourceCount != 1 || targetCount != 0 || projectCount != 1 || backgroundCount < 1 {
		t.Fatalf("restored source=%d target=%d project=%d backgrounds=%d", sourceCount, targetCount, projectCount, backgroundCount)
	}
}

func TestValidateRestoreDatabaseRejectsNonDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	if err := writeTestFile(path, []byte("not a sqlite database")); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoreDatabase(path); err == nil {
		t.Fatal("invalid database should be rejected")
	}
}

func writeTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
