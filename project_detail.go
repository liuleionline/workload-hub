package main

import (
	"context"
	"net/http"
	"sort"
	"time"
)

type ProjectParticipantDetail struct {
	UserID                  int64
	Name                    string
	Role                    string
	LatestWorkContent       string
	Contents                []string
	ParticipationStatus     string
	CurrentActualHours      float64
	CurrentSiteHours        float64
	CurrentForecastHours    float64
	CurrentEffectiveHours   float64
	RecentHours             float64
	TotalHours              float64
	Bias                    float64
	PersonalLoadRate        float64
	ProjectContributionRate float64
	HasBias                 bool
	HasCorrection           bool
	HasCurrentContent       bool
}

type ProjectDetailData struct {
	Project      Project
	WeekEnd      string
	Usage        ProjectWeekUsage
	Trend        []WeekMetric
	Participants []ProjectParticipantDetail
}

func (a *App) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
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
	if !a.canViewProject(r, *project) {
		http.Error(w, "你没有查看此项目的权限", http.StatusForbidden)
		return
	}
	weekEnd := a.worklogWeekEnd(r.Context(), time.Now().In(a.cfg.Location))
	if raw := r.URL.Query().Get("week_end"); raw != "" {
		if parsed, parseErr := parseISODate(raw, a.cfg.Location); parseErr == nil {
			weekEnd = currentWeekEnd(parsed)
		}
	}
	factorCache := map[string]correctionFactorResult{}
	includeTest := currentUser(r).IsTestUser
	usages, err := a.projectUsagesForWeek(r.Context(), weekEnd, project.ID, factorCache, includeTest)
	if err != nil {
		http.Error(w, "项目负荷读取失败", http.StatusInternalServerError)
		return
	}
	usage := usages[project.ID]
	allowCorrection := currentPermissions(r)["dashboard.bias"]
	if !allowCorrection {
		usage = projectUsageWithoutCorrection(usage)
	}
	participants, err := a.projectParticipantDetails(r.Context(), project.ID, weekEnd, usage, factorCache, allowCorrection, includeTest)
	if err != nil {
		http.Error(w, "项目参与人员读取失败", http.StatusInternalServerError)
		return
	}
	project.CanEdit = a.canEditProject(r, *project)
	project.CanArchive = a.canArchiveProject(r, *project)
	project.CanDelete = a.canDeleteProject(r, *project)
	data := ProjectDetailData{
		Project:      *project,
		WeekEnd:      isoDate(weekEnd),
		Usage:        usage,
		Trend:        a.projectUsageTrend(r.Context(), project.ID, weekEnd, 12, includeTest),
		Participants: participants,
	}
	if !allowCorrection {
		data.Trend = projectTrendWithoutCorrection(data.Trend)
	}
	a.render(w, r, http.StatusOK, "project-detail.html", PageData{Title: project.ShortName + " · 项目详情", Data: data})
}

func (a *App) projectParticipantDetails(ctx context.Context, projectID int64, weekEnd time.Time, usage ProjectWeekUsage, factorCache map[string]correctionFactorResult, allowCorrection bool, includeTest bool) ([]ProjectParticipantDetail, error) {
	week := isoDate(weekEnd)
	recentStart := isoDate(weekEnd.AddDate(0, 0, -77))
	rows, err := a.db.QueryContext(ctx, `SELECT u.id,u.name,u.role,
		COALESCE(pp.latest_work_content,''),COALESCE(pp.status,''),
		COALESCE((SELECT SUM(a.hours) FROM actual_work_entries a WHERE a.project_id=? AND a.user_id=u.id AND a.week_end=?),0),
		COALESCE((SELECT SUM(a.hours) FROM actual_work_entries a WHERE a.project_id=? AND a.user_id=u.id AND a.week_end=? AND a.work_category='site'),0),
		COALESCE((SELECT SUM(f.hours) FROM forecast_entries f WHERE f.project_id=? AND f.user_id=u.id AND f.target_week_end=?),0),
		COALESCE((SELECT SUM(a.hours) FROM actual_work_entries a WHERE a.project_id=? AND a.user_id=u.id AND a.week_end BETWEEN ? AND ?),0),
		COALESCE((SELECT SUM(a.hours) FROM actual_work_entries a WHERE a.project_id=? AND a.user_id=u.id),0)
		FROM users u
		LEFT JOIN project_participations pp ON pp.project_id=? AND pp.user_id=u.id
		WHERE (?=1 OR u.is_test_user=0) AND (
			pp.user_id IS NOT NULL
			OR EXISTS (SELECT 1 FROM actual_work_entries a WHERE a.project_id=? AND a.user_id=u.id)
			OR EXISTS (SELECT 1 FROM forecast_entries f WHERE f.project_id=? AND f.user_id=u.id)
		)
		ORDER BY u.name`,
		projectID, week, projectID, week, projectID, week, projectID, recentStart, week, projectID,
		projectID, includeTest, projectID, projectID)
	if err != nil {
		return nil, err
	}
	participants := []ProjectParticipantDetail{}
	indexes := map[int64]int{}
	for rows.Next() {
		var item ProjectParticipantDetail
		if err := rows.Scan(&item.UserID, &item.Name, &item.Role, &item.LatestWorkContent, &item.ParticipationStatus,
			&item.CurrentActualHours, &item.CurrentSiteHours, &item.CurrentForecastHours, &item.RecentHours, &item.TotalHours); err != nil {
			rows.Close()
			return nil, err
		}
		item.CurrentEffectiveHours = item.CurrentActualHours
		if item.CurrentActualHours > 0 && item.CurrentForecastHours > 0 {
			item.Bias = item.CurrentActualHours / item.CurrentForecastHours
			item.HasBias = true
			if allowCorrection {
				if factor, ok := a.cachedCorrectionFactor(ctx, item.UserID, weekEnd, factorCache); ok {
					item.CurrentEffectiveHours = item.CurrentActualHours / factor
					item.HasCorrection = true
				}
			}
		}
		item.PersonalLoadRate = loadRate(item.CurrentEffectiveHours, a.availableHours(ctx, item.UserID, weekEnd))
		item.ProjectContributionRate = loadRate(item.CurrentEffectiveHours, usage.EffectiveHours)
		indexes[item.UserID] = len(participants)
		participants = append(participants, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	contentRows, err := a.db.QueryContext(ctx, `SELECT a.user_id,a.work_content
		FROM actual_work_entries a JOIN users u ON u.id=a.user_id
		WHERE a.project_id=? AND a.week_end=? AND a.work_content<>'' AND (?=1 OR u.is_test_user=0)
		ORDER BY a.id`, projectID, week, includeTest)
	if err != nil {
		return nil, err
	}
	for contentRows.Next() {
		var userID int64
		var content string
		if err := contentRows.Scan(&userID, &content); err != nil {
			contentRows.Close()
			return nil, err
		}
		index, exists := indexes[userID]
		if !exists {
			continue
		}
		seen := false
		for _, current := range participants[index].Contents {
			if current == content {
				seen = true
				break
			}
		}
		if !seen {
			participants[index].Contents = append(participants[index].Contents, content)
			participants[index].HasCurrentContent = true
		}
	}
	if err := contentRows.Close(); err != nil {
		return nil, err
	}
	for index := range participants {
		if len(participants[index].Contents) == 0 && participants[index].LatestWorkContent != "" {
			participants[index].Contents = []string{participants[index].LatestWorkContent}
		}
	}
	sort.SliceStable(participants, func(i, j int) bool {
		if participants[i].CurrentEffectiveHours != participants[j].CurrentEffectiveHours {
			return participants[i].CurrentEffectiveHours > participants[j].CurrentEffectiveHours
		}
		if participants[i].RecentHours != participants[j].RecentHours {
			return participants[i].RecentHours > participants[j].RecentHours
		}
		return participants[i].Name < participants[j].Name
	})
	return participants, nil
}
