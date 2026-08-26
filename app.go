package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

//go:embed web/templates/*.html web/static/*
var webFiles embed.FS

type App struct {
	cfg       Config
	db        *DB
	templates embed.FS
	staticFS  fs.FS
}

type PageData struct {
	Title                  string
	Page                   string
	CurrentUser            *User
	Permissions            map[string]bool
	CSRFToken              string
	Data                   any
	Flash                  string
	Error                  string
	Now                    time.Time
	ReportReminder         bool
	ReportWindowLabel      string
	IncompleteProjectCount int
}

type contextKey string

const (
	userContextKey        contextKey = "user"
	permissionsContextKey contextKey = "permissions"
	csrfContextKey        contextKey = "csrf"
)

func NewApp(cfg Config, db *DB) (*App, error) {
	staticFS, err := fs.Sub(webFiles, "web/static")
	if err != nil {
		return nil, err
	}
	return &App{cfg: cfg, db: db, templates: webFiles, staticFS: staticFS}, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	assetHandler := http.StripPrefix("/assets/", http.FileServer(http.FS(a.staticFS)))
	mux.Handle("GET /assets/", a.cacheStatic(assetHandler))
	mux.HandleFunc("GET /login-background", a.handleRandomLoginBackground)
	mux.HandleFunc("GET /login-backgrounds/{id}", a.handleLoginBackgroundImage)
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /login", a.handleLoginPage)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.withAuth(a.handleLogout))
	mux.HandleFunc("GET /change-password", a.withAuth(a.handleChangePasswordPage))
	mux.HandleFunc("POST /change-password", a.withAuth(a.handleChangePassword))
	mux.HandleFunc("GET /", a.withPermission("dashboard.self", a.handleDashboard))
	mux.HandleFunc("GET /department", a.withPermission("dashboard.department", a.handleDepartment))
	mux.HandleFunc("GET /period/employees", a.withPermission("dashboard.department", a.handleEmployeePeriod))
	mux.HandleFunc("GET /period/projects", a.withPermission("dashboard.department", a.handleProjectPeriod))
	mux.HandleFunc("GET /employees/{id}", a.withPermission("dashboard.department", a.handleEmployeeDetail))
	mux.HandleFunc("GET /bias", a.withPermission("dashboard.bias", a.handleBiasDashboard))
	mux.HandleFunc("GET /worklog", a.withPermission("worklog.submit", a.handleWorklogPage))
	mux.HandleFunc("POST /worklog", a.withPermission("worklog.submit", a.withWorklogWindow(a.handleWorklogSave)))
	mux.HandleFunc("GET /forecasts", a.withAnyPermission([]string{"forecast.manage_own", "forecast.manage_all"}, a.handleForecastPage))
	mux.HandleFunc("POST /forecasts", a.withAnyPermission([]string{"forecast.manage_own", "forecast.manage_all"}, a.withReportWindow(a.handleForecastSave)))
	mux.HandleFunc("GET /projects", a.withAnyPermission([]string{"projects.view_own", "projects.view_all", "projects.create"}, a.handleProjects))
	mux.HandleFunc("GET /projects/new", a.withPermission("projects.create", a.handleProjectNew))
	mux.HandleFunc("POST /projects", a.withPermission("projects.create", a.handleProjectCreate))
	mux.HandleFunc("GET /projects/{id}", a.withAnyPermission([]string{"projects.view_own", "projects.view_all"}, a.handleProjectDetail))
	mux.HandleFunc("GET /projects/{id}/edit", a.withAuth(a.handleProjectEdit))
	mux.HandleFunc("POST /projects/{id}", a.withAuth(a.handleProjectUpdate))
	mux.HandleFunc("POST /projects/{id}/archive", a.withAuth(a.handleProjectArchive))
	mux.HandleFunc("POST /projects/{id}/delete", a.withAuth(a.handleProjectDelete))
	mux.HandleFunc("GET /reports", a.withAnyPermission([]string{"exports.self", "exports.all"}, a.handleReports))
	mux.HandleFunc("GET /exports/workbook.xlsx", a.withAnyPermission([]string{"exports.self", "exports.all"}, a.handleExcelExport))
	mux.HandleFunc("GET /exports/summary.xlsx", a.withAuth(a.handleSummaryExcelExport))
	mux.HandleFunc("GET /admin/users", a.withPermission("admin.users", a.handleUsers))
	mux.HandleFunc("POST /admin/users", a.withPermission("admin.users", a.handleUserCreate))
	mux.HandleFunc("POST /admin/users/{id}/reset-password", a.withPermission("admin.users", a.handleUserResetPassword))
	mux.HandleFunc("GET /admin/users/{id}/edit", a.withPermission("admin.users", a.handleUserEdit))
	mux.HandleFunc("POST /admin/users/{id}/edit", a.withPermission("admin.users", a.handleUserUpdate))
	mux.HandleFunc("GET /admin/users/{id}/permissions", a.withPermission("admin.permissions", a.handleUserPermissions))
	mux.HandleFunc("POST /admin/users/{id}/permissions", a.withPermission("admin.permissions", a.handleUserPermissionsSave))
	mux.HandleFunc("GET /admin/settings", a.withPermission("admin.settings", a.handleSettings))
	mux.HandleFunc("POST /admin/settings", a.withPermission("admin.settings", a.handleSettingsSave))
	mux.HandleFunc("POST /admin/calendar", a.withPermission("admin.settings", a.handleCalendarSave))
	mux.HandleFunc("GET /admin/backgrounds", a.withAuth(a.handleLoginBackgrounds))
	mux.HandleFunc("POST /admin/backgrounds", a.withAuth(a.handleLoginBackgroundUpload))
	mux.HandleFunc("POST /admin/backgrounds/{id}/delete", a.withAuth(a.handleLoginBackgroundDelete))
	mux.HandleFunc("GET /admin/backups", a.withAuth(a.handleBackups))
	mux.HandleFunc("GET /admin/backups/download", a.withAuth(a.handleBackupDownload))
	mux.HandleFunc("POST /admin/backups/restore", a.withAuth(a.handleBackupRestore))
	mux.HandleFunc("GET /admin/audit", a.withPermission("audit.view", a.handleAudit))
	return a.secureHeaders(a.recoverer(a.loadSession(mux)))
}

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, page string, data PageData) {
	funcs := template.FuncMap{
		"hasPerm": func(code string) bool { return data.Permissions[code] },
		"hours":   func(v float64) string { return fmt.Sprintf("%.1f", v) },
		"pct":     func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) },
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"roleName": func(role string) string {
			return (User{Role: role}).RoleName()
		},
		"projectStatus": func(status string) string {
			switch status {
			case "completed":
				return "已完成"
			case "archived":
				return "已归档"
			default:
				return "进行中"
			}
		},
		"seq": func(n int) []int { return make([]int, n) },
	}
	tmpl, err := template.New("base.html").Funcs(funcs).ParseFS(a.templates, "web/templates/base.html", "web/templates/"+page)
	if err != nil {
		slog.Error("模板解析失败", "page", page, "error", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	data.Page = strings.TrimSuffix(page, ".html")
	data.Now = time.Now().In(a.cfg.Location)
	if user := currentUser(r); user != nil {
		data.CurrentUser = user
		data.Permissions = currentPermissions(r)
		data.CSRFToken = currentCSRF(r)
		data.ReportReminder = a.reportWindowOpen(r.Context(), data.Now)
		data.ReportWindowLabel = a.reportSchedule(r.Context()).Label()
		data.IncompleteProjectCount = a.incompleteProjectCount(r.Context(), user.ID)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		slog.Error("模板输出失败", "page", page, "error", err)
	}
}

func (a *App) cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

func (a *App) secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; font-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("请求处理异常", "error", recovered, "stack", string(debug.Stack()))
				http.Error(w, "系统暂时不可用", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := a.db.PingContext(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
