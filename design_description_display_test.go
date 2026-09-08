package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesignDescriptionAppearsInDetailsReportsAndExcel(t *testing.T) {
	app, db, user, weekEnd := newWorklogBugfixApp(t)
	projectID := createDesignDescriptionProject(t, db, user)
	week := isoDate(weekEnd)
	if _, err := db.Exec(`INSERT INTO actual_work_entries(
		week_end,user_id,project_id,hours,work_content,work_subitem,work_area,work_structure,work_role,design_description)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, week, user.ID, projectID, 8, "101生产厂房（10000平米混凝土框架结构）独立设计", "101生产厂房", 10000, "混凝土框架结构", "独立设计", "基础设计"); err != nil {
		t.Fatal(err)
	}

	employeeProjects, _, err := app.employeeProjectWork(context.Background(), user.ID, week)
	if err != nil {
		t.Fatal(err)
	}
	if len(employeeProjects) != 1 || !strings.Contains(strings.Join(employeeProjects[0].Contents, "\n"), "本周设计内容：基础设计") {
		t.Fatalf("employee detail contents=%+v", employeeProjects)
	}

	usageByProject, err := app.projectUsagesForWeek(context.Background(), weekEnd, projectID, map[string]correctionFactorResult{}, false)
	if err != nil {
		t.Fatal(err)
	}
	projectPeople, err := app.projectParticipantDetails(context.Background(), projectID, weekEnd, usageByProject[projectID], map[string]correctionFactorResult{}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projectPeople) != 1 || !strings.Contains(strings.Join(projectPeople[0].Contents, "\n"), "本周设计内容：基础设计") {
		t.Fatalf("project detail contents=%+v", projectPeople)
	}

	request := httptest.NewRequest(http.MethodGet, "/reports?period=week&week_end="+week, nil)
	requestContext := context.WithValue(request.Context(), userContextKey, &user)
	requestContext = context.WithValue(requestContext, permissionsContextKey, map[string]bool{"exports.all": true})
	requestContext = context.WithValue(requestContext, csrfContextKey, "test-csrf")
	request = request.WithContext(requestContext)
	report := httptest.NewRecorder()
	app.handleReports(report, request)
	if report.Code != http.StatusOK || !strings.Contains(report.Body.String(), "简述设计内容") || !strings.Contains(report.Body.String(), "基础设计") {
		t.Fatalf("report status=%d body=%s", report.Code, report.Body.String())
	}

	excel := httptest.NewRecorder()
	app.handleExcelExport(excel, request)
	if excel.Code != http.StatusOK {
		t.Fatalf("excel status=%d body=%s", excel.Code, excel.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(excel.Body.Bytes()), int64(excel.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var sheet []byte
	for _, file := range zr.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		sheet, err = io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Contains(sheet, []byte("简述设计内容")) || !bytes.Contains(sheet, []byte("基础设计")) {
		t.Fatalf("xlsx sheet missing design description: %s", sheet)
	}
}
