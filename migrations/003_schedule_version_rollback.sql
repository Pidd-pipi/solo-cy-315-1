-- 课表版本回滚：新草稿记录来源版本。
-- 说明：SQLite 不支持 ADD COLUMN IF NOT EXISTS，本脚本由应用内迁移器执行，
-- 通过 schema_migrations 记录保证只应用一次；来源字段或索引已存在时安全跳过。
ALTER TABLE schedule_versions ADD COLUMN source_version_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_schedule_versions_source ON schedule_versions(source_version_id);
