package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/model"
	"gorm.io/gorm"
)

// createMaxRetries bounds how often version creation retries after losing a
// concurrent version-number allocation.
const createMaxRetries = 3

// ScheduleVersionRepository defines persistence operations for timetable
// snapshots. Versions are append-only: no update or delete of entries exists.
type ScheduleVersionRepository interface {
	Create(ctx context.Context, version *model.ScheduleVersion, entries []model.ScheduleVersionEntry) error
	List(ctx context.Context, page, pageSize int) ([]model.ScheduleVersion, int64, error)
	GetByID(ctx context.Context, id uint) (*model.ScheduleVersion, error)
	GetByIDs(ctx context.Context, ids []uint) ([]model.ScheduleVersion, error)
	GetEntries(ctx context.Context, versionID uint) ([]model.ScheduleVersionEntry, error)
	Latest(ctx context.Context) (*model.ScheduleVersion, error)
	Publish(ctx context.Context, id uint, publishedAt time.Time) error
}

type scheduleVersionRepository struct {
	db *gorm.DB
}

// NewScheduleVersionRepository constructs a schedule version repository.
func NewScheduleVersionRepository(db *gorm.DB) ScheduleVersionRepository {
	return &scheduleVersionRepository{db: db}
}

// Create persists a snapshot with all of its entries. When ctx carries a
// caller-managed transaction the snapshot joins it, so the caller's other
// writes commit or roll back together with the snapshot. Standalone creates
// run in their own transaction and retry when a concurrent insert claims the
// same version number first.
func (r *scheduleVersionRepository) Create(ctx context.Context, version *model.ScheduleVersion, entries []model.ScheduleVersionEntry) error {
	if tx, ok := txFromContext(ctx); ok {
		return r.createWithNextNumber(tx.WithContext(ctx), version, entries)
	}
	var err error
	for attempt := 0; attempt < createMaxRetries; attempt++ {
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return r.createWithNextNumber(tx, version, entries)
		})
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrConstraint) {
			return err
		}
	}
	return fmt.Errorf("create schedule version: %w", err)
}

// createWithNextNumber assigns the next sequential version number and inserts
// the snapshot with its entries inside tx.
func (r *scheduleVersionRepository) createWithNextNumber(tx *gorm.DB, version *model.ScheduleVersion, entries []model.ScheduleVersionEntry) error {
	var maxNo uint
	if err := tx.Model(&model.ScheduleVersion{}).
		Select("COALESCE(MAX(version_no), 0)").
		Scan(&maxNo).Error; err != nil {
		return fmt.Errorf("next version number: %w", err)
	}
	version.VersionNo = maxNo + 1
	if err := tx.Create(version).Error; err != nil {
		if isConstraintError(err) {
			return ErrConstraint
		}
		return fmt.Errorf("create schedule version: %w", err)
	}
	for i := range entries {
		entries[i].VersionID = version.ID
	}
	if len(entries) > 0 {
		if err := tx.CreateInBatches(entries, 200).Error; err != nil {
			return fmt.Errorf("create schedule version entries: %w", err)
		}
	}
	return nil
}

func (r *scheduleVersionRepository) List(ctx context.Context, page, pageSize int) ([]model.ScheduleVersion, int64, error) {
	var items []model.ScheduleVersion
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.ScheduleVersion{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count schedule versions: %w", err)
	}
	if err := paginate(r.db.WithContext(ctx).Model(&model.ScheduleVersion{}), page, pageSize).
		Order("version_no DESC").Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list schedule versions: %w", err)
	}
	return items, total, nil
}

func (r *scheduleVersionRepository) GetByID(ctx context.Context, id uint) (*model.ScheduleVersion, error) {
	var item model.ScheduleVersion
	if err := r.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, normalizeError(err)
	}
	return &item, nil
}

func (r *scheduleVersionRepository) GetByIDs(ctx context.Context, ids []uint) ([]model.ScheduleVersion, error) {
	items := []model.ScheduleVersion{}
	if len(ids) == 0 {
		return items, nil
	}
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("get schedule versions by ids: %w", err)
	}
	return items, nil
}

func (r *scheduleVersionRepository) GetEntries(ctx context.Context, versionID uint) ([]model.ScheduleVersionEntry, error) {
	var items []model.ScheduleVersionEntry
	if err := r.db.WithContext(ctx).
		Where("version_id = ?", versionID).
		Order("week ASC, day_of_week ASC, time_slot_id ASC, id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list schedule version entries: %w", err)
	}
	return items, nil
}

func (r *scheduleVersionRepository) Latest(ctx context.Context) (*model.ScheduleVersion, error) {
	var item model.ScheduleVersion
	if err := r.db.WithContext(ctx).Order("version_no DESC").First(&item).Error; err != nil {
		return nil, normalizeError(err)
	}
	return &item, nil
}

// Publish atomically validates and marks the target version as published. The
// transaction leads with a write so the database write lock is held before
// any check runs: concurrent publishers and generators cannot change the
// observed state until commit. At most one published version exists at any
// moment — other published versions are archived in the same transaction.
func (r *scheduleVersionRepository) Publish(ctx context.Context, id uint, publishedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Archive other published versions first. Besides enforcing the
		// single-published invariant, this write acquires the write lock up
		// front so the checks below observe a stable state.
		if err := tx.Model(&model.ScheduleVersion{}).
			Where("status = ? AND id <> ?", constants.VersionStatusPublished, id).
			Update("status", constants.VersionStatusArchived).Error; err != nil {
			return fmt.Errorf("archive published version: %w", err)
		}
		var target model.ScheduleVersion
		if err := tx.First(&target, id).Error; err != nil {
			return normalizeError(err)
		}
		if target.Status == constants.VersionStatusPublished {
			return ErrAlreadyPublished
		}
		var maxNo uint
		if err := tx.Model(&model.ScheduleVersion{}).
			Select("COALESCE(MAX(version_no), 0)").
			Scan(&maxNo).Error; err != nil {
			return fmt.Errorf("latest version number: %w", err)
		}
		if target.VersionNo != maxNo {
			return ErrNotLatest
		}
		if err := tx.Model(&model.ScheduleVersion{}).
			Where("id = ?", id).
			Updates(map[string]any{"status": constants.VersionStatusPublished, "published_at": publishedAt}).Error; err != nil {
			return fmt.Errorf("publish schedule version: %w", err)
		}
		return nil
	})
}
