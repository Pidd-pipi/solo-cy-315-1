package dto

// ScheduleVersionResponse is the summary of one timetable snapshot.
type ScheduleVersionResponse struct {
	ID              uint   `json:"id"`
	VersionNo       uint   `json:"version_no"`
	Semester        string `json:"semester"`
	Status          string `json:"status"`
	EntryCount      int    `json:"entry_count"`
	Params          string `json:"params"`
	PublishedAt     string `json:"published_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	SourceVersionID *uint  `json:"source_version_id,omitempty"`
	SourceVersionNo *uint  `json:"source_version_no,omitempty"`
}

// ScheduleVersionEntryResponse is one lesson inside a snapshot, enriched with
// related names the same way timetable entries are.
type ScheduleVersionEntryResponse struct {
	ID            uint   `json:"id"`
	Week          uint   `json:"week"`
	DayOfWeek     int    `json:"day_of_week"`
	TimeSlotID    uint   `json:"time_slot_id"`
	TimeSlotCode  string `json:"time_slot_code"`
	TimeSlotName  string `json:"time_slot_name"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time"`
	ClassroomID   uint   `json:"classroom_id"`
	ClassroomName string `json:"classroom_name"`
	TeacherID     uint   `json:"teacher_id"`
	TeacherName   string `json:"teacher_name"`
	ClassID       uint   `json:"class_id"`
	ClassName     string `json:"class_name"`
	CourseID      uint   `json:"course_id"`
	CourseName    string `json:"course_name"`
}

// ScheduleVersionDetailResponse is a snapshot together with its entries.
type ScheduleVersionDetailResponse struct {
	ScheduleVersionResponse
	Entries []ScheduleVersionEntryResponse `json:"entries"`
}

// CompareScheduleVersionsRequest carries the two version ids to diff.
type CompareScheduleVersionsRequest struct {
	From uint `form:"from" binding:"required,gte=1"`
	To   uint `form:"to" binding:"required,gte=1"`
}

// ScheduleVersionEntryChange describes a lesson kept in both versions whose
// time slot or classroom changed.
type ScheduleVersionEntryChange struct {
	Week       uint                         `json:"week"`
	ClassID    uint                         `json:"class_id"`
	ClassName  string                       `json:"class_name"`
	CourseID   uint                         `json:"course_id"`
	CourseName string                       `json:"course_name"`
	TeacherID  uint                         `json:"teacher_id"`
	From       ScheduleVersionEntryResponse `json:"from"`
	To         ScheduleVersionEntryResponse `json:"to"`
}

// CompareScheduleVersionsResponse reports added, removed and rescheduled
// lessons between two snapshots.
type CompareScheduleVersionsResponse struct {
	FromVersionID uint                           `json:"from_version_id"`
	FromVersionNo uint                           `json:"from_version_no"`
	ToVersionID   uint                           `json:"to_version_id"`
	ToVersionNo   uint                           `json:"to_version_no"`
	Added         []ScheduleVersionEntryResponse `json:"added"`
	Removed       []ScheduleVersionEntryResponse `json:"removed"`
	Changed       []ScheduleVersionEntryChange   `json:"changed"`
}
