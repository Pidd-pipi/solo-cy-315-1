-- 课表版本回滚：新草稿记录来源版本
ALTER TABLE schedule_versions ADD COLUMN source_version_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_schedule_versions_source ON schedule_versions(source_version_id);
