-- +statement
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS departments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    department_id INTEGER NOT NULL REFERENCES departments(id),
    name TEXT NOT NULL,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    mobile TEXT NOT NULL DEFAULT '',
    qualification TEXT NOT NULL DEFAULT '',
    professional_title TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('manager','lead','designer')),
    is_system_admin INTEGER NOT NULL DEFAULT 0,
    is_test_user INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 1,
    must_change_password INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS permissions (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL
);

-- +statement
CREATE TABLE IF NOT EXISTS role_permissions (
    role TEXT NOT NULL,
    permission_code TEXT NOT NULL REFERENCES permissions(code),
    PRIMARY KEY (role, permission_code)
);

-- +statement
CREATE TABLE IF NOT EXISTS user_permission_overrides (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission_code TEXT NOT NULL REFERENCES permissions(code),
    allowed INTEGER NOT NULL,
    changed_by INTEGER REFERENCES users(id),
    changed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, permission_code)
);

-- +statement
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    csrf_token TEXT NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS login_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL COLLATE NOCASE,
    ip_address TEXT NOT NULL,
    succeeded INTEGER NOT NULL,
    attempted_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL COLLATE NOCASE UNIQUE,
    name TEXT NOT NULL,
    short_name TEXT NOT NULL,
    size TEXT NOT NULL CHECK (size IN ('超大','大','中','小')),
    chief_designer TEXT NOT NULL,
    creator_user_id INTEGER NOT NULL REFERENCES users(id),
    executing_lead_user_id INTEGER REFERENCES users(id),
    start_date TEXT NOT NULL,
    expected_end_date TEXT NOT NULL,
    intro_address TEXT NOT NULL DEFAULT '',
    intro_type TEXT NOT NULL DEFAULT '',
    intro_scale TEXT NOT NULL DEFAULT '',
    intro_components TEXT NOT NULL DEFAULT '',
    intro_features TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','completed','archived')),
    completed_at TEXT,
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS project_leads (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    is_execution INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, user_id)
);

-- +statement
CREATE TABLE IF NOT EXISTS project_stages (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stage TEXT NOT NULL CHECK (stage IN ('投标','方案设计','初步设计','施工图设计','工地服务')),
    PRIMARY KEY (project_id, stage)
);

-- +statement
CREATE TABLE IF NOT EXISTS project_subitems (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    area REAL NOT NULL DEFAULT 0 CHECK (area >= 0),
    structure TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    active INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS project_participations (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    latest_work_content TEXT NOT NULL DEFAULT '',
    latest_work_subitem TEXT NOT NULL DEFAULT '',
    latest_work_area REAL NOT NULL DEFAULT 0,
    latest_work_structure TEXT NOT NULL DEFAULT '',
    latest_work_role TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','ended')),
    joined_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at TEXT,
    PRIMARY KEY (project_id, user_id)
);

-- +statement
CREATE TABLE IF NOT EXISTS actual_work_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    week_end TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES users(id),
    project_id INTEGER REFERENCES projects(id),
    hours REAL NOT NULL CHECK (hours >= 0 AND hours <= 168),
    work_content TEXT NOT NULL DEFAULT '',
    work_subitem TEXT NOT NULL DEFAULT '',
    work_area REAL NOT NULL DEFAULT 0,
    work_structure TEXT NOT NULL DEFAULT '',
    work_role TEXT NOT NULL DEFAULT '',
    work_category TEXT NOT NULL DEFAULT 'regular' CHECK (work_category IN ('regular','site')),
    other_description TEXT NOT NULL DEFAULT '',
    end_participation INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS leave_records (
    week_end TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES users(id),
    leave_days REAL NOT NULL DEFAULT 0 CHECK (leave_days >= 0 AND leave_days <= 7),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (week_end, user_id)
);

-- +statement
CREATE TABLE IF NOT EXISTS forecast_entries (
    target_week_end TEXT NOT NULL,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id),
    hours REAL NOT NULL CHECK (hours >= 0 AND hours <= 168),
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (target_week_end, project_id, user_id)
);

-- +statement
CREATE TABLE IF NOT EXISTS work_calendar (
    work_date TEXT PRIMARY KEY,
    work_hours REAL NOT NULL CHECK (work_hours >= 0 AND work_hours <= 24),
    label TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'admin',
    updated_by INTEGER REFERENCES users(id),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_by INTEGER REFERENCES users(id),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_user_id INTEGER REFERENCES users(id),
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE TABLE IF NOT EXISTS backup_downloads (
    backup_name TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_size INTEGER NOT NULL DEFAULT 0,
    downloaded_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (backup_name, user_id)
);

-- +statement
CREATE TABLE IF NOT EXISTS login_backgrounds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    asset_path TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT 'image/jpeg',
    image_data BLOB,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +statement
CREATE UNIQUE INDEX IF NOT EXISTS idx_login_backgrounds_asset_path
ON login_backgrounds(asset_path) WHERE asset_path<>'';

-- +statement
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- +statement
CREATE INDEX IF NOT EXISTS idx_sessions_token_expires ON sessions(token_hash, expires_at);

-- +statement
CREATE INDEX IF NOT EXISTS idx_login_attempts_email_time ON login_attempts(email, attempted_at);

-- +statement
CREATE INDEX IF NOT EXISTS idx_projects_status_creator ON projects(status, creator_user_id);

-- +statement
CREATE INDEX IF NOT EXISTS idx_project_subitems_project ON project_subitems(project_id, active, sort_order, id);

-- +statement
CREATE INDEX IF NOT EXISTS idx_actual_user_week ON actual_work_entries(user_id, week_end);

-- +statement
CREATE INDEX IF NOT EXISTS idx_actual_project_week ON actual_work_entries(project_id, week_end) WHERE project_id IS NOT NULL;

-- +statement
CREATE INDEX IF NOT EXISTS idx_forecast_user_week ON forecast_entries(user_id, target_week_end);

-- +statement
CREATE INDEX IF NOT EXISTS idx_forecast_project_week ON forecast_entries(project_id, target_week_end);

-- +statement
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);
