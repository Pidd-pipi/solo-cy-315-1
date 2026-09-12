-- 课表版本快照（不可变）与发布状态
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
