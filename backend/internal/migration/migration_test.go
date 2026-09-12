package migration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/gbschedule/gbschedule/internal/migration"
)

func newMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=busy_timeout(5000)", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func columnExists(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var count int64
	if err := db.Raw(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?", table), column).Scan(&count).Error; err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return count > 0
}

func indexExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&count).Error; err != nil {
		t.Fatalf("check index %s: %v", name, err)
	}
	return count > 0
}

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&count).Error; err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return count > 0
}

func appliedVersions(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var versions []string
	if err := db.Raw("SELECT version FROM schema_migrations ORDER BY version").Scan(&versions).Error; err != nil {
		t.Fatalf("list applied versions: %v", err)
	}
	return versions
}

func TestApplyFirstRun(t *testing.T) {
	db := newMigrationTestDB(t)

	if err := migration.Apply(db, migration.Migrations); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// The rollback migration must add the source column and its index.
	if !columnExists(t, db, "schedule_versions", "source_version_id") {
		t.Fatal("expected source_version_id column after first apply")
	}
	if !indexExists(t, db, "idx_schedule_versions_source") {
		t.Fatal("expected idx_schedule_versions_source after first apply")
	}
	// Earlier migrations create the base schema too.
	for _, table := range []string{"classrooms", "schedules", "schedule_versions", "schedule_version_entries"} {
		if !tableExists(t, db, table) {
			t.Fatalf("expected table %s after first apply", table)
		}
	}
	got := appliedVersions(t, db)
	if len(got) != 3 || got[0] != "001" || got[1] != "002" || got[2] != "003" {
		t.Fatalf("expected migrations 001-003 recorded, got %v", got)
	}
}

func TestApplyRepeatRun(t *testing.T) {
	db := newMigrationTestDB(t)

	if err := migration.Apply(db, migration.Migrations); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	// Repeating the full migration set must finish safely.
	for i := 0; i < 3; i++ {
		if err := migration.Apply(db, migration.Migrations); err != nil {
			t.Fatalf("repeat apply %d: %v", i, err)
		}
	}

	if got := appliedVersions(t, db); len(got) != 3 {
		t.Fatalf("expected exactly 3 recorded migrations after repeats, got %v", got)
	}
	if !columnExists(t, db, "schedule_versions", "source_version_id") || !indexExists(t, db, "idx_schedule_versions_source") {
		t.Fatal("source column and index must survive repeated applies")
	}
}

func TestApplyPreservesExistingData(t *testing.T) {
	db := newMigrationTestDB(t)

	// Simulate a 002-era database that already holds version rows.
	if err := migration.Apply(db, migration.Migrations[:2]); err != nil {
		t.Fatalf("apply 001-002: %v", err)
	}
	now := time.Now()
	if err := db.Exec("INSERT INTO schedule_versions (created_at, updated_at, version_no, semester, params, entry_count, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		now, now, 7, "2025-2026-1", "{}", 3, "published").Error; err != nil {
		t.Fatalf("insert existing version: %v", err)
	}

	if err := migration.Apply(db, migration.Migrations); err != nil {
		t.Fatalf("apply with existing data: %v", err)
	}
	// Repeating must not destroy the row either.
	if err := migration.Apply(db, migration.Migrations); err != nil {
		t.Fatalf("repeat apply with existing data: %v", err)
	}

	var got struct {
		VersionNo       uint
		Status          string
		EntryCount      int
		SourceVersionID *uint
	}
	if err := db.Raw("SELECT version_no, status, entry_count, source_version_id FROM schedule_versions WHERE version_no = 7").Scan(&got).Error; err != nil {
		t.Fatalf("read existing version: %v", err)
	}
	if got.VersionNo != 7 || got.Status != "published" || got.EntryCount != 3 {
		t.Fatalf("existing version data corrupted: %+v", got)
	}
	if got.SourceVersionID != nil {
		t.Fatalf("pre-existing row must keep NULL source_version_id, got %v", *got.SourceVersionID)
	}
}

func TestApplyToleratesPreexistingColumn(t *testing.T) {
	db := newMigrationTestDB(t)

	// Simulate drift where the source column already exists (e.g. added by
	// AutoMigrate on an older deployment) before 003 is recorded.
	if err := migration.Apply(db, migration.Migrations[:2]); err != nil {
		t.Fatalf("apply 001-002: %v", err)
	}
	if err := db.Exec("ALTER TABLE schedule_versions ADD COLUMN source_version_id INTEGER").Error; err != nil {
		t.Fatalf("pre-add source column: %v", err)
	}

	if err := migration.Apply(db, migration.Migrations); err != nil {
		t.Fatalf("apply with pre-existing column: %v", err)
	}

	if !columnExists(t, db, "schedule_versions", "source_version_id") {
		t.Fatal("source column must exist after apply")
	}
	if !indexExists(t, db, "idx_schedule_versions_source") {
		t.Fatal("source index must still be created when the column pre-exists")
	}
	got := appliedVersions(t, db)
	if len(got) != 3 || got[2] != "003" {
		t.Fatalf("expected 003 recorded despite pre-existing column, got %v", got)
	}
}

func TestMigrationsMatchSQLFiles(t *testing.T) {
	for _, m := range migration.Migrations {
		path := filepath.Join("..", "..", "..", "migrations", m.Version+"_"+m.Name+".sql")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("migration file %s not readable: %v", path, err)
		}
		if strings.TrimSpace(string(data)) != strings.TrimSpace(m.SQL) {
			t.Errorf("migration %s SQL drifted from %s", m.Version, path)
		}
	}
}
