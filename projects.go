package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProjectsPageData struct {
	Projects []Project
	WeekEnd  string
}
type ProjectFormData struct {
	Project                   Project
	Candidates                []User
	IsNew                     bool
	CanManageResponsibilities bool
	StageOptions              []string
}

func (a *App) handleProjects(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	perms := currentPermissions(r)
	projects, err := a.visibleProjects(r.Context(), *user, perms["projects.view_all"])
	if err != nil {
		http.Error(w, "项目读取失败", 500)
		return
	}
	weekEnd := a.worklogWeekEnd(r.Context(), time.Now().In(a.cfg.Location))
	usages, _ := a.projectUsagesForWeek(r.Context(), weekEnd, 0, map[string]correctionFactorResult{}, user.IsTestUser)
	for i := range projects {
		projects[i].CanEdit = a.canEditProject(r, projects[i])
		projects[i].CanArchive = a.canArchiveProject(r, projects[i])
		projects[i].CanDelete = a.canDeleteProject(r, projects[i])
		usage := usages[projects[i].ID]
		if !perms["dashboard.bias"] {
			usage = projectUsageWithoutCorrection(usage)
		}
		projects[i].CurrentRawHours = usage.RawHours
		projects[i].CurrentSiteHours = usage.SiteHours
		projects[i].CurrentEffectiveHours = usage.EffectiveHours
		projects[i].CurrentForecastHours = usage.ForecastHours
		projects[i].DepartmentResourceRate = usage.ResourceRate
		projects[i].DepartmentWorkShare = usage.WorkShare
		projects[i].CurrentParticipantCount = usage.ParticipantCount
		projects[i].HasCorrectedHours = usage.HasCorrection
	}
	a.render(w, r, http.StatusOK, "projects.html", PageData{Title: "项目管理", Data: ProjectsPageData{Projects: projects, WeekEnd: isoDate(weekEnd)}, Flash: r.URL.Query().Get("ok"), Error: r.URL.Query().Get("error")})
}

func (a *App) visibleProjects(ctx context.Context, user User, viewAll bool) ([]Project, error) {
	query := `SELECT p.id,p.code,p.name,p.short_name,p.size,p.chief_designer,p.creator_user_id,cu.name,
		COALESCE(p.executing_lead_user_id,0),COALESCE(eu.name,''),p.start_date,p.expected_end_date,
		p.intro_address,p.intro_type,p.intro_scale,p.intro_components,p.intro_features,p.status,cu.is_test_user
		FROM projects p JOIN users cu ON cu.id=p.creator_user_id LEFT JOIN users eu ON eu.id=p.executing_lead_user_id`
	args := []any{}
	if !viewAll {
		query += ` WHERE p.creator_user_id=? OR p.executing_lead_user_id=?
			OR EXISTS (SELECT 1 FROM project_leads pl WHERE pl.project_id=p.id AND pl.user_id=?)`
		args = append(args, user.ID, user.ID, user.ID)
	}
	query += ` ORDER BY CASE p.status WHEN 'active' THEN 1 WHEN 'completed' THEN 2 ELSE 3 END,p.short_name`
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.ShortName, &p.Size, &p.ChiefDesigner, &p.CreatorUserID, &p.CreatorName, &p.ExecutingLeadUserID, &p.ExecutingLeadName, &p.StartDate, &p.ExpectedEndDate, &p.IntroAddress, &p.IntroType, &p.IntroScale, &p.IntroComponents, &p.IntroFeatures, &p.Status, &p.IsTestData); err != nil {
			return nil, err
		}
		p.Stages, _ = a.projectStages(ctx, p.ID)
		p.IsIncomplete = projectNeedsCompletion(p)
		p.Leads, _ = a.projectLeads(ctx, p.ID)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (a *App) projectStages(ctx context.Context, projectID int64) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT stage FROM project_stages WHERE project_id=?", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	selected := map[string]bool{}
	for rows.Next() {
		var stage string
		if err := rows.Scan(&stage); err != nil {
			return nil, err
		}
		selected[stage] = true
	}
	stages := make([]string, 0, len(selected))
	for _, option := range projectStageOptions {
		if selected[option] {
			stages = append(stages, option)
		}
	}
	return stages, rows.Err()
}

func (a *App) projectLeads(ctx context.Context, projectID int64) ([]User, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT u.id,u.department_id,u.name,u.email,u.role,u.is_system_admin,u.active,u.must_change_password
		FROM project_leads pl JOIN users u ON u.id=pl.user_id WHERE pl.project_id=? ORDER BY pl.is_execution DESC,u.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DepartmentID, &u.Name, &u.Email, &u.Role, &u.IsSystemAdmin, &u.Active, &u.MustChangePassword); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (a *App) leadCandidates(ctx context.Context) ([]User, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,department_id,name,email,role,is_system_admin,active,must_change_password FROM users
		WHERE active=1 AND role IN ('manager','lead') ORDER BY CASE role WHEN 'manager' THEN 1 ELSE 2 END,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DepartmentID, &u.Name, &u.Email, &u.Role, &u.IsSystemAdmin, &u.Active, &u.MustChangePassword); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (a *App) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	candidates, _ := a.leadCandidates(r.Context())
	user := currentUser(r)
	p := Project{Size: "中", CreatorUserID: user.ID, StartDate: isoDate(currentWeekEnd(time.Now().In(a.cfg.Location))), ExpectedEndDate: isoDate(currentWeekEnd(time.Now().In(a.cfg.Location)).AddDate(0, 3, 0))}
	a.render(w, r, http.StatusOK, "project-form.html", PageData{Title: "新建项目", Data: ProjectFormData{Project: p, Candidates: candidates, IsNew: true, CanManageResponsibilities: true, StageOptions: projectStageOptions}})
}

func (a *App) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	project, leadIDs, err := a.parseProjectForm(r, false)
	if err != nil {
		a.projectFormError(w, r, project, true, true, err.Error())
		return
	}
	project.CreatorUserID = user.ID
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "创建失败", 500)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO projects(code,name,short_name,size,chief_designer,creator_user_id,executing_lead_user_id,start_date,expected_end_date,
		intro_address,intro_type,intro_scale,intro_components,intro_features)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, project.Code, project.Name, project.ShortName, project.Size, project.ChiefDesigner, user.ID,
		project.ExecutingLeadUserID, project.StartDate, project.ExpectedEndDate, project.IntroAddress, project.IntroType, project.IntroScale, project.IntroComponents, project.IntroFeatures)
	if err != nil {
		a.projectFormError(w, r, project, true, true, "项目编号已存在或字段不完整")
		return
	}
	projectID, _ := result.LastInsertId()
	if err = a.saveProjectStages(r.Context(), tx, projectID, project.Stages); err != nil {
		a.projectFormError(w, r, project, true, true, err.Error())
		return
	}
	if err = a.saveProjectSubitems(r.Context(), tx, projectID, project.Subitems); err != nil {
		a.projectFormError(w, r, project, true, true, err.Error())
		return
	}
	if err = a.saveProjectLeads(r.Context(), tx, projectID, leadIDs, project.ExecutingLeadUserID); err != nil {
		a.projectFormError(w, r, project, true, true, err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "创建失败", 500)
		return
	}
	a.audit(r.Context(), &user.ID, "project_create", "project", strconv.FormatInt(projectID, 10), project.Code+" "+project.Name, clientIP(r))
	http.Redirect(w, r, "/projects?ok="+url.QueryEscape("项目已创建"), http.StatusSeeOther)
}

func (a *App) handleProjectEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := a.getProject(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.canEditProject(r, *p) {
		http.Error(w, "你没有编辑此项目的权限", 403)
		return
	}
	candidates, _ := a.leadCandidates(r.Context())
	canManageResponsibilities := a.canManageProjectResponsibilities(r, *p)
	a.render(w, r, http.StatusOK, "project-form.html", PageData{Title: "编辑项目", Data: ProjectFormData{Project: *p, Candidates: candidates, CanManageResponsibilities: canManageResponsibilities, StageOptions: projectStageOptions}})
}

func (a *App) handleProjectUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	existing, err := a.getProject(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.canEditProject(r, *existing) {
		http.Error(w, "你没有编辑此项目的权限", 403)
		return
	}
	canManageResponsibilities := a.canManageProjectResponsibilities(r, *existing)
	project, leadIDs, err := a.parseProjectForm(r, existing.IsIncomplete)
	project.ID = id
	project.CreatorUserID = existing.CreatorUserID
	if !canManageResponsibilities {
		leadIDs = projectLeadIDs(existing.Leads)
		project.ExecutingLeadUserID = existing.ExecutingLeadUserID
		project.Leads = existing.Leads
	}
	if err != nil {
		a.projectFormError(w, r, project, false, canManageResponsibilities, err.Error())
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE projects SET code=?,name=?,short_name=?,size=?,chief_designer=?,executing_lead_user_id=?,start_date=?,expected_end_date=?,
		intro_address=?,intro_type=?,intro_scale=?,intro_components=?,intro_features=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		project.Code, project.Name, project.ShortName, project.Size, project.ChiefDesigner, project.ExecutingLeadUserID, project.StartDate,
		project.ExpectedEndDate, project.IntroAddress, project.IntroType, project.IntroScale, project.IntroComponents, project.IntroFeatures, id)
	if err != nil {
		a.projectFormError(w, r, project, false, canManageResponsibilities, "项目编号已存在或字段不完整")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM project_stages WHERE project_id=?", id); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	if err = a.saveProjectStages(r.Context(), tx, id, project.Stages); err != nil {
		a.projectFormError(w, r, project, false, canManageResponsibilities, err.Error())
		return
	}
	if err = a.saveProjectSubitems(r.Context(), tx, id, project.Subitems); err != nil {
		a.projectFormError(w, r, project, false, canManageResponsibilities, err.Error())
		return
	}
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM project_leads WHERE project_id=?", id); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	if err = a.saveProjectLeads(r.Context(), tx, id, leadIDs, project.ExecutingLeadUserID); err != nil {
		a.projectFormError(w, r, project, false, canManageResponsibilities, err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	user := currentUser(r)
	a.audit(r.Context(), &user.ID, "project_update", "project", strconv.FormatInt(id, 10), project.Code+" "+project.Name, clientIP(r))
	http.Redirect(w, r, "/projects?ok="+url.QueryEscape("项目已更新"), http.StatusSeeOther)
}

func (a *App) handleProjectArchive(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := a.getProject(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := currentUser(r)
	if !a.canArchiveProject(r, *p) {
		http.Error(w, "你没有归档此项目的权限", 403)
		return
	}
	status := r.FormValue("status")
	if status != "completed" && status != "archived" {
		status = "completed"
	}
	_, err = a.db.ExecContext(r.Context(), `UPDATE projects SET status=?,completed_at=CASE WHEN ?='completed' THEN CURRENT_TIMESTAMP ELSE completed_at END,
		archived_at=CASE WHEN ?='archived' THEN CURRENT_TIMESTAMP ELSE archived_at END,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, status, status, id)
	if err != nil {
		http.Error(w, "操作失败", 500)
		return
	}
	a.audit(r.Context(), &user.ID, "project_"+status, "project", strconv.FormatInt(id, 10), p.Code+" "+p.Name, clientIP(r))
	http.Redirect(w, r, "/projects?ok="+url.QueryEscape("项目状态已更新"), http.StatusSeeOther)
}

func (a *App) handleProjectDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	project, err := a.getProject(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.canDeleteProject(r, *project) {
		http.Error(w, "只有项目创建者或系统管理员可以删除项目", http.StatusForbidden)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "删除失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var references int
	err = tx.QueryRowContext(r.Context(), "SELECT "+
		"(SELECT COUNT(*) FROM actual_work_entries WHERE project_id=?) + "+
		"(SELECT COUNT(*) FROM forecast_entries WHERE project_id=?) + "+
		"(SELECT COUNT(*) FROM project_participations WHERE project_id=?)", id, id, id).Scan(&references)
	if err != nil {
		http.Error(w, "删除前检查失败", http.StatusInternalServerError)
		return
	}
	if references > 0 {
		http.Redirect(w, r, "/projects?error="+url.QueryEscape("该项目已经产生工时、预估或参与记录，不能删除。项目完成时请使用“完成”功能。"), http.StatusSeeOther)
		return
	}
	result, err := tx.ExecContext(r.Context(), "DELETE FROM projects WHERE id=?", id)
	if err != nil {
		http.Error(w, "删除失败", http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		http.NotFound(w, r)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "删除失败", http.StatusInternalServerError)
		return
	}
	user := currentUser(r)
	a.audit(r.Context(), &user.ID, "project_delete", "project", strconv.FormatInt(id, 10), project.Code+" "+project.Name+"（错建或重复项目）", clientIP(r))
	http.Redirect(w, r, "/projects?ok="+url.QueryEscape("重复或错建项目已删除"), http.StatusSeeOther)
}

func (a *App) getProject(ctx context.Context, id int64) (*Project, error) {
	var p Project
	err := a.db.QueryRowContext(ctx, `SELECT p.id,p.code,p.name,p.short_name,p.size,p.chief_designer,p.creator_user_id,cu.name,
		COALESCE(p.executing_lead_user_id,0),COALESCE(eu.name,''),p.start_date,p.expected_end_date,
		p.intro_address,p.intro_type,p.intro_scale,p.intro_components,p.intro_features,p.status,cu.is_test_user
		FROM projects p JOIN users cu ON cu.id=p.creator_user_id LEFT JOIN users eu ON eu.id=p.executing_lead_user_id WHERE p.id=?`, id).Scan(&p.ID, &p.Code, &p.Name, &p.ShortName, &p.Size, &p.ChiefDesigner, &p.CreatorUserID, &p.CreatorName, &p.ExecutingLeadUserID, &p.ExecutingLeadName, &p.StartDate, &p.ExpectedEndDate, &p.IntroAddress, &p.IntroType, &p.IntroScale, &p.IntroComponents, &p.IntroFeatures, &p.Status, &p.IsTestData)
	if err != nil {
		return nil, err
	}
	p.Stages, _ = a.projectStages(ctx, id)
	p.IsIncomplete = projectNeedsCompletion(p)
	p.Leads, _ = a.projectLeads(ctx, id)
	p.Subitems, _ = a.projectSubitems(ctx, id, true)
	return &p, nil
}

func (a *App) incompleteProjectCount(ctx context.Context, userID int64) int {
	var count int
	_ = a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects
		WHERE status='active'
		AND EXISTS (SELECT 1 FROM project_leads pl WHERE pl.project_id=projects.id AND pl.user_id=?)
		AND (code LIKE 'INIT-%' OR name='' OR start_date='' OR expected_end_date=''
			OR intro_address='' OR intro_type='' OR intro_scale='' OR intro_components=''
			OR NOT EXISTS (SELECT 1 FROM project_stages ps WHERE ps.project_id=projects.id))`, userID).Scan(&count)
	return count
}

func (a *App) canViewProject(r *http.Request, p Project) bool {
	perms := currentPermissions(r)
	user := currentUser(r)
	return perms["projects.view_all"] || (perms["projects.view_own"] && (p.CreatorUserID == user.ID || p.ExecutingLeadUserID == user.ID || p.HasLead(user.ID)))
}

func (a *App) canEditProject(r *http.Request, p Project) bool {
	perms := currentPermissions(r)
	user := currentUser(r)
	return perms["projects.edit_all"] || (perms["projects.edit_own"] && (p.CreatorUserID == user.ID || p.ExecutingLeadUserID == user.ID || p.HasLead(user.ID)))
}

func (a *App) canArchiveProject(r *http.Request, p Project) bool {
	perms := currentPermissions(r)
	user := currentUser(r)
	return perms["projects.archive_all"] || (perms["projects.archive_own"] && (p.CreatorUserID == user.ID || p.ExecutingLeadUserID == user.ID))
}

func (a *App) canDeleteProject(r *http.Request, p Project) bool {
	user := currentUser(r)
	return user != nil && (user.IsSystemAdmin || p.CreatorUserID == user.ID)
}

func (a *App) canManageProjectResponsibilities(r *http.Request, p Project) bool {
	perms := currentPermissions(r)
	user := currentUser(r)
	return perms["projects.edit_all"] || (perms["projects.edit_own"] && (p.CreatorUserID == user.ID || p.ExecutingLeadUserID == user.ID))
}

func projectLeadIDs(leads []User) []int64 {
	ids := make([]int64, 0, len(leads))
	for _, lead := range leads {
		ids = append(ids, lead.ID)
	}
	return ids
}

func (a *App) parseProjectForm(r *http.Request, allowIncomplete bool) (Project, []int64, error) {
	stages, stageErr := parseProjectStages(r.Form["stages"])
	p := Project{
		Code: strings.TrimSpace(r.FormValue("code")), Name: strings.TrimSpace(r.FormValue("name")),
		ShortName: strings.TrimSpace(r.FormValue("short_name")), Size: r.FormValue("size"),
		ChiefDesigner: strings.TrimSpace(r.FormValue("chief_designer")), StartDate: strings.TrimSpace(r.FormValue("start_date")),
		ExpectedEndDate: strings.TrimSpace(r.FormValue("expected_end_date")), Stages: stages,
		IntroAddress: strings.TrimSpace(r.FormValue("intro_address")), IntroType: strings.TrimSpace(r.FormValue("intro_type")),
		IntroScale: strings.TrimSpace(r.FormValue("intro_scale")), IntroComponents: strings.TrimSpace(r.FormValue("intro_components")),
		IntroFeatures: strings.TrimSpace(r.FormValue("intro_features")),
	}
	if stageErr != nil {
		return p, nil, stageErr
	}
	subitems, subitemErr := parseProjectSubitems(r)
	p.Subitems = subitems
	if subitemErr != nil {
		return p, nil, subitemErr
	}
	if p.Code == "" || p.ShortName == "" || p.ChiefDesigner == "" {
		return p, nil, fmt.Errorf("请填写项目编号、项目简称和总设计师")
	}
	_ = allowIncomplete
	if p.Name == "" || len(p.Stages) == 0 || p.StartDate == "" || p.ExpectedEndDate == "" {
		return p, nil, fmt.Errorf("请填写项目名称、项目阶段和计划日期")
	}
	if p.IntroAddress == "" || p.IntroType == "" || p.IntroScale == "" || p.IntroComponents == "" {
		return p, nil, fmt.Errorf("请完整填写项目简介中的项目地址、项目类别、项目规模和子项组成")
	}
	if p.Size != "超大" && p.Size != "大" && p.Size != "中" && p.Size != "小" {
		return p, nil, fmt.Errorf("请选择有效的项目类型")
	}
	if p.StartDate != "" {
		if _, err := parseISODate(p.StartDate, a.cfg.Location); err != nil {
			return p, nil, fmt.Errorf("开始日期无效")
		}
	}
	if p.ExpectedEndDate != "" {
		if _, err := parseISODate(p.ExpectedEndDate, a.cfg.Location); err != nil {
			return p, nil, fmt.Errorf("预计结束日期无效")
		}
	}
	leadIDs := []int64{}
	seen := map[int64]bool{}
	for _, raw := range r.Form["lead_ids"] {
		id, err := parseID(raw)
		if err == nil && !seen[id] {
			leadIDs = append(leadIDs, id)
			seen[id] = true
		}
	}
	maxLeads := 1
	if p.Size == "超大" {
		maxLeads = 2
	}
	if len(leadIDs) == 0 || len(leadIDs) > maxLeads {
		return p, leadIDs, fmt.Errorf("该项目类型应设置1至%d位专业负责人", maxLeads)
	}
	execID, err := parseID(r.FormValue("executing_lead_user_id"))
	if err != nil || !seen[execID] {
		return p, leadIDs, fmt.Errorf("执行专业负责人必须从专业负责人中选择")
	}
	p.ExecutingLeadUserID = execID
	p.IsIncomplete = projectNeedsCompletion(p)
	return p, leadIDs, nil
}

func (a *App) saveProjectStages(ctx context.Context, tx *sql.Tx, projectID int64, stages []string) error {
	for _, stage := range stages {
		if _, err := tx.ExecContext(ctx, "INSERT INTO project_stages(project_id,stage) VALUES (?,?)", projectID, stage); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) saveProjectLeads(ctx context.Context, tx *sql.Tx, projectID int64, leadIDs []int64, execID int64) error {
	for _, id := range leadIDs {
		var active bool
		var role string
		if err := tx.QueryRowContext(ctx, "SELECT active,role FROM users WHERE id=?", id).Scan(&active, &role); err != nil || !active || (role != "lead" && role != "manager") {
			return fmt.Errorf("专业负责人选择无效")
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO project_leads(project_id,user_id,is_execution) VALUES (?,?,?)", projectID, id, id == execID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) projectFormError(w http.ResponseWriter, r *http.Request, p Project, isNew bool, canManageResponsibilities bool, message string) {
	candidates, _ := a.leadCandidates(r.Context())
	a.render(w, r, http.StatusBadRequest, "project-form.html", PageData{Title: map[bool]string{true: "新建项目", false: "编辑项目"}[isNew], Data: ProjectFormData{Project: p, Candidates: candidates, IsNew: isNew, CanManageResponsibilities: canManageResponsibilities, StageOptions: projectStageOptions}, Error: message})
}
