package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/model"
	"gorm.io/gorm"
)

// ScheduleVersionRepository defines persistence operations for timetable
// snapshots. Versions are append-only: no update or delete of entries exists.
type ScheduleVersionRepository interface {
	Create(ctx context.Context, version *model.ScheduleVersion, entries []model.ScheduleVersionEntry) error
	List(ctx context.Context, page, pageSize int) ([]model.ScheduleVersion, int64, error)
	GetByID(ctx context.Context, id uint) (*model.ScheduleVersion, error)
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

// Create assigns the next sequential version number and persists the snapshot
// with all of its entries in one transaction.
func (r *scheduleVersionRepository) Create(ctx context.Context, version *model.ScheduleVersion, entries []model.ScheduleVersionEntry) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxNo uint
		if err := tx.Model(&model.ScheduleVersion{}).
			Select("COALESCE(MAX(version_no), 0)").
			Scan(&maxNo).Error; err != nil {
			return fmt.Errorf("next version number: %w", err)
		}
		version.VersionNo = maxNo + 1
		if err := tx.Create(version).Error; err != nil {
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
	})
	if err != nil {
		return err
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

// Publish atomically archives the currently published version (if any) and
// marks the target version as published, guaranteeing at most one published
// version at any moment.
func (r *scheduleVersionRepository) Publish(ctx context.Context, id uint, publishedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ScheduleVersion{}).
			Where("status = ?", constants.VersionStatusPublished).
			Update("status", constants.VersionStatusArchived).Error; err != nil {
			return fmt.Errorf("archive published version: %w", err)
		}
		result := tx.Model(&model.ScheduleVersion{}).
			Where("id = ?", id).
			Updates(map[string]any{"status": constants.VersionStatusPublished, "published_at": publishedAt})
		if result.Error != nil {
			return fmt.Errorf("publish schedule version: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}
