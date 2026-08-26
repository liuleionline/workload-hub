package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *App) handleUserEdit(w http.ResponseWriter, r *http.Request) {
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
	a.render(w, r, http.StatusOK, "user-edit.html", PageData{Title: "编辑员工账号", Data: *user})
}

func (a *App) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target, err := a.getUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	actor := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	mobile := strings.TrimSpace(r.FormValue("mobile"))
	qualification := strings.TrimSpace(r.FormValue("qualification"))
	professionalTitle := strings.TrimSpace(r.FormValue("professional_title"))
	role := r.FormValue("role")
	if name == "" || email == "" || (role != "manager" && role != "lead" && role != "designer") {
		http.Error(w, "员工层级无效", 400)
		return
	}
	active := r.FormValue("active") == "1"
	isAdmin := r.FormValue("is_system_admin") == "1"
	isTest := r.FormValue("is_test_user") == "1"
	if isAdmin && isTest {
		http.Error(w, "测试用户不能同时设为系统管理员", http.StatusBadRequest)
		return
	}
	if target.ID == actor.ID && (!active || !isAdmin) {
		http.Error(w, "当前系统管理员不能停用或取消自己的管理员身份", 400)
		return
	}
	mustChangePassword := target.MustChangePassword
	if isTest {
		mustChangePassword = false
	} else if target.IsTestUser {
		mustChangePassword = true
	}
	_, err = a.db.ExecContext(r.Context(), "UPDATE users SET name=?,email=?,mobile=?,qualification=?,professional_title=?,role=?,active=?,is_system_admin=?,is_test_user=?,must_change_password=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", name, email, mobile, qualification, professionalTitle, role, active, isAdmin, isTest, mustChangePassword, id)
	if err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	if !active {
		_, _ = a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id=?", id)
	}
	a.audit(r.Context(), &actor.ID, "user_update", "user", strconv.FormatInt(id, 10), fmt.Sprintf("更新员工资料、层级、启用状态、系统管理员或测试用户身份；test=%t", isTest), clientIP(r))
	http.Redirect(w, r, "/admin/users?ok="+url.QueryEscape("员工账号已更新"), http.StatusSeeOther)
}
