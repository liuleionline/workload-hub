package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditPageDisplaysConfiguredTimezone(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "audit-timezone.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO audit_logs(action,entity_type,detail,ip_address,created_at)
		VALUES ('timezone_test','system','UTC timestamp','127.0.0.1','2026-08-12 01:30:00')`); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Config{Location: time.FixedZone("Asia/Shanghai", 8*60*60)}, db)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	request = request.WithContext(context.WithValue(request.Context(), permissionsContextKey, map[string]bool{"audit.view": true}))
	recorder := httptest.NewRecorder()
	app.handleAudit(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("audit status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "2026-08-12 09:30") {
		t.Fatalf("audit page did not convert UTC to Asia/Shanghai: %s", body)
	}
}

func TestMobileSidebarKeepsLogoutReachable(t *testing.T) {
	css, err := webFiles.ReadFile("web/static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	content := string(css)
	for _, required := range []string{"height:100dvh", "overflow-y:auto", "-webkit-overflow-scrolling:touch", "env(safe-area-inset-bottom)"} {
		if !strings.Contains(content, required) {
			t.Fatalf("mobile sidebar CSS missing %q", required)
		}
	}
}
