package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func projectPermissionRequest(userID int64, permissions map[string]bool) *http.Request {
	request := httptest.NewRequest("GET", "/projects/10", nil)
	requestContext := context.WithValue(request.Context(), userContextKey, &User{ID: userID, Role: "lead"})
	requestContext = context.WithValue(requestContext, permissionsContextKey, permissions)
	return request.WithContext(requestContext)
}

func TestAssignedNonExecutingLeadCannotChangeResponsibilities(t *testing.T) {
	app := &App{}
	project := Project{
		ID:                  10,
		CreatorUserID:       1,
		ExecutingLeadUserID: 2,
		Leads:               []User{{ID: 2, Role: "lead"}, {ID: 3, Role: "lead"}},
	}
	assignedLead := projectPermissionRequest(3, map[string]bool{"projects.edit_own": true})
	if !app.canEditProject(assignedLead, project) {
		t.Fatal("assigned professional lead should be able to edit project information")
	}
	if app.canManageProjectResponsibilities(assignedLead, project) {
		t.Fatal("non-executing professional lead must not change project responsibility assignments")
	}
	executingLead := projectPermissionRequest(2, map[string]bool{"projects.edit_own": true})
	if !app.canManageProjectResponsibilities(executingLead, project) {
		t.Fatal("executing professional lead should be able to change project responsibility assignments")
	}
}
