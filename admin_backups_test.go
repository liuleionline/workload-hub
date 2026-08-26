package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminBackupReminderDownloadAndIsolation(t *testing.T) {
	dir := t.TempDir()
	db, err := openDatabase(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.createInitialAdmin(ctx, "备份管理员", "backup-admin@test.local", "Temp123456"); err != nil {
		t.Fatal(err)
	}
	var admin User
	if err = db.QueryRow("SELECT id,department_id,name,email,mobile,qualification,professional_title,role,is_system_admin,active,must_change_password FROM users LIMIT 1").Scan(
		&admin.ID, &admin.DepartmentID, &admin.Name, &admin.Email, &admin.Mobile, &admin.Qualification,
		&admin.ProfessionalTitle, &admin.Role, &admin.IsSystemAdmin, &admin.Active, &admin.MustChangePassword,
	); err != nil {
		t.Fatal(err)
	}
	nonAdminResult, err := db.Exec("INSERT INTO users(department_id,name,email,password_hash,role,is_system_admin,must_change_password) VALUES (?,?,?,?,?,0,0)", admin.DepartmentID, "普通部门领导", "backup-manager@test.local", "unused", "manager")
	if err != nil {
		t.Fatal(err)
	}
	nonAdminID, _ := nonAdminResult.LastInsertId()
	nonAdmin, err := (&App{db: db}).getUser(ctx, nonAdminID)
	if err != nil {
		t.Fatal(err)
	}

	backupDir := filepath.Join(dir, "backups")
	if err = os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatal(err)
	}
	oldName := "workload-20260801-180000.db.gz"
	latestName := "workload-20260808-180000.db.gz"
	if err = os.WriteFile(filepath.Join(backupDir, oldName), []byte("old-backup"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(backupDir, latestName), []byte("latest-backup"), 0o640); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2026, 8, 1, 18, 0, 0, 0, time.Local)
	latestTime := time.Date(2026, 8, 8, 18, 0, 0, 0, time.Local)
	_ = os.Chtimes(filepath.Join(backupDir, oldName), oldTime, oldTime)
	_ = os.Chtimes(filepath.Join(backupDir, latestName), latestTime, latestTime)

	app, err := NewApp(Config{Location: time.FixedZone("CST", 8*3600), BackupDir: backupDir}, db)
	if err != nil {
		t.Fatal(err)
	}
	if pending := app.pendingBackupForAdmin(ctx, admin.ID); pending == nil || pending.Name != latestName || pending.Downloaded {
		t.Fatalf("pending backup=%+v", pending)
	}

	withUser := func(request *http.Request, user *User) *http.Request {
		requestContext := context.WithValue(request.Context(), userContextKey, user)
		requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{})
		requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
		return request.WithContext(requestContext)
	}
	pageRequest := withUser(httptest.NewRequest(http.MethodGet, "/admin/backups", nil), &admin)
	pageRecorder := httptest.NewRecorder()
	app.handleBackups(pageRecorder, pageRequest)
	if pageRecorder.Code != http.StatusOK || !strings.Contains(pageRecorder.Body.String(), "最新备份待本地存档") || !strings.Contains(pageRecorder.Body.String(), latestName) {
		t.Fatalf("backup page status=%d body=%s", pageRecorder.Code, pageRecorder.Body.String())
	}

	nonAdminRequest := withUser(httptest.NewRequest(http.MethodGet, "/admin/backups/download?file="+latestName, nil), nonAdmin)
	nonAdminRecorder := httptest.NewRecorder()
	app.handleBackupDownload(nonAdminRecorder, nonAdminRequest)
	if nonAdminRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin download status=%d", nonAdminRecorder.Code)
	}

	badRequest := withUser(httptest.NewRequest(http.MethodGet, "/admin/backups/download?file=../test.db", nil), &admin)
	badRecorder := httptest.NewRecorder()
	app.handleBackupDownload(badRecorder, badRequest)
	if badRecorder.Code != http.StatusNotFound {
		t.Fatalf("traversal download status=%d", badRecorder.Code)
	}

	downloadRequest := withUser(httptest.NewRequest(http.MethodGet, "/admin/backups/download?file="+latestName, nil), &admin)
	downloadRecorder := httptest.NewRecorder()
	app.handleBackupDownload(downloadRecorder, downloadRequest)
	if downloadRecorder.Code != http.StatusOK || downloadRecorder.Body.String() != "latest-backup" || !strings.Contains(downloadRecorder.Header().Get("Content-Disposition"), latestName) {
		t.Fatalf("download status=%d body=%q headers=%v", downloadRecorder.Code, downloadRecorder.Body.String(), downloadRecorder.Header())
	}
	if pending := app.pendingBackupForAdmin(ctx, admin.ID); pending != nil {
		t.Fatalf("backup remained pending after download: %+v", pending)
	}
	var downloads, audits int
	_ = db.QueryRow("SELECT COUNT(*) FROM backup_downloads WHERE backup_name=? AND user_id=?", latestName, admin.ID).Scan(&downloads)
	_ = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action='backup_download' AND entity_id=?", latestName).Scan(&audits)
	if downloads != 1 || audits != 1 {
		t.Fatalf("download records=%d audits=%d", downloads, audits)
	}
}

func TestHumanFileSize(t *testing.T) {
	if humanFileSize(1024) != "1.0 KB" || humanFileSize(2*1024*1024) != "2.0 MB" {
		t.Fatalf("unexpected file size labels: %s %s", humanFileSize(1024), humanFileSize(2*1024*1024))
	}
}
