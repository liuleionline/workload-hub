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
	return p.Code == "" || len(p.Code) >= 5 && p.Code[:5] == "INIT-" || p.Name == "" || len(p.Stages) == 0 ||
		p.StartDate == "" || p.ExpectedEndDate == "" || p.IntroAddress == "" || p.IntroType == "" ||
		p.IntroScale == "" || p.IntroComponents == ""
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
