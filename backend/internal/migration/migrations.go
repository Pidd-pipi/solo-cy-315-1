package migration

// The SQL below mirrors the files under migrations/ exactly. The
// TestMigrationsMatchSQLFiles test keeps both copies in sync.

const initSQL = `-- 教室排课助手 initial schema (SQLite)
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS classrooms (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    capacity INTEGER NOT NULL DEFAULT 0,
    equipment TEXT
);

CREATE TABLE IF NOT EXISTS teachers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    name TEXT NOT NULL,
    employee_no TEXT NOT NULL UNIQUE,
    contact TEXT,
    subjects TEXT,
    unavailable_slots TEXT
);

CREATE TABLE IF NOT EXISTS classes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    name TEXT NOT NULL,
    student_count INTEGER NOT NULL DEFAULT 0,
    grade TEXT
);

CREATE TABLE IF NOT EXISTS courses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    name TEXT NOT NULL,
    code TEXT NOT NULL UNIQUE,
    duration INTEGER NOT NULL DEFAULT 1,
    room_type TEXT
);

CREATE TABLE IF NOT EXISTS time_slots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    start_time TEXT NOT NULL,
    end_time TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    week INTEGER NOT NULL,
    day_of_week INTEGER NOT NULL,
    time_slot_id INTEGER NOT NULL,
    classroom_id INTEGER NOT NULL,
    teacher_id INTEGER NOT NULL,
    class_id INTEGER NOT NULL,
    course_id INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_schedules_week ON schedules(week);
CREATE INDEX IF NOT EXISTS idx_schedules_day ON schedules(day_of_week);
CREATE INDEX IF NOT EXISTS idx_schedules_time_slot ON schedules(time_slot_id);
CREATE INDEX IF NOT EXISTS idx_schedules_classroom ON schedules(classroom_id);
CREATE INDEX IF NOT EXISTS idx_schedules_teacher ON schedules(teacher_id);
CREATE INDEX IF NOT EXISTS idx_schedules_class ON schedules(class_id);
CREATE INDEX IF NOT EXISTS idx_schedules_course ON schedules(course_id);

CREATE TABLE IF NOT EXISTS adjustment_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    schedule_id INTEGER,
    action TEXT NOT NULL,
    detail TEXT
);
CREATE INDEX IF NOT EXISTS idx_adjustment_logs_schedule ON adjustment_logs(schedule_id);
`

const scheduleVersionsSQL = `-- 课表版本快照（不可变）与发布状态
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schedule_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    version_no INTEGER NOT NULL UNIQUE,
    semester TEXT,
    params TEXT,
    entry_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    published_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_versions_version_no ON schedule_versions(version_no);
CREATE INDEX IF NOT EXISTS idx_schedule_versions_status ON schedule_versions(status);

CREATE TABLE IF NOT EXISTS schedule_version_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL,
    week INTEGER NOT NULL,
    day_of_week INTEGER NOT NULL,
    time_slot_id INTEGER NOT NULL,
    classroom_id INTEGER NOT NULL,
    teacher_id INTEGER NOT NULL,
    class_id INTEGER NOT NULL,
    course_id INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_schedule_version_entries_version ON schedule_version_entries(version_id);
`

const scheduleVersionRollbackSQL = `-- 课表版本回滚：新草稿记录来源版本。
-- 说明：SQLite 不支持 ADD COLUMN IF NOT EXISTS，本脚本由应用内迁移器执行，
-- 通过 schema_migrations 记录保证只应用一次；来源字段或索引已存在时安全跳过。
ALTER TABLE schedule_versions ADD COLUMN source_version_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_schedule_versions_source ON schedule_versions(source_version_id);
`
