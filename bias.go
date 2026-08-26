package main

import (
	"net/http"
	"time"
)

type BiasDashboardData struct {
	WeekEnd       string
	Metrics       []EmployeeMetric
	ValidCount    int
	AdjustedCount int
	AverageBias   float64
}

func (a *App) handleBiasDashboard(w http.ResponseWriter, r *http.Request) {
	weekEnd := a.worklogWeekEnd(r.Context(), time.Now().In(a.cfg.Location))
	if raw := r.URL.Query().Get("week_end"); raw != "" {
		if parsed, err := parseISODate(raw, a.cfg.Location); err == nil {
			weekEnd = currentWeekEnd(parsed)
		}
	}
	metrics, err := a.listEmployeeMetrics(r.Context(), weekEnd)
	if err != nil {
		http.Error(w, "偏差数据读取失败", http.StatusInternalServerError)
		return
	}
	data := BiasDashboardData{WeekEnd: isoDate(weekEnd), Metrics: metrics}
	for _, metric := range metrics {
		if metric.HasBias {
			data.ValidCount++
			data.AverageBias += metric.Bias
		}
		if metric.HasAdjusted {
			data.AdjustedCount++
		}
	}
	if data.ValidCount > 0 {
		data.AverageBias /= float64(data.ValidCount)
	}
	a.render(w, r, http.StatusOK, "bias.html", PageData{Title: "偏差与修正负荷", Data: data})
}
