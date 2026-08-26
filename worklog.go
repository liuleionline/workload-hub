package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type WorklogPageData struct {
	WeekEnd           string
	WindowStart       string
	WindowEnd         string
	LeaveDays         float64
	Entries           []WorkEntry
	Projects          []Project
	Carried           bool
	Submitted         bool
	LatestWorkDetails map[int64]WorkContentFields
	ProjectSubitems   map[int64][]ProjectSubitem
	CanEdit           bool
}

func (a *App) handleWorklogPage(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	now := time.Now().In(a.cfg.Location)
	weekEnd := a.worklogWeekEnd(r.Context(), now)
	start, end := a.reportWindow(r.Context(), weekEnd)
	data := WorklogPageData{WeekEnd: isoDate(weekEnd), WindowStart: start.Format("1月2日 15:04"), WindowEnd: end.Format("1月2日 15:04"), CanEdit: a.worklogEditAllowed(r.Context(), now, user)}
	data.Projects, _ = a.activeProjects(r.Context())
	data.ProjectSubitems, _ = a.activeProjectSubitemMap(r.Context(), data.Projects)
	data.LatestWorkDetails, _ = a.latestWorkContents(r.Context(), user.ID)
	if err := a.db.QueryRowContext(r.Context(), "SELECT leave_days FROM leave_records WHERE week_end=? AND user_id=?", data.WeekEnd, user.ID).Scan(&data.LeaveDays); err == nil {
		data.Submitted = true
	}
	data.Entries, _ = a.workEntries(r.Context(), user.ID, data.WeekEnd, false)
	if len(data.Entries) > 0 {
		data.Submitted = true
	}
	if len(data.Entries) == 0 && !data.Submitted && r.URL.Query().Get("cleared") != "1" {
		previous := isoDate(weekEnd.AddDate(0, 0, -7))
		data.Entries, _ = a.workEntries(r.Context(), user.ID, previous, true)
		for i := range data.Entries {
			data.Entries[i].ID = 0
			data.Entries[i].WeekEnd = data.WeekEnd
			data.Entries[i].EndParticipation = false
		}
		data.Carried = len(data.Entries) > 0
	}
	a.render(w, r, http.StatusOK, "worklog.html", PageData{Title: "本周工时填报", Data: data, Flash: r.URL.Query().Get("ok")})
}

func (a *App) latestWorkContents(ctx context.Context, userID int64) (map[int64]WorkContentFields, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT project_id,latest_work_content,COALESCE(latest_project_subitem_id,0),latest_work_subitem,latest_work_area,latest_work_structure,latest_work_role
		FROM project_participations WHERE user_id=? AND (latest_work_content<>'' OR latest_work_subitem<>'')`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	contents := map[int64]WorkContentFields{}
	for rows.Next() {
		var projectID int64
		var legacy string
		var item WorkContentFields
		if err := rows.Scan(&projectID, &legacy, &item.ProjectSubitemID, &item.Subitem, &item.Area, &item.Structure, &item.Role); err != nil {
			return nil, err
		}
		if item.LegacyText() == "" {
			item.Subitem = legacy
		}
		contents[projectID] = item
	}
	return contents, rows.Err()
}

func (a *App) workEntries(ctx context.Context, userID int64, weekEnd string, carry bool) ([]WorkEntry, error) {
	query := `SELECT a.id,a.week_end,a.user_id,COALESCE(a.project_id,0),COALESCE(a.project_subitem_id,0),COALESCE(p.code,''),COALESCE(p.name,''),COALESCE(p.short_name,''),
		a.hours,a.work_content,a.work_subitem,a.work_area,a.work_structure,a.work_role,a.work_category,a.other_description,a.end_participation
		FROM actual_work_entries a LEFT JOIN projects p ON p.id=a.project_id
		WHERE a.user_id=? AND a.week_end=?`
	if carry {
		query += ` AND a.project_id IS NOT NULL AND p.status='active' AND EXISTS (
		SELECT 1 FROM project_participations pp WHERE pp.project_id=a.project_id AND pp.user_id=a.user_id AND pp.status='active')`
	}
	query += ` ORDER BY a.id`
	rows, err := a.db.QueryContext(ctx, query, userID, weekEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []WorkEntry{}
	for rows.Next() {
		var e WorkEntry
		if err := rows.Scan(&e.ID, &e.WeekEnd, &e.UserID, &e.ProjectID, &e.ProjectSubitemID, &e.ProjectCode, &e.ProjectName, &e.ProjectShortName, &e.Hours, &e.WorkContent, &e.WorkSubitem, &e.WorkArea, &e.WorkStructure, &e.WorkRole, &e.WorkCategory, &e.OtherDescription, &e.EndParticipation); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (a *App) activeProjects(ctx context.Context) ([]Project, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,code,name,short_name,size,chief_designer,creator_user_id,COALESCE(executing_lead_user_id,0),start_date,expected_end_date,status
		FROM projects WHERE status='active' ORDER BY short_name,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.ShortName, &p.Size, &p.ChiefDesigner, &p.CreatorUserID, &p.ExecutingLeadUserID, &p.StartDate, &p.ExpectedEndDate, &p.Status); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (a *App) handleWorklogSave(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	now := time.Now().In(a.cfg.Location)
	weekEnd := a.worklogWeekEnd(r.Context(), now)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "表单格式错误", http.StatusBadRequest)
		return
	}
	if r.FormValue("action") == "clear" {
		if !user.IsSystemAdmin {
			http.Error(w, "只有系统管理员可以清空本人本周填报", http.StatusForbidden)
			return
		}
		tx, txErr := a.db.BeginTx(r.Context(), nil)
		if txErr != nil {
			http.Error(w, "清空失败", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		if _, txErr = tx.ExecContext(r.Context(), "DELETE FROM actual_work_entries WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID); txErr != nil {
			http.Error(w, "清空失败", http.StatusInternalServerError)
			return
		}
		if _, txErr = tx.ExecContext(r.Context(), "DELETE FROM leave_records WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID); txErr != nil {
			http.Error(w, "清空失败", http.StatusInternalServerError)
			return
		}
		if txErr = tx.Commit(); txErr != nil {
			http.Error(w, "清空失败", http.StatusInternalServerError)
			return
		}
		a.audit(r.Context(), &user.ID, "worklog_clear", "week", isoDate(weekEnd), "系统管理员清空本人本周实际工时和请假记录", clientIP(r))
		http.Redirect(w, r, "/worklog?cleared=1&ok="+url.QueryEscape("本人本周填报已清空"), http.StatusSeeOther)
		return
	}
	leave, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("leave_days")), 64)
	if err != nil || leave < 0 || leave*2 != float64(int(leave*2)) {
		a.worklogError(w, r, "请假天数必须按0.5天填写")
		return
	}
	formEntries, formErr := submittedWorklogEntries(r)
	if formErr != nil {
		a.worklogError(w, r, "工时明细格式不完整")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM actual_work_entries WHERE week_end=? AND user_id=?", isoDate(weekEnd), user.ID); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO leave_records(week_end,user_id,leave_days,updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(week_end,user_id) DO UPDATE SET leave_days=excluded.leave_days,updated_at=CURRENT_TIMESTAMP`, isoDate(weekEnd), user.ID, leave); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	entryCount := 0
	for i, formEntry := range formEntries {
		hoursText := strings.TrimSpace(formEntry.Hours)
		if hoursText == "" {
			continue
		}
		hours, parseErr := strconv.ParseFloat(hoursText, 64)
		if parseErr != nil || hours < 0 || hours > 168 {
			a.worklogError(w, r, "工时必须是0至168之间的数字")
			return
		}
		if hours == 0 {
			continue
		}
		other := strings.TrimSpace(formEntry.OtherDescription)
		workCategory := strings.TrimSpace(formEntry.WorkCategory)
		if workCategory != "site" {
			workCategory = "regular"
		}
		endPart := formEntry.EndParticipation
		var projectID any
		var fields WorkContentFields
		content := ""
		if formEntry.EntryType == "other" {
			workCategory = "regular"
			if other == "" {
				a.worklogError(w, r, "“其它”工作必须填写具体内容")
				return
			}
			projectID = nil
		} else {
			rawProjectID := strings.TrimSpace(formEntry.ProjectID)
			id, idErr := parseID(rawProjectID)
			if idErr != nil && strings.TrimSpace(formEntry.ProjectCode) != "" {
				idErr = tx.QueryRowContext(r.Context(), "SELECT id FROM projects WHERE code=? AND status='active'", strings.TrimSpace(formEntry.ProjectCode)).Scan(&id)
			}
			if idErr != nil {
				rawJSON := strings.TrimSpace(r.FormValue("work_entries_json"))
				slog.Warn("工时项目选择字段缺失",
					"row", i+1,
					"submission_source", formEntry.SubmissionSource,
					"project_id_present", rawProjectID != "",
					"json_present", rawJSON != "",
					"json_length", len(rawJSON),
					"project_choice_count", len(r.Form["project_choice[]"]),
					"project_id_count", len(r.Form["project_id[]"]),
				)
				a.worklogError(w, r, fmt.Sprintf("第%d项工作未选择有效项目，请重新选择", i+1))
				return
			}
			var status string
			if err = tx.QueryRowContext(r.Context(), "SELECT status FROM projects WHERE id=?", id).Scan(&status); err != nil || status != "active" {
				a.worklogError(w, r, "所选项目已不可填报")
				return
			}
			fields.ProjectSubitemID, _ = parseID(strings.TrimSpace(formEntry.ProjectSubitemID))
			if workCategory == "site" {
				fields = WorkContentFields{}
				content = "工地驻场"
				projectID = id
				participationStatus := "active"
				var endedAt any
				if endPart {
					participationStatus = "ended"
					endedAt = isoDate(weekEnd)
				}
				_, err = tx.ExecContext(r.Context(), `INSERT INTO project_participations(project_id,user_id,status,ended_at)
					VALUES (?,?,?,?) ON CONFLICT(project_id,user_id) DO UPDATE SET status=excluded.status,ended_at=excluded.ended_at`,
					id, user.ID, participationStatus, endedAt)
				if err != nil {
					http.Error(w, "保存失败", 500)
					return
				}
			} else {
				if fields.ProjectSubitemID > 0 {
					subitemErr := tx.QueryRowContext(r.Context(), "SELECT name,area,structure FROM project_subitems WHERE id=? AND project_id=? AND active=1",
						fields.ProjectSubitemID, id).Scan(&fields.Subitem, &fields.Area, &fields.Structure)
					if subitemErr != nil {
						a.worklogError(w, r, "所选子项无效，请重新选择")
						return
					}
				} else {
					fields.Subitem = strings.TrimSpace(formEntry.WorkSubitem)
					if fields.Subitem == "" {
						fields.Subitem = strings.TrimSpace(formEntry.LegacyContent)
					}
					fields.Structure = strings.TrimSpace(formEntry.WorkStructure)
					if areaText := strings.TrimSpace(formEntry.WorkArea); areaText != "" {
						fields.Area, _ = strconv.ParseFloat(areaText, 64)
					}
				}
				fields.Role = strings.TrimSpace(formEntry.WorkRole)
				var savedLegacy string
				var saved WorkContentFields
				savedErr := tx.QueryRowContext(r.Context(), `SELECT latest_work_content,COALESCE(latest_project_subitem_id,0),latest_work_subitem,latest_work_area,latest_work_structure,latest_work_role
				FROM project_participations WHERE project_id=? AND user_id=? AND (latest_work_content<>'' OR latest_work_subitem<>'')`, id, user.ID).Scan(
					&savedLegacy, &saved.ProjectSubitemID, &saved.Subitem, &saved.Area, &saved.Structure, &saved.Role)
				hadSaved := savedErr == nil
				if fields.Subitem == "" && hadSaved {
					fields = saved
					if fields.LegacyText() == "" {
						fields.Subitem = savedLegacy
					}
				}
				var catalogCount int
				_ = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM project_subitems WHERE project_id=? AND active=1", id).Scan(&catalogCount)
				if !hadSaved && catalogCount > 0 && fields.ProjectSubitemID == 0 {
					a.worklogError(w, r, "第一次填写该项目时，请从项目子项中选择本人参与的子项")
					return
				}
				if !hadSaved && fields.ProjectSubitemID > 0 && fields.Role == "" {
					a.worklogError(w, r, "第一次填写参与某项目时，请选择本人担任职责")
					return
				}
				if !hadSaved && fields.ProjectSubitemID == 0 && (fields.Subitem == "" || fields.Area <= 0 || fields.Structure == "" || fields.Role == "") {
					a.worklogError(w, r, "第一次填写参与某项目时，如果项目暂未维护子项，请完整填写子项号及名称、建筑面积、结构形式和担任职责")
					return
				}
				if fields.Subitem == "" {
					a.worklogError(w, r, "请填写该项目的子项号及子项名称")
					return
				}
				content = fields.LegacyText()
				projectID = id
				participationStatus := "active"
				var endedAt any
				if endPart {
					participationStatus = "ended"
					endedAt = isoDate(weekEnd)
				}
				_, err = tx.ExecContext(r.Context(), `INSERT INTO project_participations(
				project_id,user_id,latest_work_content,latest_project_subitem_id,latest_work_subitem,latest_work_area,latest_work_structure,latest_work_role,status,ended_at)
				VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,user_id) DO UPDATE SET
				latest_work_content=excluded.latest_work_content,latest_project_subitem_id=excluded.latest_project_subitem_id,latest_work_subitem=excluded.latest_work_subitem,
				latest_work_area=excluded.latest_work_area,latest_work_structure=excluded.latest_work_structure,
				latest_work_role=excluded.latest_work_role,status=excluded.status,ended_at=excluded.ended_at`,
					id, user.ID, content, nullableID(fields.ProjectSubitemID), fields.Subitem, fields.Area, fields.Structure, fields.Role, participationStatus, endedAt)
				if err != nil {
					http.Error(w, "保存失败", 500)
					return
				}
			}
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO actual_work_entries(
			week_end,user_id,project_id,project_subitem_id,hours,work_content,work_subitem,work_area,work_structure,work_role,work_category,other_description,end_participation)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, isoDate(weekEnd), user.ID, projectID, nullableID(fields.ProjectSubitemID), hours, content,
			fields.Subitem, fields.Area, fields.Structure, fields.Role, workCategory, other, endPart)
		if err != nil {
			http.Error(w, "保存失败", 500)
			return
		}
		entryCount++
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "保存失败", 500)
		return
	}
	a.audit(r.Context(), &user.ID, "worklog_save", "week", isoDate(weekEnd), fmt.Sprintf("保存%d项实际工时，请假%.1f天", entryCount, leave), clientIP(r))
	http.Redirect(w, r, "/worklog?ok="+url.QueryEscape("本周工时已保存；在窗口关闭前仍可继续修改"), http.StatusSeeOther)
}

func (a *App) worklogError(w http.ResponseWriter, r *http.Request, message string) {
	user := currentUser(r)
	now := time.Now().In(a.cfg.Location)
	weekEnd := a.worklogWeekEnd(r.Context(), now)
	start, end := a.reportWindow(r.Context(), weekEnd)
	data := WorklogPageData{WeekEnd: isoDate(weekEnd), WindowStart: start.Format("1月2日 15:04"), WindowEnd: end.Format("1月2日 15:04"), CanEdit: a.worklogEditAllowed(r.Context(), now, user)}
	data.Projects, _ = a.activeProjects(r.Context())
	data.ProjectSubitems, _ = a.activeProjectSubitemMap(r.Context(), data.Projects)
	if user != nil {
		data.LatestWorkDetails, _ = a.latestWorkContents(r.Context(), user.ID)
	}
	data.LeaveDays, _ = strconv.ParseFloat(strings.TrimSpace(r.FormValue("leave_days")), 64)
	data.Entries = postedWorkEntries(r)
	a.render(w, r, http.StatusBadRequest, "worklog.html", PageData{Title: "本周工时填报", Data: data, Error: message})
}

func postedWorkEntries(r *http.Request) []WorkEntry {
	formEntries, _ := submittedWorklogEntries(r)
	entries := make([]WorkEntry, 0, len(formEntries))
	for _, formEntry := range formEntries {
		entry := WorkEntry{
			WorkCategory:     strings.TrimSpace(formEntry.WorkCategory),
			WorkSubitem:      strings.TrimSpace(formEntry.WorkSubitem),
			WorkStructure:    strings.TrimSpace(formEntry.WorkStructure),
			WorkRole:         strings.TrimSpace(formEntry.WorkRole),
			OtherDescription: strings.TrimSpace(formEntry.OtherDescription),
			EndParticipation: formEntry.EndParticipation,
		}
		entry.Hours, _ = strconv.ParseFloat(strings.TrimSpace(formEntry.Hours), 64)
		entry.WorkArea, _ = strconv.ParseFloat(strings.TrimSpace(formEntry.WorkArea), 64)
		if formEntry.EntryType != "other" {
			entry.ProjectID, _ = parseID(strings.TrimSpace(formEntry.ProjectID))
			entry.ProjectSubitemID, _ = parseID(strings.TrimSpace(formEntry.ProjectSubitemID))
			if entry.ProjectID == 0 {
				entry.ProjectID = -1
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}
