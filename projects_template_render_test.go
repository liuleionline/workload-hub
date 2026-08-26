package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestProjectsTemplateRendersEveryProject(t *testing.T) {
	funcs := template.FuncMap{
		"hasPerm":       func(string) bool { return true },
		"hours":         func(float64) string { return "0.0" },
		"pct":           func(float64) string { return "0%" },
		"projectStatus": func(string) string { return "进行中" },
	}
	tmpl, err := template.New("projects.html").Funcs(funcs).ParseFS(webFiles, "web/templates/projects.html")
	if err != nil {
		t.Fatal(err)
	}
	projects := []Project{
		{ID: 1, Code: "INIT-001", Name: "", ShortName: "项目一", Size: "大", ChiefDesigner: "总设计师一", ExecutingLeadName: "负责人一", Status: "active", IsIncomplete: true, CanEdit: true},
		{ID: 2, Code: "P-002", Name: "项目二全名", ShortName: "项目二", Size: "中", ChiefDesigner: "总设计师二", ExecutingLeadName: "负责人二", Status: "active", CanEdit: true},
	}
	var output bytes.Buffer
	data := PageData{Data: ProjectsPageData{Projects: projects}, CSRFToken: "test"}
	if err = tmpl.ExecuteTemplate(&output, "content", data); err != nil {
		t.Fatal(err)
	}
	if rows := strings.Count(output.String(), "<tr>") - 1; rows != len(projects) {
		t.Fatalf("rendered project rows=%d, want %d", rows, len(projects))
	}
	if !strings.Contains(output.String(), "资料待完善") {
		t.Fatal("incomplete project marker was not rendered")
	}
}
