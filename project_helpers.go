package main

import (
	"fmt"
	"strings"
)

var projectStageOptions = []string{"投标", "方案设计", "初步设计", "施工图设计", "工地服务"}

func parseProjectStages(values []string) ([]string, error) {
	selected := map[string]bool{}
	for _, raw := range values {
		stage := strings.TrimSpace(raw)
		if stage == "" || selected[stage] {
			continue
		}
		valid := false
		for _, option := range projectStageOptions {
			if stage == option {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("项目阶段选择无效")
		}
		selected[stage] = true
	}
	stages := make([]string, 0, len(selected))
	for _, option := range projectStageOptions {
		if selected[option] {
			stages = append(stages, option)
		}
	}
	return stages, nil
}

func projectNeedsCompletion(p Project) bool {
	return len(p.CompletionIssues()) > 0
}

type ProjectCompletionIssue struct {
	FieldID string
	Message string
}

// CompletionIssues is shared by project status and actionable reminders.
func (p Project) CompletionIssues() []ProjectCompletionIssue {
	var issues []ProjectCompletionIssue
	if p.Code == "" {
		issues = append(issues, ProjectCompletionIssue{"project-code", "项目编号：未填写"})
	} else if strings.HasPrefix(p.Code, "INIT-") {
		issues = append(issues, ProjectCompletionIssue{"project-code", "项目编号：仍为临时编号，请改为正式编号"})
	}
	for _, field := range []struct{ id, label, value string }{
		{"project-name", "项目全名", p.Name},
		{"project-start-date", "开始日期", p.StartDate},
		{"project-end-date", "预计结束日期", p.ExpectedEndDate},
		{"project-intro-address", "项目简介 / 项目地址", p.IntroAddress},
		{"project-intro-type", "项目简介 / 项目类别", p.IntroType},
		{"project-intro-scale", "项目简介 / 项目规模", p.IntroScale},
		{"project-intro-components", "项目简介 / 子项组成", p.IntroComponents},
	} {
		if field.value == "" {
			issues = append(issues, ProjectCompletionIssue{field.id, field.label + "：未填写"})
		}
	}
	if len(p.Stages) == 0 {
		issues = append(issues, ProjectCompletionIssue{"project-stages", "项目阶段：未选择"})
	}
	return issues
}

func (p Project) HasLead(id int64) bool {
	for _, lead := range p.Leads {
		if lead.ID == id {
			return true
		}
	}
	return false
}

// SelectedProjectID keeps older form drafts renderable; JavaScript preserves
// the explicit selection on subsequent edits.
func (d WorklogPageData) SelectedProjectID() int64 {
	if len(d.Entries) == 1 {
		return d.Entries[0].ProjectID
	}
	return 0
}
