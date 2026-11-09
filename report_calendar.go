package main

import (
	"context"
	"fmt"
	"time"
)

const reportDuration = 24 * time.Hour
const reminderDelay = 4 * time.Hour

type ReportingWindow struct {
	WeekEnd  time.Time
	Start    time.Time
	End      time.Time
	Required bool
}

func (w ReportingWindow) Open(now time.Time) bool {
	return w.Required && !now.Before(w.Start) && now.Before(w.End)
}

func (w ReportingWindow) Label() string {
	if !w.Required {
		return "填报周" + isoDate(w.WeekEnd) + "整周无工作日，无需填报，也不发送催填邮件。"
	}
	return w.Start.Format("1月2日 15:04") + "至" + w.End.Format("1月2日 15:04")
}

type reportingCalendar map[string]float64

func (c reportingCalendar) working(day time.Time) bool {
	if hours, ok := c[isoDate(day)]; ok {
		return hours > 0
	}
	return day.Weekday() >= time.Monday && day.Weekday() <= time.Friday
}

func (a *App) loadReportingCalendar(ctx context.Context) (reportingCalendar, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT work_date,work_hours FROM work_calendar")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calendar := reportingCalendar{}
	for rows.Next() {
		var date string
		var hours float64
		if err := rows.Scan(&date, &hours); err != nil {
			return nil, err
		}
		if hours < 0 || hours > 24 {
			return nil, fmt.Errorf("工作日历工时无效")
		}
		calendar[date] = hours
	}
	return calendar, rows.Err()
}

// Week identity stays Friday even when the opening is shifted. Existing rows
// are never re-keyed. Exemption uses the same Saturday-Friday capacity period.
func calendarReportWindow(weekEnd time.Time, schedule ReportSchedule, calendar reportingCalendar) (ReportingWindow, error) {
	start, _ := reportWindowWithSchedule(weekEnd, schedule)
	window := ReportingWindow{WeekEnd: weekEnd, Start: start, End: start.Add(reportDuration)}
	for day := weekEnd.AddDate(0, 0, -6); !day.After(weekEnd); day = day.AddDate(0, 0, 1) {
		if calendar.working(day) {
			window.Required = true
			break
		}
	}
	if !window.Required {
		return window, nil
	}
	for offset := 0; offset < 21; offset++ {
		day := start.AddDate(0, 0, -offset)
		if calendar.working(day) {
			window.Start, window.End = day, day.Add(reportDuration)
			return window, nil
		}
	}
	return window, fmt.Errorf("未找到可用工作日，请检查节假日与调休日历")
}

func checkedCalendarWindow(weekEnd time.Time, schedule ReportSchedule, calendar reportingCalendar) (ReportingWindow, error) {
	window, err := calendarReportWindow(weekEnd, schedule, calendar)
	if err != nil || !window.Required {
		return window, err
	}
	for _, offset := range []int{-7, 7} {
		other, err := calendarReportWindow(weekEnd.AddDate(0, 0, offset), schedule, calendar)
		if err != nil {
			return window, err
		}
		if other.Required && window.Start.Before(other.End) && other.Start.Before(window.End) {
			return window, fmt.Errorf("填报窗口重叠，请管理员检查开始时间和工作日历；暂停填报及自动提醒")
		}
	}
	return window, nil
}

func (a *App) reportingWindowForWeek(ctx context.Context, weekEnd time.Time) (ReportingWindow, error) {
	schedule, err := a.reminderSchedule(ctx)
	if err != nil {
		return ReportingWindow{}, err
	}
	calendar, err := a.loadReportingCalendar(ctx)
	if err != nil {
		return ReportingWindow{}, err
	}
	return checkedCalendarWindow(weekEnd, schedule, calendar)
}

func (a *App) reportingWindowAt(ctx context.Context, now time.Time) (ReportingWindow, error) {
	now = now.In(a.cfg.Location)
	schedule, err := a.reminderSchedule(ctx)
	if err != nil {
		return ReportingWindow{}, err
	}
	calendar, err := a.loadReportingCalendar(ctx)
	if err != nil {
		return ReportingWindow{}, err
	}
	current := currentWeekEnd(now)
	var selected ReportingWindow
	for _, offset := range []int{-7, 0, 7} {
		window, err := checkedCalendarWindow(current.AddDate(0, 0, offset), schedule, calendar)
		if err != nil {
			return selected, err
		}
		if offset == -7 || (!now.Before(window.Start) && window.Start.After(selected.Start)) {
			selected = window
		}
	}
	return selected, nil
}

func (a *App) previousRequiredReportWeek(ctx context.Context, weekEnd time.Time) time.Time {
	previous := weekEnd.AddDate(0, 0, -7)
	for i := 0; i < 8; i++ {
		window, err := a.reportingWindowForWeek(ctx, previous)
		if err != nil || window.Required {
			return previous
		}
		previous = previous.AddDate(0, 0, -7)
	}
	return previous
}
