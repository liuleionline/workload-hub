package main

import (
	"fmt"
	"time"
)

func currentWeekEnd(now time.Time) time.Time {
	weekday := int(now.Weekday())
	delta := (int(time.Friday) - weekday + 7) % 7
	friday := time.Date(now.Year(), now.Month(), now.Day()+delta, 0, 0, 0, 0, now.Location())
	return friday
}

func parseISODate(value string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", value, loc)
}

func isoDate(t time.Time) string { return t.Format("2006-01-02") }

func weekLabel(value string, loc *time.Location) string {
	t, err := parseISODate(value, loc)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%d/%d", int(t.Month()), t.Day())
}

func reportWindow(weekEnd time.Time) (time.Time, time.Time) {
	return reportWindowWithSchedule(weekEnd, defaultReportSchedule())
}

func worklogWeekEnd(now time.Time) time.Time {
	return worklogWeekEndWithSchedule(now, defaultReportSchedule())
}

func forecastTargetWeekEnd(now time.Time) time.Time {
	return worklogWeekEnd(now).AddDate(0, 0, 7)
}
