package main

import (
	"context"
	"fmt"
	"time"
)

type ProjectWeekUsage struct {
	RawHours              float64
	EffectiveHours        float64
	ForecastHours         float64
	SiteHours             float64
	DepartmentCapacity    float64
	DepartmentActualHours float64
	ResourceRate          float64
	WorkShare             float64
	ParticipantCount      int
	HasCorrection         bool
}

type userProjectKey struct {
	UserID    int64
	ProjectID int64
}

type correctionFactorResult struct {
	Factor float64
	OK     bool
}

func (a *App) projectUsagesForWeek(ctx context.Context, weekEnd time.Time, projectID int64, factorCache map[string]correctionFactorResult, includeTest bool) (map[int64]ProjectWeekUsage, error) {
	week := isoDate(weekEnd)
	users, err := a.listStatUsers(ctx)
	if includeTest {
		users, err = a.listActiveUsers(ctx)
	}
	if err != nil {
		return nil, err
	}
	departmentCapacity := 0.0
	for _, user := range users {
		departmentCapacity += a.availableHours(ctx, user.ID, weekEnd)
	}
	departmentActual := 0.0
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(a.hours),0)
		FROM actual_work_entries a
		JOIN users u ON u.id=a.user_id
		LEFT JOIN projects p ON p.id=a.project_id
		LEFT JOIN users pc ON pc.id=p.creator_user_id
		WHERE a.week_end=? AND (?=1 OR (u.is_test_user=0 AND (a.project_id IS NULL OR pc.is_test_user=0)))`, week, includeTest).Scan(&departmentActual); err != nil {
		return nil, err
	}

	forecastQuery := `SELECT f.user_id,f.project_id,SUM(f.hours)
		FROM forecast_entries f
		JOIN users u ON u.id=f.user_id
		JOIN users fc ON fc.id=f.created_by
		JOIN projects p ON p.id=f.project_id
		JOIN users pc ON pc.id=p.creator_user_id
		WHERE f.target_week_end=? AND f.hours>0
			AND (?=1 OR (u.is_test_user=0 AND fc.is_test_user=0 AND pc.is_test_user=0))`
	forecastArgs := []any{week, includeTest}
	if projectID > 0 {
		forecastQuery += " AND f.project_id=?"
		forecastArgs = append(forecastArgs, projectID)
	}
	forecastQuery += " GROUP BY f.user_id,f.project_id"
	forecastRows, err := a.db.QueryContext(ctx, forecastQuery, forecastArgs...)
	if err != nil {
		return nil, err
	}
	forecasts := map[userProjectKey]float64{}
	participants := map[int64]map[int64]bool{}
	usages := map[int64]ProjectWeekUsage{}
	for forecastRows.Next() {
		var userID, currentProjectID int64
		var hours float64
		if err := forecastRows.Scan(&userID, &currentProjectID, &hours); err != nil {
			forecastRows.Close()
			return nil, err
		}
		forecasts[userProjectKey{UserID: userID, ProjectID: currentProjectID}] = hours
		usage := usages[currentProjectID]
		usage.ForecastHours += hours
		usages[currentProjectID] = usage
		if participants[currentProjectID] == nil {
			participants[currentProjectID] = map[int64]bool{}
		}
		participants[currentProjectID][userID] = true
	}
	if err := forecastRows.Close(); err != nil {
		return nil, err
	}

	actualQuery := `SELECT a.user_id,a.project_id,SUM(a.hours),SUM(CASE WHEN a.work_category='site' THEN a.hours ELSE 0 END)
		FROM actual_work_entries a
		JOIN users u ON u.id=a.user_id
		JOIN projects p ON p.id=a.project_id
		JOIN users pc ON pc.id=p.creator_user_id
		WHERE a.week_end=? AND a.project_id IS NOT NULL
			AND (?=1 OR (u.is_test_user=0 AND pc.is_test_user=0))`
	actualArgs := []any{week, includeTest}
	if projectID > 0 {
		actualQuery += " AND a.project_id=?"
		actualArgs = append(actualArgs, projectID)
	}
	actualQuery += " GROUP BY a.user_id,a.project_id"
	actualRows, err := a.db.QueryContext(ctx, actualQuery, actualArgs...)
	if err != nil {
		return nil, err
	}
	for actualRows.Next() {
		var userID, currentProjectID int64
		var rawHours float64
		var siteHours float64
		if err := actualRows.Scan(&userID, &currentProjectID, &rawHours, &siteHours); err != nil {
			actualRows.Close()
			return nil, err
		}
		usage := usages[currentProjectID]
		usage.RawHours += rawHours
		usage.SiteHours += siteHours
		effectiveHours := rawHours
		if forecasts[userProjectKey{UserID: userID, ProjectID: currentProjectID}] > 0 {
			if factor, ok := a.cachedCorrectionFactor(ctx, userID, weekEnd, factorCache); ok {
				effectiveHours = rawHours / factor
				usage.HasCorrection = true
			}
		}
		usage.EffectiveHours += effectiveHours
		usages[currentProjectID] = usage
		if participants[currentProjectID] == nil {
			participants[currentProjectID] = map[int64]bool{}
		}
		participants[currentProjectID][userID] = true
	}
	if err := actualRows.Close(); err != nil {
		return nil, err
	}

	for currentProjectID, usage := range usages {
		usage.DepartmentCapacity = departmentCapacity
		usage.DepartmentActualHours = departmentActual
		usage.ResourceRate = loadRate(usage.EffectiveHours, departmentCapacity)
		usage.WorkShare = loadRate(usage.RawHours, departmentActual)
		usage.ParticipantCount = len(participants[currentProjectID])
		usages[currentProjectID] = usage
	}
	return usages, nil
}

func (a *App) cachedCorrectionFactor(ctx context.Context, userID int64, weekEnd time.Time, cache map[string]correctionFactorResult) (float64, bool) {
	key := fmt.Sprintf("%d:%s", userID, isoDate(weekEnd))
	if cached, exists := cache[key]; exists {
		return cached.Factor, cached.OK
	}
	factor, ok := a.correctionFactorForWeek(ctx, userID, weekEnd)
	cache[key] = correctionFactorResult{Factor: factor, OK: ok}
	return factor, ok
}

func projectUsageWithoutCorrection(usage ProjectWeekUsage) ProjectWeekUsage {
	usage.EffectiveHours = usage.RawHours
	usage.ResourceRate = loadRate(usage.RawHours, usage.DepartmentCapacity)
	usage.HasCorrection = false
	return usage
}

func projectTrendWithoutCorrection(trend []WeekMetric) []WeekMetric {
	result := make([]WeekMetric, len(trend))
	copy(result, trend)
	for i := range result {
		result[i].AdjustedHours = result[i].ActualHours
		result[i].LoadRate = loadRate(result[i].ActualHours, result[i].Available)
		result[i].HasAdjusted = false
	}
	return result
}

func (a *App) projectUsageTrend(ctx context.Context, projectID int64, weekEnd time.Time, count int, includeTest bool) []WeekMetric {
	trend := make([]WeekMetric, 0, count)
	factorCache := map[string]correctionFactorResult{}
	for offset := count - 1; offset >= 0; offset-- {
		date := weekEnd.AddDate(0, 0, -7*offset)
		usages, err := a.projectUsagesForWeek(ctx, date, projectID, factorCache, includeTest)
		usage := usages[projectID]
		if err != nil {
			usage = ProjectWeekUsage{}
		}
		trend = append(trend, WeekMetric{
			WeekEnd:       isoDate(date),
			WeekLabel:     weekLabel(isoDate(date), a.cfg.Location),
			ActualHours:   usage.RawHours,
			ForecastHours: usage.ForecastHours,
			SiteHours:     usage.SiteHours,
			AdjustedHours: usage.EffectiveHours,
			Available:     usage.DepartmentCapacity,
			LoadRate:      usage.ResourceRate,
			HasAdjusted:   usage.HasCorrection,
		})
	}
	return trend
}
