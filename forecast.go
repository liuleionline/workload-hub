package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ForecastRow struct {
	User          User
	Hours         float64
	Participating bool
}
type ForecastPageData struct {
	Projects      []Project
	Selected      Project
	TargetWeekEnd string
	Rows          []ForecastRow
}

func (a *App) handleForecastPage(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	projects, _ := a.manageableProjects(r, *user)
	data := ForecastPageData{Projects: projects, TargetWeekEnd: isoDate(a.forecastTargetWeekEnd(r.Context(), time.Now().In(a.cfg.Location)))}
	if len(projects) > 0 {
		selectedID, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
		if selectedID == 0 {
			selectedID = projects[0].ID
		}
		for _, p := range projects {
			if p.ID == selectedID {
				data.Selected = p
				break
			}
		}
		if data.Selected.ID == 0 {
			data.Selected = projects[0]
		}
		data.Rows, _ = a.forecastRows(r, data.Selected.ID, data.TargetWeekEnd)
	}
	a.render(w, r, http.StatusOK, "forecasts.html", PageData{Title: "下周工时预估", Data: data, Flash: r.URL.Query().Get("ok")})
}

func (a *App) manageableProjects(r *http.Request, user User) ([]Project, error) {
	all := currentPermissions(r)["forecast.manage_all"]
	return a.visibleActiveProjectsForForecast(r, user, all)
}

func (a *App) visibleActiveProjectsForForecast(r *http.Request, user User, all bool) ([]Project, error) {
	projects, err := a.visibleProjects(r.Context(), user, all)
	if err != nil {
		return nil, err
	}
	active := projects[:0]
	for _, p := range projects {
		if p.Status == "active" && (all || p.CreatorUserID == user.ID || p.ExecutingLeadUserID == user.ID) {
			active = append(active, p)
		}
	}
	return active, nil
}

func (a *App) forecastRows(r *http.Request, projectID int64, targetWeekEnd string) ([]ForecastRow, error) {
	users, err := a.listActiveUsers(r.Context())
	if err != nil {
		return nil, err
	}
	rows := make([]ForecastRow, 0, len(users))
	for _, u := range users {
		var participating bool
		_ = a.db.QueryRowContext(r.Context(), "SELECT status='active' FROM project_participations WHERE project_id=? AND user_id=?", projectID, u.ID).Scan(&participating)
		var hours float64
		_ = a.db.QueryRowContext(r.Context(), "SELECT hours FROM forecast_entries WHERE project_id=? AND user_id=? AND target_week_end=?", projectID, u.ID, targetWeekEnd).Scan(&hours)
		if participating || hours > 0 {
			rows = append(rows, ForecastRow{User: u, Hours: hours, Participating: participating})
		}
	}
	// 仍允许负责人从全部员工中添加；前端将未参与人员放在折叠区。
	for _, u := range users {
		found := false
		for _, row := range rows {
			if row.User.ID == u.ID {
				found = true
				break
			}
		}
		if !found {
			rows = append(rows, ForecastRow{User: u})
		}
	}
	return rows, nil
}

func (a *App) handleForecastSave(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	projectID, err := parseID(r.FormValue("project_id"))
	if err != nil {
		http.Error(w, "项目无效", 400)
		return
	}
	p, err := a.getProject(r.Context(), projectID)
	if err != nil || p.Status != "active" {
		http.Error(w, "项目不可预估", 400)
		return
	}
	if !currentPermissions(r)["forecast.manage_all"] && !(currentPermissions(r)["forecast.manage_own"] && (p.CreatorUserID == user.ID || p.ExecutingLeadUserID == user.ID)) {
		http.Error(w, "你没有管理此项目预估的权限", 403)
		return
	}
	target := isoDate(a.forecastTargetWeekEnd(r.Context(), time.Now().In(a.cfg.Location)))
	userIDs := r.Form["user_id[]"]
	hoursValues := r.Form["hours[]"]
	if len(userIDs) != len(hoursValues) {
		http.Error(w, "预估表格格式错误", 400)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM forecast_entries WHERE target_week_end=? AND project_id=?", target, projectID); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	count := 0
	total := 0.0
	for i, rawID := range userIDs {
		id, err := parseID(rawID)
		if err != nil {
			continue
		}
		hoursText := strings.TrimSpace(valueAt(hoursValues, i))
		if hoursText == "" {
			continue
		}
		hours, err := strconv.ParseFloat(hoursText, 64)
		if err != nil || hours < 0 || hours > 168 {
			http.Error(w, "预估工时必须是0至168之间的数字", 400)
			return
		}
		if hours == 0 {
			continue
		}
		var active bool
		if err = tx.QueryRowContext(r.Context(), "SELECT active FROM users WHERE id=?", id).Scan(&active); err != nil || !active {
			http.Error(w, "参与人员无效", 400)
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO forecast_entries(target_week_end,project_id,user_id,hours,created_by) VALUES (?,?,?,?,?)`, target, projectID, id, hours, user.ID)
		if err != nil {
			http.Error(w, "保存失败", 500)
			return
		}
		_, _ = tx.ExecContext(r.Context(), `INSERT INTO project_participations(project_id,user_id,status) VALUES (?,?,'active')
			ON CONFLICT(project_id,user_id) DO UPDATE SET status='active',ended_at=NULL`, projectID, id)
		count++
		total += hours
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	a.audit(r.Context(), &user.ID, "forecast_save", "project", strconv.FormatInt(projectID, 10), fmt.Sprintf("%s：%d人，合计%.1f小时", target, count, total), clientIP(r))
	http.Redirect(w, r, "/forecasts?project="+strconv.FormatInt(projectID, 10)+"&ok="+url.QueryEscape("下周预估已保存"), http.StatusSeeOther)
}
