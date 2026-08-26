package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var backupFilePattern = regexp.MustCompile(`^workload-[0-9]{8}-[0-9]{6}\.db(?:\.gz)?$`)

type BackupFile struct {
	Name         string
	CreatedAt    time.Time
	CreatedLabel string
	Size         int64
	SizeLabel    string
	Downloaded   bool
	DownloadedAt string
}

type BackupsPageData struct {
	Files          []BackupFile
	Latest         *BackupFile
	RetentionCount int
	Schedule       string
	BackupReady    bool
}

func (a *App) handleBackups(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	user := currentUser(r)
	files, err := a.listBackupFiles(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "备份文件读取失败", http.StatusInternalServerError)
		return
	}
	data := BackupsPageData{Files: files, RetentionCount: 3, Schedule: "每周六18:00"}
	if len(files) > 0 {
		latest := files[0]
		data.Latest = &latest
		data.BackupReady = true
	}
	a.render(w, r, http.StatusOK, "backups.html", PageData{Title: "数据备份", Data: data, Flash: r.URL.Query().Get("ok"), Error: r.URL.Query().Get("error")})
}

func (a *App) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("file"))
	if !backupFilePattern.MatchString(name) || filepath.Base(name) != name {
		http.NotFound(w, r)
		return
	}
	path, ok := a.backupPath(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "备份文件打开失败", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	contentType := "application/octet-stream"
	if strings.HasSuffix(name, ".gz") {
		contentType = "application/gzip"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Cache-Control", "private, no-store")
	if _, err = io.Copy(w, file); err != nil {
		return
	}
	user := currentUser(r)
	_, _ = a.db.ExecContext(r.Context(), `INSERT INTO backup_downloads(backup_name,user_id,file_size,downloaded_at)
		VALUES (?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(backup_name,user_id) DO UPDATE SET file_size=excluded.file_size,downloaded_at=CURRENT_TIMESTAMP`, name, user.ID, info.Size())
	a.audit(r.Context(), &user.ID, "backup_download", "backup", name, fmt.Sprintf("下载服务器备份，%d字节", info.Size()), clientIP(r))
}

func (a *App) pendingBackupForAdmin(ctx context.Context, userID int64) *BackupFile {
	files, err := a.listBackupFiles(ctx, userID)
	if err != nil || len(files) == 0 || files[0].Downloaded {
		return nil
	}
	latest := files[0]
	return &latest
}

func (a *App) listBackupFiles(ctx context.Context, userID int64) ([]BackupFile, error) {
	entries, err := os.ReadDir(a.cfg.BackupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupFile{}, nil
		}
		return nil, err
	}
	files := []BackupFile{}
	for _, entry := range entries {
		if entry.IsDir() || !backupFilePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		item := BackupFile{
			Name: entry.Name(), CreatedAt: info.ModTime().In(a.cfg.Location),
			CreatedLabel: info.ModTime().In(a.cfg.Location).Format("2006年1月2日 15:04"),
			Size:         info.Size(), SizeLabel: humanFileSize(info.Size()),
		}
		_ = a.db.QueryRowContext(ctx, "SELECT downloaded_at FROM backup_downloads WHERE backup_name=? AND user_id=?", item.Name, userID).Scan(&item.DownloadedAt)
		item.Downloaded = item.DownloadedAt != ""
		files = append(files, item)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].CreatedAt.Equal(files[j].CreatedAt) {
			return files[i].Name > files[j].Name
		}
		return files[i].CreatedAt.After(files[j].CreatedAt)
	})
	return files, nil
}

func (a *App) backupPath(name string) (string, bool) {
	if !backupFilePattern.MatchString(name) || filepath.Base(name) != name {
		return "", false
	}
	root, err := filepath.Abs(a.cfg.BackupDir)
	if err != nil {
		return "", false
	}
	path, err := filepath.Abs(filepath.Join(root, name))
	if err != nil || filepath.Dir(path) != root {
		return "", false
	}
	return path, true
}

func humanFileSize(size int64) string {
	const mb = 1024 * 1024
	const kb = 1024
	if size >= mb {
		return fmt.Sprintf("%.1f MB", float64(size)/mb)
	}
	if size >= kb {
		return fmt.Sprintf("%.1f KB", float64(size)/kb)
	}
	return fmt.Sprintf("%d B", size)
}
