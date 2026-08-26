package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"testing"
)

func TestChangedTemplatesParse(t *testing.T) {
	funcs := template.FuncMap{
		"hasPerm": func(string) bool { return true },
		"hours":   func(v float64) string { return fmt.Sprintf("%.1f", v) },
		"pct":     func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) },
		"json": func(v any) template.JS {
			encoded, _ := json.Marshal(v)
			return template.JS(encoded)
		},
		"roleName":      func(role string) string { return (User{Role: role}).RoleName() },
		"projectStatus": func(status string) string { return status },
		"seq":           func(n int) []int { return make([]int, n) },
	}
	for _, page := range []string{"login.html", "projects.html", "project-form.html", "users.html", "user-edit.html", "dashboard.html", "department.html", "bias.html", "employee-detail.html", "project-detail.html", "period-employees.html", "period-projects.html", "backups.html", "backgrounds.html", "worklog.html", "reports.html"} {
		if _, err := template.New("base.html").Funcs(funcs).ParseFS(webFiles, "web/templates/base.html", "web/templates/"+page); err != nil {
			t.Fatalf("parse %s: %v", page, err)
		}
	}
}
