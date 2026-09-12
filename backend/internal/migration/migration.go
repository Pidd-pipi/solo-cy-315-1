// Package migration applies versioned SQL schema migrations exactly once and
// records them, so repeated runs always finish safely without touching
// existing data.
package migration

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Migration is a single ordered schema change. SQL mirrors the file of the
// same version under migrations/ and must be kept in sync with it.
type Migration struct {
	Version string
	Name    string
	SQL     string
}

// Migrations lists every schema migration in apply order.
var Migrations = []Migration{
	{Version: "001", Name: "init", SQL: initSQL},
	{Version: "002", Name: "schedule_versions", SQL: scheduleVersionsSQL},
	{Version: "003", Name: "schedule_version_rollback", SQL: scheduleVersionRollbackSQL},
}

// Apply runs every pending migration in order and records it in the
// schema_migrations table. Migrations already recorded, or whose effects are
// already present in the database (for example a column added earlier by
// AutoMigrate), are skipped without error, so Apply is always safe to re-run
// and never modifies existing rows.
func Apply(db *gorm.DB, migrations []Migration) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at DATETIME NOT NULL
	)`).Error; err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	for _, m := range migrations {
		applied, err := isApplied(db, m.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyOne(db, m); err != nil {
			return fmt.Errorf("apply migration %s_%s: %w", m.Version, m.Name, err)
		}
	}
	return nil
}

func isApplied(db *gorm.DB, version string) (bool, error) {
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count).Error; err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return count > 0, nil
}

// applyOne executes all statements of a migration in a single transaction and
// records the migration as applied. Statements whose effect already exists
// (duplicate column, existing table or index) are tolerated so pre-existing
// schema drift cannot break repeated runs.
func applyOne(db *gorm.DB, m Migration) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range splitStatements(m.SQL) {
			if err := tx.Exec(stmt).Error; err != nil {
				if !isAlreadyPresentError(err) {
					return fmt.Errorf("execute statement: %w", err)
				}
			}
		}
		if err := tx.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)", m.Version, m.Name, time.Now()).Error; err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
		return nil
	})
}

// splitStatements breaks a SQL script into individual statements, dropping
// comment lines and blank fragments.
func splitStatements(sql string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	var stmts []string
	for _, stmt := range strings.Split(b.String(), ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}

// isAlreadyPresentError reports whether err only means the statement's effect
// (column, table or index) already exists in the database.
func isAlreadyPresentError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}
