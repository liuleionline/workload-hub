package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type worklogFormEntry struct {
	SubmissionSource string `json:"-"`
	EntryType        string `json:"entry_type"`
	WorkCategory     string `json:"work_category"`
	ProjectID        string `json:"project_id"`
	ProjectSubitemID string `json:"project_subitem_id"`
	ProjectCode      string `json:"project_code"`
	Hours            string `json:"hours"`
	LegacyContent    string `json:"work_content"`
	WorkSubitem      string `json:"work_subitem"`
	WorkArea         string `json:"work_area"`
	WorkStructure    string `json:"work_structure"`
	WorkRole         string `json:"work_role"`
	OtherDescription string `json:"other_description"`
	EndParticipation bool   `json:"end_participation"`
}

func submittedWorklogEntries(r *http.Request) ([]worklogFormEntry, error) {
	if raw := strings.TrimSpace(r.FormValue("work_entries_json")); raw != "" {
		var entries []worklogFormEntry
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, fmt.Errorf("工时明细格式不完整")
		}
		for i := range entries {
			entries[i].SubmissionSource = "json"
		}
		entries = mergeNativeWorklogEntries(entries, r)
		return entries, nil
	}

	types := r.Form["entry_type[]"]
	hours := r.Form["hours[]"]
	if len(types) != len(hours) {
		return nil, fmt.Errorf("工时明细格式不完整")
	}
	projectChoices := r.Form["project_choice[]"]
	projectIDs := r.Form["project_id[]"]
	entries := make([]worklogFormEntry, 0, len(types))
	for i, entryType := range types {
		projectID := strings.TrimSpace(valueAt(projectChoices, i))
		if projectID == "" {
			projectID = strings.TrimSpace(valueAt(projectIDs, i))
		}
		entries = append(entries, worklogFormEntry{
			SubmissionSource: "form",
			EntryType:        entryType,
			WorkCategory:     valueAt(r.Form["work_category[]"], i),
			ProjectID:        projectID,
			ProjectSubitemID: valueAt(r.Form["project_subitem_id[]"], i),
			ProjectCode:      valueAt(r.Form["project_code[]"], i),
			Hours:            valueAt(hours, i),
			LegacyContent:    valueAt(r.Form["work_content[]"], i),
			WorkSubitem:      valueAt(r.Form["work_subitem[]"], i),
			WorkArea:         valueAt(r.Form["work_area[]"], i),
			WorkStructure:    valueAt(r.Form["work_structure[]"], i),
			WorkRole:         valueAt(r.Form["work_role[]"], i),
			OtherDescription: valueAt(r.Form["other_description[]"], i),
			EndParticipation: valueAt(r.Form["end_participation[]"], i) == "1",
		})
	}
	return entries, nil
}
func mergeNativeWorklogEntries(entries []worklogFormEntry, r *http.Request) []worklogFormEntry {
	types := r.Form["entry_type[]"]
	hours := r.Form["hours[]"]
	if len(entries) == 0 || len(types) != len(entries) || len(hours) != len(entries) {
		return entries
	}
	projectChoices := r.Form["project_choice[]"]
	projectIDs := r.Form["project_id[]"]
	for i := range entries {
		if value := strings.TrimSpace(valueAt(types, i)); value != "" {
			entries[i].EntryType = value
		}
		entries[i].Hours = valueAt(hours, i)
		projectID := strings.TrimSpace(valueAt(projectChoices, i))
		if projectID == "" {
			projectID = strings.TrimSpace(valueAt(projectIDs, i))
		}
		if projectID != "" {
			entries[i].ProjectID = projectID
		}
		if values := r.Form["work_category[]"]; len(values) == len(entries) {
			entries[i].WorkCategory = valueAt(values, i)
		}
		if values := r.Form["project_subitem_id[]"]; len(values) == len(entries) {
			entries[i].ProjectSubitemID = valueAt(values, i)
		}
		if values := r.Form["work_content[]"]; len(values) == len(entries) {
			entries[i].LegacyContent = valueAt(values, i)
		}
		if values := r.Form["work_subitem[]"]; len(values) == len(entries) {
			entries[i].WorkSubitem = valueAt(values, i)
		}
		if values := r.Form["work_area[]"]; len(values) == len(entries) {
			entries[i].WorkArea = valueAt(values, i)
		}
		if values := r.Form["work_structure[]"]; len(values) == len(entries) {
			entries[i].WorkStructure = valueAt(values, i)
		}
		if values := r.Form["work_role[]"]; len(values) == len(entries) {
			entries[i].WorkRole = valueAt(values, i)
		}
		if values := r.Form["other_description[]"]; len(values) == len(entries) {
			entries[i].OtherDescription = valueAt(values, i)
		}
		if values := r.Form["end_participation[]"]; len(values) == len(entries) {
			entries[i].EndParticipation = valueAt(values, i) == "1"
		}
	}
	return entries
}
