package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "zh_session"

func (a *App) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		var user User
		var csrf, expires string
		err = a.db.QueryRowContext(r.Context(), `SELECT u.id,u.department_id,u.name,u.email,u.role,u.is_system_admin,u.is_test_user,u.active,u.must_change_password,s.csrf_token,s.expires_at
			FROM sessions s JOIN users u ON u.id=s.user_id
			WHERE s.token_hash=?`, hashToken(cookie.Value)).Scan(
			&user.ID, &user.DepartmentID, &user.Name, &user.Email, &user.Role, &user.IsSystemAdmin, &user.IsTestUser,
			&user.Active, &user.MustChangePassword, &csrf, &expires)
		if err != nil || !user.Active {
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		expiry, err := time.Parse(time.RFC3339, expires)
		if err != nil || time.Now().After(expiry) {
			_, _ = a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", hashToken(cookie.Value))
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		permissions, err := a.effectivePermissions(r.Context(), user)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		_, _ = a.db.ExecContext(r.Context(), "UPDATE sessions SET last_seen_at=CURRENT_TIMESTAMP WHERE token_hash=?", hashToken(cookie.Value))
		ctx := context.WithValue(r.Context(), userContextKey, &user)
		ctx = context.WithValue(ctx, permissionsContextKey, permissions)
		ctx = context.WithValue(ctx, csrfContextKey, csrf)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) effectivePermissions(ctx context.Context, user User) (map[string]bool, error) {
	permissions := map[string]bool{}
	if user.IsSystemAdmin {
		rows, err := a.db.QueryContext(ctx, "SELECT code FROM permissions")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err != nil {
				return nil, err
			}
			permissions[code] = true
		}
		return permissions, rows.Err()
	}
	rows, err := a.db.QueryContext(ctx, `SELECT p.code,
		COALESCE(o.allowed, CASE WHEN rp.permission_code IS NULL THEN 0 ELSE 1 END)
		FROM permissions p
		LEFT JOIN role_permissions rp ON rp.permission_code=p.code AND rp.role=?
		LEFT JOIN user_permission_overrides o ON o.permission_code=p.code AND o.user_id=?`, user.Role, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var allowed bool
		if err := rows.Scan(&code, &allowed); err != nil {
			return nil, err
		}
		permissions[code] = allowed
	}
	return permissions, rows.Err()
}

func currentUser(r *http.Request) *User {
	user, _ := r.Context().Value(userContextKey).(*User)
	return user
}

func currentPermissions(r *http.Request) map[string]bool {
	perms, _ := r.Context().Value(permissionsContextKey).(map[string]bool)
	if perms == nil {
		return map[string]bool{}
	}
	return perms
}

func currentCSRF(r *http.Request) string {
	value, _ := r.Context().Value(csrfContextKey).(string)
	return value
}

func (a *App) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		if user == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && !a.validCSRF(r) {
			http.Error(w, "请求已过期，请刷新页面后重试", http.StatusForbidden)
			return
		}
		if user.MustChangePassword && !user.IsTestUser && r.URL.Path != "/change-password" && r.URL.Path != "/logout" {
			http.Redirect(w, r, "/change-password", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *App) withPermission(code string, next http.HandlerFunc) http.HandlerFunc {
	return a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if !currentPermissions(r)[code] {
			http.Error(w, "你没有访问此功能的权限", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (a *App) withAnyPermission(codes []string, next http.HandlerFunc) http.HandlerFunc {
	return a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		perms := currentPermissions(r)
		for _, code := range codes {
			if perms[code] {
				next(w, r)
				return
			}
		}
		http.Error(w, "你没有访问此功能的权限", http.StatusForbidden)
	})
}

func (a *App) validCSRF(r *http.Request) bool {
	provided := r.FormValue("csrf_token")
	if provided == "" {
		provided = r.Header.Get("X-CSRF-Token")
	}
	return provided != "" && provided == currentCSRF(r)
}

func (a *App) createSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, err := randomToken(32)
	if err != nil {
		return err
	}
	csrf, err := randomToken(24)
	if err != nil {
		return err
	}
	expires := time.Now().Add(12 * time.Hour)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO sessions(user_id,token_hash,csrf_token,ip_address,user_agent,expires_at)
		VALUES (?,?,?,?,?,?)`, userID, hashToken(token), csrf, clientIP(r), truncate(r.UserAgent(), 300), expires.Format(time.RFC3339))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int((12 * time.Hour).Seconds())})
	return nil
}

func (a *App) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	flash := ""
	if r.URL.Query().Get("restored") == "1" {
		flash = "备份已恢复。为保证会话安全，请重新登录。"
	}
	a.render(w, r, http.StatusOK, "login.html", PageData{Title: "登录", Error: r.URL.Query().Get("error"), Flash: flash})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOriginRequest(r) {
		http.Error(w, "无效的登录请求", http.StatusForbidden)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	ip := clientIP(r)
	if a.loginBlocked(r.Context(), email, ip) {
		http.Redirect(w, r, "/login?error="+url.QueryEscape("尝试次数过多，请15分钟后再试"), http.StatusSeeOther)
		return
	}
	var userID int64
	var hash string
	var active bool
	err := a.db.QueryRowContext(r.Context(), "SELECT id,password_hash,active FROM users WHERE email=?", email).Scan(&userID, &hash, &active)
	succeeded := err == nil && active && verifyPassword(hash, password)
	_, _ = a.db.ExecContext(r.Context(), "INSERT INTO login_attempts(email,ip_address,succeeded) VALUES (?,?,?)", email, ip, succeeded)
	if !succeeded {
		http.Redirect(w, r, "/login?error="+url.QueryEscape("邮箱或密码不正确"), http.StatusSeeOther)
		return
	}
	if err := a.createSession(w, r, userID); err != nil {
		http.Error(w, "登录失败，请稍后重试", http.StatusInternalServerError)
		return
	}
	_, _ = a.db.ExecContext(r.Context(), "DELETE FROM login_attempts WHERE attempted_at < datetime('now','-7 day')")
	a.audit(r.Context(), &userID, "login", "session", "", "用户登录", ip)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) loginBlocked(ctx context.Context, email, ip string) bool {
	var failures int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_attempts
		WHERE email=? AND ip_address=? AND succeeded=0 AND attempted_at >= datetime('now','-15 minute')`, email, ip).Scan(&failures)
	return err == nil && failures >= 5
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_, _ = a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", hashToken(cookie.Value))
	}
	user := currentUser(r)
	if user != nil {
		a.audit(r.Context(), &user.ID, "logout", "session", "", "用户退出", clientIP(r))
	}
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) handleChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, "change-password.html", PageData{Title: "修改密码"})
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	current := r.FormValue("current_password")
	password := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	var stored string
	if err := a.db.QueryRowContext(r.Context(), "SELECT password_hash FROM users WHERE id=?", user.ID).Scan(&stored); err != nil || !verifyPassword(stored, current) {
		a.render(w, r, http.StatusBadRequest, "change-password.html", PageData{Title: "修改密码", Error: "当前密码不正确"})
		return
	}
	if password != confirm {
		a.render(w, r, http.StatusBadRequest, "change-password.html", PageData{Title: "修改密码", Error: "两次输入的新密码不一致"})
		return
	}
	if err := validatePasswordChange(current, password); err != nil {
		a.render(w, r, http.StatusBadRequest, "change-password.html", PageData{Title: "修改密码", Error: err.Error()})
		return
	}
	hash, err := hashPassword(password)
	if err != nil {
		a.render(w, r, http.StatusBadRequest, "change-password.html", PageData{Title: "修改密码", Error: err.Error()})
		return
	}
	if _, err = a.db.ExecContext(r.Context(), "UPDATE users SET password_hash=?,must_change_password=0,updated_at=CURRENT_TIMESTAMP WHERE id=?", hash, user.ID); err != nil {
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	_, _ = a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=? AND token_hash<>?", user.ID, sessionTokenHash(r))
	a.audit(r.Context(), &user.ID, "password_change", "user", strconv.FormatInt(user.ID, 10), "修改个人密码", clientIP(r))
	http.Redirect(w, r, "/?ok="+url.QueryEscape("密码已更新"), http.StatusSeeOther)
}

func sessionTokenHash(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		return hashToken(cookie.Value)
	}
	return ""
}

func sameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func (a *App) audit(ctx context.Context, actor *int64, action, entityType, entityID, detail, ip string) {
	_, _ = a.db.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,action,entity_type,entity_id,detail,ip_address)
		VALUES (?,?,?,?,?,?)`, actor, action, entityType, entityID, truncate(detail, 1000), truncate(ip, 100))
}

func scanNullableInt(value sql.NullInt64) int64 {
	if value.Valid {
		return value.Int64
	}
	return 0
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效编号")
	}
	return id, nil
}
