package main

import (
	"testing"
	"time"
)

func TestReportWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	if reportWindowOpen(time.Date(2026, 8, 7, 11, 59, 0, 0, loc)) {
		t.Fatal("Friday 11:59 should be closed")
	}
	if !reportWindowOpen(time.Date(2026, 8, 7, 12, 0, 0, 0, loc)) {
		t.Fatal("Friday 12:00 should be open")
	}
	if !reportWindowOpen(time.Date(2026, 8, 7, 15, 30, 0, 0, loc)) {
		t.Fatal("Friday 15:30 should be open")
	}
	if !reportWindowOpen(time.Date(2026, 8, 7, 23, 59, 0, 0, loc)) {
		t.Fatal("Friday 23:59 should be open")
	}
	if !reportWindowOpen(time.Date(2026, 8, 8, 11, 59, 0, 0, loc)) {
		t.Fatal("Saturday 11:59 should be open")
	}
	if reportWindowOpen(time.Date(2026, 8, 8, 12, 0, 0, 0, loc)) {
		t.Fatal("Saturday 12:00 should be closed")
	}
	if reportWindowOpen(time.Date(2026, 8, 6, 16, 0, 0, 0, loc)) {
		t.Fatal("Thursday should be closed")
	}
}

func TestSystemAdminCanEditWorklogOutsideWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	outsideWindow := time.Date(2026, 8, 6, 16, 0, 0, 0, loc)
	if !worklogEditAllowed(outsideWindow, &User{IsSystemAdmin: true}) {
		t.Fatal("system administrator should be able to edit worklog outside the reporting window")
	}
	if worklogEditAllowed(outsideWindow, &User{Role: "manager"}) {
		t.Fatal("non-system administrator should remain blocked outside the reporting window")
	}
	if !worklogEditAllowed(outsideWindow, &User{Role: "designer", IsTestUser: true}) {
		t.Fatal("test user should be able to edit worklog outside the reporting window")
	}
	if !reportEditAllowed(outsideWindow, &User{Role: "lead", IsTestUser: true}) {
		t.Fatal("test lead should be able to submit forecasts outside the reporting window")
	}
}

func TestWorklogAndForecastWeekEnds(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	friday := time.Date(2026, 8, 7, 12, 0, 0, 0, loc)
	saturdayMorning := time.Date(2026, 8, 8, 11, 59, 0, 0, loc)
	saturdayAfternoon := time.Date(2026, 8, 8, 12, 1, 0, 0, loc)
	monday := time.Date(2026, 8, 10, 9, 0, 0, 0, loc)
	for label, now := range map[string]time.Time{
		"Friday":             friday,
		"Saturday morning":   saturdayMorning,
		"Saturday afternoon": saturdayAfternoon,
		"Monday":             monday,
	} {
		if got := isoDate(worklogWeekEnd(now)); got != "2026-08-07" {
			t.Fatalf("%s worklog week end = %s, want 2026-08-07", label, got)
		}
		if got := isoDate(forecastTargetWeekEnd(now)); got != "2026-08-14" {
			t.Fatalf("%s forecast target = %s, want 2026-08-14", label, got)
		}
	}
}
