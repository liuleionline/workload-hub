package main

import (
	"context"
	"time"
)

type WorklogSubmissionStatus struct {
	WeekEnd   string
	Total     int
	Submitted int
	Missing   []User
	Error     string
}

func (a *App) worklogSubmissionStatus(ctx context.Context, now time.Time) (*WorklogSubmissionStatus, error) {
	return a.worklogSubmissionStatusForWeek(ctx, isoDate(a.worklogWeekEnd(ctx, now)))
}

func (a *App) worklogSubmissionStatusForWeek(ctx context.Context, weekEnd string) (*WorklogSubmissionStatus, error) {
	status := &WorklogSubmissionStatus{WeekEnd: weekEnd}
	rows, err := a.db.QueryContext(ctx, `SELECT u.id,u.name,
		EXISTS(SELECT 1 FROM actual_work_entries a WHERE a.user_id=u.id AND a.week_end=?) OR
		EXISTS(SELECT 1 FROM leave_records l WHERE l.user_id=u.id AND l.week_end=?)
		FROM users u WHERE u.active=1 AND u.is_test_user=0 ORDER BY u.name,u.id`, status.WeekEnd, status.WeekEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user User
		var submitted bool
		if err := rows.Scan(&user.ID, &user.Name, &submitted); err != nil {
			return nil, err
		}
		status.Total++
		if submitted {
			status.Submitted++
		} else {
			status.Missing = append(status.Missing, user)
		}
	}
	return status, rows.Err()
}
