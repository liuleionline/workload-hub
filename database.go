package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed db/schema.sql
var schemaSQL string

type DB struct {
	*sql.DB
}

func openDatabase(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(4)
	raw.SetMaxIdleConns(2)
	db := &DB{DB: raw}
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, stmt := range pragmas {
		if _, err := db.Exec(stmt); err != nil {
			raw.Close()
			return nil, fmt.Errorf("database pragma: %w", err)
		}
	}
	if err := db.migrate(); err != nil {
		raw.Close()
		return nil, err
	}
	if err := db.seedDefaults(); err != nil {
		raw.Close()
		return nil, err
	}
	if err := db.seedOfficialCalendar(); err != nil {
		raw.Close()
		return nil, err
	}
	_, _ = db.Exec("PRAGMA optimize")
	return db, nil
}

func (db *DB) migrate() error {
	for _, statement := range strings.Split(schemaSQL, "-- +statement") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}
	return db.ensureSchemaColumns()
}

func (db *DB) ensureUserProfileColumns() error {
	rows, err := db.Query("PRAGMA table_info(users)")
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	columns := []struct {
		name string
		sql  string
	}{
		{"mobile", "ALTER TABLE users ADD COLUMN mobile TEXT NOT NULL DEFAULT ''"},
		{"qualification", "ALTER TABLE users ADD COLUMN qualification TEXT NOT NULL DEFAULT ''"},
		{"professional_title", "ALTER TABLE users ADD COLUMN professional_title TEXT NOT NULL DEFAULT ''"},
		{"is_test_user", "ALTER TABLE users ADD COLUMN is_test_user INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		if !existing[column.name] {
			if _, err := db.Exec(column.sql); err != nil {
				return fmt.Errorf("add users.%s: %w", column.name, err)
			}
		}
	}
	return nil
}

func (db *DB) ensureSchemaColumns() error {
	if err := db.ensureUserProfileColumns(); err != nil {
		return err
	}
	columns := map[string][]struct {
		name string
		sql  string
	}{
		"projects": {
			{"intro_address", "ALTER TABLE projects ADD COLUMN intro_address TEXT NOT NULL DEFAULT ''"},
			{"intro_type", "ALTER TABLE projects ADD COLUMN intro_type TEXT NOT NULL DEFAULT ''"},
			{"intro_scale", "ALTER TABLE projects ADD COLUMN intro_scale TEXT NOT NULL DEFAULT ''"},
			{"intro_components", "ALTER TABLE projects ADD COLUMN intro_components TEXT NOT NULL DEFAULT ''"},
			{"intro_features", "ALTER TABLE projects ADD COLUMN intro_features TEXT NOT NULL DEFAULT ''"},
		},
		"project_participations": {
			{"latest_project_subitem_id", "ALTER TABLE project_participations ADD COLUMN latest_project_subitem_id INTEGER REFERENCES project_subitems(id)"},
			{"latest_work_subitem", "ALTER TABLE project_participations ADD COLUMN latest_work_subitem TEXT NOT NULL DEFAULT ''"},
			{"latest_work_area", "ALTER TABLE project_participations ADD COLUMN latest_work_area TEXT NOT NULL DEFAULT ''"},
			{"latest_work_structure", "ALTER TABLE project_participations ADD COLUMN latest_work_structure TEXT NOT NULL DEFAULT ''"},
			{"latest_work_role", "ALTER TABLE project_participations ADD COLUMN latest_work_role TEXT NOT NULL DEFAULT ''"},
		},
		"actual_work_entries": {
			{"project_subitem_id", "ALTER TABLE actual_work_entries ADD COLUMN project_subitem_id INTEGER REFERENCES project_subitems(id)"},
			{"work_subitem", "ALTER TABLE actual_work_entries ADD COLUMN work_subitem TEXT NOT NULL DEFAULT ''"},
			{"work_area", "ALTER TABLE actual_work_entries ADD COLUMN work_area TEXT NOT NULL DEFAULT ''"},
			{"work_structure", "ALTER TABLE actual_work_entries ADD COLUMN work_structure TEXT NOT NULL DEFAULT ''"},
			{"work_role", "ALTER TABLE actual_work_entries ADD COLUMN work_role TEXT NOT NULL DEFAULT ''"},
			{"work_category", "ALTER TABLE actual_work_entries ADD COLUMN work_category TEXT NOT NULL DEFAULT 'regular'"},
		},
	}
	for table, defs := range columns {
		existing, err := db.tableColumns(table)
		if err != nil {
			return err
		}
		for _, column := range defs {
			if !existing[column.name] {
				if _, err = db.Exec(column.sql); err != nil {
					return fmt.Errorf("add %s.%s: %w", table, column.name, err)
				}
			}
		}
	}
	return nil
}

func (db *DB) tableColumns(table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		existing[name] = true
	}
	return existing, rows.Err()
}

type permissionSeed struct {
	Code, Name, Category string
}

var permissionSeeds = []permissionSeed{
	{"dashboard.self", "查看个人看板", "看板"},
	{"dashboard.team", "查看负责项目人员看板", "看板"},
	{"dashboard.department", "查看部门看板", "看板"},
	{"dashboard.bias", "查看偏差与修正看板", "看板"},
	{"projects.create", "创建项目", "项目"},
	{"projects.view_own", "查看本人负责项目", "项目"},
	{"projects.view_all", "查看全部项目", "项目"},
	{"projects.edit_own", "编辑本人负责项目", "项目"},
	{"projects.edit_all", "编辑全部项目", "项目"},
	{"projects.archive_own", "归档本人负责项目", "项目"},
	{"projects.archive_all", "归档全部项目", "项目"},
	{"worklog.submit", "填报个人实际工时", "填报"},
	{"forecast.manage_own", "管理本人负责项目预估", "填报"},
	{"forecast.manage_all", "管理全部项目预估", "填报"},
	{"exports.self", "导出个人数据", "导出"},
	{"exports.all", "导出全部数据", "导出"},
	{"admin.users", "管理员工", "系统"},
	{"admin.permissions", "管理权限", "系统"},
	{"admin.settings", "管理系统设置", "系统"},
	{"audit.view", "查看审计日志", "系统"},
}

var defaultRolePermissions = map[string][]string{
	"designer": {"dashboard.self", "worklog.submit", "exports.self"},
	"lead": {
		"dashboard.self", "dashboard.team", "projects.create", "projects.view_own",
		"projects.edit_own", "projects.archive_own", "worklog.submit",
		"forecast.manage_own", "exports.self",
	},
	"manager": {
		"dashboard.self", "dashboard.team", "dashboard.department", "dashboard.bias",
		"projects.create", "projects.view_own", "projects.view_all", "projects.edit_own",
		"projects.edit_all", "projects.archive_own", "projects.archive_all",
		"worklog.submit", "forecast.manage_own", "forecast.manage_all",
		"exports.self", "exports.all", "audit.view",
	},
}

func (db *DB) seedDefaults() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT OR IGNORE INTO departments(name) VALUES (?)", "设计部门"); err != nil {
		return err
	}
	for _, p := range permissionSeeds {
		if _, err = tx.Exec("INSERT OR IGNORE INTO permissions(code,name,category) VALUES (?,?,?)", p.Code, p.Name, p.Category); err != nil {
			return err
		}
	}
	for role, codes := range defaultRolePermissions {
		for _, code := range codes {
			if _, err = tx.Exec("INSERT OR IGNORE INTO role_permissions(role,permission_code) VALUES (?,?)", role, code); err != nil {
				return err
			}
		}
	}
	var backgroundCount int
	if err = tx.QueryRow("SELECT COUNT(*) FROM login_backgrounds").Scan(&backgroundCount); err != nil {
		return err
	}
	if backgroundCount == 0 {
		_, err = tx.Exec(`INSERT INTO login_backgrounds(name,asset_path,mime_type) VALUES
			('载衡抽象背景·晨光','login-default-1.svg','image/svg+xml'),
			('载衡抽象背景·节奏','login-default-2.svg','image/svg+xml')`)
	}
	if err != nil {
		return err
	}
	settings := map[string]string{
		"workday_hours":           "8",
		"alert_consecutive_weeks": "3",
		"alert_hours_threshold":   "48",
		"load_thresholds":         "60,80,100,120",
		"report_open_hour":        "12",
		"report_close_hour":       "12",
		"report_open_weekday":     "5",
		"report_open_minute":      "720",
		"report_close_weekday":    "6",
		"report_close_minute":     "720",
	}
	for key, value := range settings {
		if _, err = tx.Exec("INSERT OR IGNORE INTO settings(key,value) VALUES (?,?)", key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) createInitialAdmin(ctx context.Context, name, email, password string) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("系统中已存在用户，请在管理后台新增管理员")
	}
	hash, err := hashTemporaryPassword(password)
	if err != nil {
		return err
	}
	var departmentID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM departments ORDER BY id LIMIT 1").Scan(&departmentID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO users(department_id,name,email,password_hash,role,is_system_admin,must_change_password)
		VALUES (?,?,?,?, 'manager', 1, 1)`, departmentID, name, strings.ToLower(strings.TrimSpace(email)), hash)
	return err
}
