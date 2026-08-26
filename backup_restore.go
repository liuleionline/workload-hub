package main

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var restoreDataTables = []string{
	"departments", "users", "permissions", "role_permissions", "user_permission_overrides",
	"projects", "project_leads", "project_stages", "project_participations",
	"actual_work_entries", "leave_records", "forecast_entries", "work_calendar",
	"settings", "audit_logs", "login_backgrounds",
}

var restoreOperationalTables = []string{"sessions", "login_attempts", "backup_downloads"}

func (a *App) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	admin := currentUser(r)
	adminName := admin.Name
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery("恢复文件不能超过64MB"), http.StatusSeeOther)
		return
	}
	file, header, err := r.FormFile("backup_file")
	if err != nil {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery("请选择.db或.db.gz备份文件"), http.StatusSeeOther)
		return
	}
	defer file.Close()
	lowerName := strings.ToLower(header.Filename)
	if !strings.HasSuffix(lowerName, ".db") && !strings.HasSuffix(lowerName, ".db.gz") {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery("仅支持系统生成的.db或.db.gz数据库备份"), http.StatusSeeOther)
		return
	}
	tempDir, err := os.MkdirTemp("", "workload-restore-")
	if err != nil {
		http.Error(w, "无法准备恢复文件", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)
	restorePath := filepath.Join(tempDir, "restore.db")
	if err = writeRestoreUpload(file, restorePath, strings.HasSuffix(lowerName, ".gz")); err != nil {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery(err.Error()), http.StatusSeeOther)
		return
	}
	if err = validateRestoreDatabase(restorePath); err != nil {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery("备份校验失败："+err.Error()), http.StatusSeeOther)
		return
	}
	safetyName := "workload-" + time.Now().In(a.cfg.Location).Format("20060102-150405") + ".db"
	safetyPath := filepath.Join(a.cfg.BackupDir, safetyName)
	if _, statErr := os.Stat(safetyPath); statErr == nil {
		safetyName = "workload-" + time.Now().In(a.cfg.Location).Add(time.Second).Format("20060102-150405") + ".db"
		safetyPath = filepath.Join(a.cfg.BackupDir, safetyName)
	}
	if err = runBackup(a.db, []string{"-output", safetyPath}); err != nil {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery("恢复前保护备份创建失败："+err.Error()), http.StatusSeeOther)
		return
	}
	if err = a.restoreDatabase(r.Context(), restorePath); err != nil {
		http.Redirect(w, r, "/admin/backups?error="+urlQuery("恢复未完成："+err.Error()), http.StatusSeeOther)
		return
	}
	a.audit(r.Context(), nil, "backup_restore", "backup", header.Filename, "系统管理员"+adminName+"恢复数据库；恢复前保护备份："+safetyName, clientIP(r))
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/login?restored=1", http.StatusSeeOther)
}

func writeRestoreUpload(source io.Reader, target string, compressed bool) error {
	var reader io.Reader = io.LimitReader(source, (64<<20)+1)
	if compressed {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return fmt.Errorf("压缩包无法读取")
		}
		defer gz.Close()
		reader = io.LimitReader(gz, (128<<20)+1)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("恢复文件读取失败")
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 || written > 128<<20 {
		return fmt.Errorf("解压后的数据库为空或超过128MB")
	}
	return nil
}

func validateRestoreDatabase(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		if err != nil {
			return err
		}
		return fmt.Errorf("SQLite完整性检查结果：%s", integrity)
	}
	for _, table := range []string{"users", "projects", "actual_work_entries", "settings"} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil || count != 1 {
			return fmt.Errorf("缺少系统数据表%s", table)
		}
	}
	return nil
}

func (a *App) restoreDatabase(ctx context.Context, restorePath string) error {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	if _, err = conn.ExecContext(ctx, "ATTACH DATABASE ? AS restore_db", restorePath); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "DETACH DATABASE restore_db")
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := len(restoreDataTables) - 1; i >= 0; i-- {
		if _, err = tx.ExecContext(ctx, "DELETE FROM main."+restoreDataTables[i]); err != nil {
			return fmt.Errorf("清理%s: %w", restoreDataTables[i], err)
		}
	}
	for _, table := range restoreOperationalTables {
		if _, err = tx.ExecContext(ctx, "DELETE FROM main."+table); err != nil {
			return fmt.Errorf("清理%s: %w", table, err)
		}
	}
	for _, table := range restoreDataTables {
		mainColumns, _, err := pragmaColumns(ctx, tx, "main", table)
		if err != nil {
			return err
		}
		_, restoreColumns, err := pragmaColumns(ctx, tx, "restore_db", table)
		if err != nil {
			return err
		}
		shared := []string{}
		for _, column := range mainColumns {
			if restoreColumns[column] {
				shared = append(shared, column)
			}
		}
		if len(shared) == 0 {
			continue
		}
		quoted := make([]string, len(shared))
		for i, column := range shared {
			quoted[i] = `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
		}
		list := strings.Join(quoted, ",")
		if _, err = tx.ExecContext(ctx, "INSERT INTO main."+table+"("+list+") SELECT "+list+" FROM restore_db."+table); err != nil {
			return fmt.Errorf("恢复%s: %w", table, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = a.db.ensureSchemaColumns(); err != nil {
		return err
	}
	if err = a.db.seedDefaults(); err != nil {
		return err
	}
	var violation string
	err = conn.QueryRowContext(ctx, "SELECT COALESCE((SELECT `table`||':'||rowid FROM pragma_foreign_key_check LIMIT 1),'')").Scan(&violation)
	if err != nil {
		return err
	}
	if violation != "" {
		return fmt.Errorf("恢复后外键检查失败：%s", violation)
	}
	return nil
}

func pragmaColumns(ctx context.Context, tx *sql.Tx, schema, table string) ([]string, map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA "+schema+".table_info("+table+")")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	order := []string{}
	set := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, nil, err
		}
		order = append(order, name)
		set[name] = true
	}
	return order, set, rows.Err()
}
