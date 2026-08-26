package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOnlineBackupCanReopen(t *testing.T) {
	dir := t.TempDir()
	source, err := openDatabase(filepath.Join(dir, "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = source.createInitialAdmin(context.Background(), "备份测试", "backup@test.local", "123456"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "backup.db")
	if err = runBackup(source, []string{"--output", target}); err != nil {
		t.Fatal(err)
	}
	source.Close()
	restored, err := openDatabase(target)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var count int
	if err = restored.QueryRow("SELECT COUNT(*) FROM users WHERE email='backup@test.local'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("backup user count=%d err=%v", count, err)
	}
}
