package model

import (
	"time"

	"gorm.io/gorm"
)

// ScheduleVersion is an immutable snapshot of the full timetable captured at
// generation time. Snapshots are never updated or deleted; publishing only
// flips the status field. A version created by a rollback records the source
// version it was copied from.
type ScheduleVersion struct {
	gorm.Model
	VersionNo       uint       `gorm:"uniqueIndex;not null" json:"version_no"`
	Semester        string     `gorm:"size:128" json:"semester"`
	Params          string     `gorm:"type:text" json:"params"`
	EntryCount      int        `gorm:"not null" json:"entry_count"`
	Status          string     `gorm:"size:16;not null;index" json:"status"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	SourceVersionID *uint      `gorm:"index" json:"source_version_id,omitempty"`
}

// TableName explicitly names the table.
func (ScheduleVersion) TableName() string { return "schedule_versions" }

// ScheduleVersionEntry is one lesson inside a schedule version snapshot.
type ScheduleVersionEntry struct {
	ID          uint `gorm:"primarykey" json:"id"`
	VersionID   uint `gorm:"index;not null" json:"version_id"`
	Week        uint `gorm:"not null" json:"week"`
	DayOfWeek   int  `gorm:"not null" json:"day_of_week"`
	TimeSlotID  uint `gorm:"not null" json:"time_slot_id"`
	ClassroomID uint `gorm:"not null" json:"classroom_id"`
	TeacherID   uint `gorm:"not null" json:"teacher_id"`
	ClassID     uint `gorm:"not null" json:"class_id"`
	CourseID    uint `gorm:"not null" json:"course_id"`
}

// TableName explicitly names the table.
func (ScheduleVersionEntry) TableName() string { return "schedule_version_entries" }
