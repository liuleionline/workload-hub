package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

type LoginBackgroundsPageData struct {
	Items []LoginBackground
}

func (a *App) handleRandomLoginBackground(w http.ResponseWriter, r *http.Request) {
	var item LoginBackground
	var data []byte
	err := a.db.QueryRowContext(r.Context(), `SELECT id,name,asset_path,mime_type,COALESCE(image_data,X''),active,created_at
		FROM login_backgrounds WHERE active=1 ORDER BY RANDOM() LIMIT 1`).Scan(
		&item.ID, &item.Name, &item.AssetPath, &item.MimeType, &data, &item.Active, &item.CreatedAt)
	if err != nil {
		http.Redirect(w, r, "/assets/login-default-1.svg", http.StatusTemporaryRedirect)
		return
	}
	a.serveLoginBackground(w, r, item, data)
}

func (a *App) handleLoginBackgroundImage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var item LoginBackground
	var data []byte
	err = a.db.QueryRowContext(r.Context(), `SELECT id,name,asset_path,mime_type,COALESCE(image_data,X''),active,created_at
		FROM login_backgrounds WHERE id=? AND active=1`, id).Scan(
		&item.ID, &item.Name, &item.AssetPath, &item.MimeType, &data, &item.Active, &item.CreatedAt)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.serveLoginBackground(w, r, item, data)
}

func (a *App) serveLoginBackground(w http.ResponseWriter, r *http.Request, item LoginBackground, data []byte) {
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Type", item.MimeType)
	if len(data) > 0 {
		_, _ = io.Copy(w, bytes.NewReader(data))
		return
	}
	file, err := a.staticFS.Open(item.AssetPath)
	if err != nil {
		http.Redirect(w, r, "/assets/login-default-1.svg", http.StatusTemporaryRedirect)
		return
	}
	defer file.Close()
	_, _ = io.Copy(w, file)
}

func (a *App) handleLoginBackgrounds(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,asset_path,mime_type,active,created_at
		FROM login_backgrounds ORDER BY id`)
	if err != nil {
		http.Error(w, "背景图库读取失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := []LoginBackground{}
	for rows.Next() {
		var item LoginBackground
		if err = rows.Scan(&item.ID, &item.Name, &item.AssetPath, &item.MimeType, &item.Active, &item.CreatedAt); err != nil {
			http.Error(w, "背景图库读取失败", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	a.render(w, r, http.StatusOK, "backgrounds.html", PageData{Title: "登录背景", Data: LoginBackgroundsPageData{Items: items}, Flash: r.URL.Query().Get("ok"), Error: r.URL.Query().Get("error")})
}

func (a *App) handleLoginBackgroundUpload(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Redirect(w, r, "/admin/backgrounds?error="+urlQuery("图片不能超过8MB"), http.StatusSeeOther)
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Redirect(w, r, "/admin/backgrounds?error="+urlQuery("请选择图片"), http.StatusSeeOther)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 8<<20 {
		http.Redirect(w, r, "/admin/backgrounds?error="+urlQuery("图片读取失败或超过8MB"), http.StatusSeeOther)
		return
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
		http.Redirect(w, r, "/admin/backgrounds?error="+urlQuery("仅支持JPG、PNG或WebP图片"), http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	result, err := a.db.ExecContext(r.Context(), `INSERT INTO login_backgrounds(name,mime_type,image_data) VALUES (?,?,?)`, truncate(name, 80), mimeType, data)
	if err != nil {
		http.Error(w, "背景图片保存失败", http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	user := currentUser(r)
	a.audit(r.Context(), &user.ID, "background_upload", "login_background", strconv.FormatInt(id, 10), fmt.Sprintf("新增登录背景：%s", name), clientIP(r))
	http.Redirect(w, r, "/admin/backgrounds?ok="+urlQuery("背景图片已加入随机图库"), http.StatusSeeOther)
}

func (a *App) handleLoginBackgroundDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var activeCount int
	_ = a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM login_backgrounds WHERE active=1").Scan(&activeCount)
	if activeCount <= 1 {
		http.Redirect(w, r, "/admin/backgrounds?error="+urlQuery("至少保留一张登录背景"), http.StatusSeeOther)
		return
	}
	var name string
	if err = a.db.QueryRowContext(r.Context(), "SELECT name FROM login_backgrounds WHERE id=?", id).Scan(&name); err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = a.db.ExecContext(r.Context(), "DELETE FROM login_backgrounds WHERE id=?", id); err != nil {
		http.Error(w, "删除背景失败", http.StatusInternalServerError)
		return
	}
	user := currentUser(r)
	a.audit(r.Context(), &user.ID, "background_delete", "login_background", strconv.FormatInt(id, 10), "删除登录背景："+name, clientIP(r))
	http.Redirect(w, r, "/admin/backgrounds?ok="+urlQuery("背景图片已移出图库"), http.StatusSeeOther)
}

func urlQuery(value string) string { return url.QueryEscape(value) }
