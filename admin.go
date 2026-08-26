package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type UsersPageData struct{ Users []User }
type PermissionView struct {
	Code, Name, Category, Mode string
	RoleAllowed                bool
}
type UserPermissionsData struct {
	User        User
	Permissions []PermissionView
}
type SettingData struct {
	WorkdayHours, AlertWeeks, AlertHours              string
	ReportOpenWeekday, ReportOpenTime                 string
	ReportCloseWeekday, ReportCloseTime, ReportWindow string
	Thresholds                                        []string
	Calendar                                          []CalendarDay
}
type CalendarDay struct {
	Date          string
	Hours         float64
	Label, Source string
}

func requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := currentUser(r)
	if user == nil || !user.IsSystemAdmin {
		http.Error(w, "只有系统管理员可以访问此功能", http.StatusForbidden)
		return false
	}
	return true
}

func (a *App) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,department_id,name,email,mobile,qualification,professional_title,role,is_system_admin,is_test_user,active,must_change_password
		FROM users ORDER BY active DESC,CASE role WHEN 'manager' THEN 1 WHEN 'lead' THEN 2 ELSE 3 END,name`)
	if err != nil {
		http.Error(w, "员工读取失败", 500)
		return
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if rows.Scan(&u.ID, &u.DepartmentID, &u.Name, &u.Email, &u.Mobile, &u.Qualification, &u.ProfessionalTitle, &u.Role, &u.IsSystemAdmin, &u.IsTestUser, &u.Active, &u.MustChangePassword) == nil {
			users = append(users, u)
		}
	}
	a.render(w, r, http.StatusOK, "users.html", PageData{Title: "员工与账号", Data: UsersPageData{Users: users}, Flash: r.URL.Query().Get("ok")})
}

func (a *App) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	actor := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	mobile := strings.TrimSpace(r.FormValue("mobile"))
	qualification := strings.TrimSpace(r.FormValue("qualification"))
	professionalTitle := strings.TrimSpace(r.FormValue("professional_title"))
	role := r.FormValue("role")
	password := r.FormValue("initial_password")
	isAdmin := r.FormValue("is_system_admin") == "1"
	isTest := r.FormValue("is_test_user") == "1"
	if name == "" || email == "" || (role != "manager" && role != "lead" && role != "designer") {
		a.userError(w, r, "请完整填写姓名、邮箱和员工层级")
		return
	}
	if isAdmin && isTest {
		a.userError(w, r, "测试用户不能同时设为系统管理员")
		return
	}
	hash, err := hashTemporaryPassword(password)
	if err != nil {
		a.userError(w, r, err.Error())
		return
	}
	var deptID int64
	if err = a.db.QueryRowContext(r.Context(), "SELECT id FROM departments WHERE active=1 ORDER BY id LIMIT 1").Scan(&deptID); err != nil {
		http.Error(w, "部门信息不存在", 500)
		return
	}
	result, err := a.db.ExecContext(r.Context(), `INSERT INTO users(department_id,name,email,mobile,qualification,professional_title,password_hash,role,is_system_admin,is_test_user,must_change_password)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, deptID, name, email, mobile, qualification, professionalTitle, hash, role, isAdmin, isTest, !isTest)
	if err != nil {
		a.userError(w, r, "邮箱已存在或信息格式不正确")
		return
	}
	id, _ := result.LastInsertId()
	a.audit(r.Context(), &actor.ID, "user_create", "user", strconv.FormatInt(id, 10), fmt.Sprintf("%s %s %s test=%t", name, email, role, isTest), clientIP(r))
	http.Redirect(w, r, "/admin/users?ok="+url.QueryEscape("员工账号已创建"), http.StatusSeeOther)
}

func (a *App) userError(w http.ResponseWriter, r *http.Request, message string) {
	a.render(w, r, http.StatusBadRequest, "users.html", PageData{Title: "员工与账号", Data: UsersPageData{}, Error: message})
}

func (a *App) handleUserResetPassword(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	password := r.FormValue("temporary_password")
	hash, err := hashTemporaryPassword(password)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE users SET password_hash=?,must_change_password=CASE WHEN is_test_user=1 THEN 0 ELSE 1 END,updated_at=CURRENT_TIMESTAMP WHERE id=?", hash, id)
	if err != nil {
		http.Error(w, "重置失败", 500)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		http.NotFound(w, r)
		return
	}
	actor := currentUser(r)
	a.audit(r.Context(), &actor.ID, "password_reset", "user", strconv.FormatInt(id, 10), "管理员重置临时密码", clientIP(r))
	http.Redirect(w, r, "/admin/users?ok="+url.QueryEscape("临时密码已重置"), http.StatusSeeOther)
}

func (a *App) handleUserPermissions(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user, err := a.getUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	views, err := a.permissionViews(r.Context(), *user)
	if err != nil {
		http.Error(w, "权限读取失败", 500)
		return
	}
	a.render(w, r, http.StatusOK, "permissions.html", PageData{Title: "个人权限设置", Data: UserPermissionsData{User: *user, Permissions: views}, Flash: r.URL.Query().Get("ok")})
}

func (a *App) permissionViews(ctx context.Context, user User) ([]PermissionView, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT p.code,p.name,p.category,CASE WHEN rp.permission_code IS NULL THEN 0 ELSE 1 END,
		CASE WHEN o.permission_code IS NULL THEN 'default' WHEN o.allowed=1 THEN 'allow' ELSE 'deny' END
		FROM permissions p LEFT JOIN role_permissions rp ON rp.permission_code=p.code AND rp.role=?
		LEFT JOIN user_permission_overrides o ON o.permission_code=p.code AND o.user_id=? ORDER BY p.category,p.code`, user.Role, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := []PermissionView{}
	for rows.Next() {
		var v PermissionView
		if err := rows.Scan(&v.Code, &v.Name, &v.Category, &v.RoleAllowed, &v.Mode); err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, rows.Err()
}

func (a *App) handleUserPermissionsSave(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = a.getUser(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	defer tx.Rollback()
	actor := currentUser(r)
	for _, p := range permissionSeeds {
		mode := r.FormValue("perm_" + p.Code)
		switch mode {
		case "allow", "deny":
			allowed := mode == "allow"
			_, err = tx.ExecContext(r.Context(), `INSERT INTO user_permission_overrides(user_id,permission_code,allowed,changed_by,changed_at) VALUES (?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(user_id,permission_code) DO UPDATE SET allowed=excluded.allowed,changed_by=excluded.changed_by,changed_at=CURRENT_TIMESTAMP`, id, p.Code, allowed, actor.ID)
		default:
			_, err = tx.ExecContext(r.Context(), "DELETE FROM user_permission_overrides WHERE user_id=? AND permission_code=?", id, p.Code)
		}
		if err != nil {
			http.Error(w, "保存失败", 500)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	a.audit(r.Context(), &actor.ID, "permissions_update", "user", strconv.FormatInt(id, 10), "更新个人权限覆盖", clientIP(r))
	http.Redirect(w, r, "/admin/users/"+strconv.FormatInt(id, 10)+"/permissions?ok="+url.QueryEscape("个人权限已保存"), http.StatusSeeOther)
}

func (a *App) getUser(ctx context.Context, id int64) (*User, error) {
	var u User
	err := a.db.QueryRowContext(ctx, `SELECT id,department_id,name,email,mobile,qualification,professional_title,role,is_system_admin,is_test_user,active,must_change_password FROM users WHERE id=?`, id).Scan(&u.ID, &u.DepartmentID, &u.Name, &u.Email, &u.Mobile, &u.Qualification, &u.ProfessionalTitle, &u.Role, &u.IsSystemAdmin, &u.IsTestUser, &u.Active, &u.MustChangePassword)
	return &u, err
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	schedule := a.reportSchedule(r.Context())
	data := SettingData{
		WorkdayHours: a.setting(r.Context(), "workday_hours", "8"), AlertWeeks: a.setting(r.Context(), "alert_consecutive_weeks", "3"),
		AlertHours: a.setting(r.Context(), "alert_hours_threshold", "48"), Thresholds: strings.Split(a.setting(r.Context(), "load_thresholds", "60,80,100,120"), ","),
		ReportOpenWeekday: strconv.Itoa(int(schedule.OpenWeekday)), ReportOpenTime: minuteLabel(schedule.OpenMinute),
		ReportCloseWeekday: strconv.Itoa(int(schedule.CloseWeekday)), ReportCloseTime: minuteLabel(schedule.CloseMinute), ReportWindow: schedule.Label(),
	}
	rows, _ := a.db.QueryContext(r.Context(), `SELECT work_date,work_hours,label,source FROM work_calendar WHERE work_date>=date('now','-60 day') ORDER BY work_date DESC LIMIT 80`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var d CalendarDay
			if rows.Scan(&d.Date, &d.Hours, &d.Label, &d.Source) == nil {
				data.Calendar = append(data.Calendar, d)
			}
		}
	}
	a.render(w, r, http.StatusOK, "settings.html", PageData{Title: "系统设置", Data: data, Flash: r.URL.Query().Get("ok")})
}

func (a *App) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	workday, err1 := strconv.ParseFloat(r.FormValue("workday_hours"), 64)
	weeks, err2 := strconv.Atoi(r.FormValue("alert_weeks"))
	alertHours, err3 := strconv.ParseFloat(r.FormValue("alert_hours"), 64)
	openWeekday, err4 := strconv.Atoi(r.FormValue("report_open_weekday"))
	closeWeekday, err5 := strconv.Atoi(r.FormValue("report_close_weekday"))
	openMinute, err6 := parseClockMinute(r.FormValue("report_open_time"))
	closeMinute, err7 := parseClockMinute(r.FormValue("report_close_time"))
	schedule := ReportSchedule{OpenWeekday: time.Weekday(openWeekday), OpenMinute: openMinute, CloseWeekday: time.Weekday(closeWeekday), CloseMinute: closeMinute}
	thresholds := []float64{}
	for _, key := range []string{"threshold_idle", "threshold_light", "threshold_normal", "threshold_busy"} {
		v, e := strconv.ParseFloat(r.FormValue(key), 64)
		if e != nil {
			err1 = e
		}
		thresholds = append(thresholds, v)
	}
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil || !schedule.Valid() ||
		workday <= 0 || workday > 12 || weeks < 1 || weeks > 12 || alertHours <= 0 || !(thresholds[0] < thresholds[1] && thresholds[1] < thresholds[2] && thresholds[2] < thresholds[3]) {
		http.Error(w, "设置值无效，请检查阈值顺序和数字范围", 400)
		return
	}
	actor := currentUser(r)
	values := map[string]string{
		"workday_hours": fmt.Sprintf("%g", workday), "alert_consecutive_weeks": strconv.Itoa(weeks),
		"alert_hours_threshold": fmt.Sprintf("%g", alertHours), "load_thresholds": fmt.Sprintf("%g,%g,%g,%g", thresholds[0], thresholds[1], thresholds[2], thresholds[3]),
		"report_open_weekday": strconv.Itoa(openWeekday), "report_open_minute": strconv.Itoa(openMinute),
		"report_close_weekday": strconv.Itoa(closeWeekday), "report_close_minute": strconv.Itoa(closeMinute),
	}
	tx, _ := a.db.BeginTx(r.Context(), nil)
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO settings(key,value,updated_by,updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_by=excluded.updated_by,updated_at=CURRENT_TIMESTAMP`, key, value, actor.ID); err != nil {
			http.Error(w, "保存失败", 500)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	a.audit(r.Context(), &actor.ID, "settings_update", "settings", "global", "更新工时、负荷阈值和填报窗口："+schedule.Label(), clientIP(r))
	http.Redirect(w, r, "/admin/settings?ok="+url.QueryEscape("系统设置已保存"), http.StatusSeeOther)
}

func (a *App) handleCalendarSave(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	date := r.FormValue("work_date")
	if _, err := parseISODate(date, a.cfg.Location); err != nil {
		http.Error(w, "日期无效", 400)
		return
	}
	hours, err := strconv.ParseFloat(r.FormValue("work_hours"), 64)
	if err != nil || hours < 0 || hours > 24 {
		http.Error(w, "工时无效", 400)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	actor := currentUser(r)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO work_calendar(work_date,work_hours,label,source,updated_by,updated_at) VALUES (?,?,?,'admin',?,CURRENT_TIMESTAMP)
		ON CONFLICT(work_date) DO UPDATE SET work_hours=excluded.work_hours,label=excluded.label,source='admin',updated_by=excluded.updated_by,updated_at=CURRENT_TIMESTAMP`, date, hours, label, actor.ID)
	if err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	a.audit(r.Context(), &actor.ID, "calendar_update", "calendar", date, fmt.Sprintf("%.1f小时 %s", hours, label), clientIP(r))
	http.Redirect(w, r, "/admin/settings?ok="+url.QueryEscape("工作日历已更新"), http.StatusSeeOther)
}

func (a *App) handleAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT l.id,COALESCE(u.name,'系统'),l.action,l.entity_type,l.detail,l.ip_address,l.created_at FROM audit_logs l LEFT JOIN users u ON u.id=l.actor_user_id ORDER BY l.id DESC LIMIT 300`)
	if err != nil {
		http.Error(w, "日志读取失败", 500)
		return
	}
	defer rows.Close()
	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		var created string
		if rows.Scan(&l.ID, &l.ActorName, &l.Action, &l.Entity, &l.Detail, &l.IPAddress, &created) == nil {
			if createdAt, parseErr := time.Parse("2006-01-02 15:04:05", created); parseErr == nil {
				l.CreatedAt = createdAt.In(a.cfg.Location)
			}
			logs = append(logs, l)
		}
	}
	a.render(w, r, http.StatusOK, "audit.html", PageData{Title: "操作审计", Data: logs})
}
