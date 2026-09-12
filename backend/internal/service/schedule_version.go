package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
)

// ScheduleVersionService exposes immutable timetable snapshot operations:
// capture on generation, paginated listing, detail view, version diff and
// publish lifecycle.
type ScheduleVersionService interface {
	Snapshot(ctx context.Context, req *dto.GenerateScheduleRequest, schedules []model.Schedule) (*model.ScheduleVersion, error)
	List(ctx context.Context, page, pageSize int) ([]dto.ScheduleVersionResponse, int64, error)
	Get(ctx context.Context, id uint) (*dto.ScheduleVersionDetailResponse, error)
	Compare(ctx context.Context, fromID, toID uint) (*dto.CompareScheduleVersionsResponse, error)
	Publish(ctx context.Context, id uint) (*dto.ScheduleVersionResponse, error)
}

type scheduleVersionService struct {
	versions   repository.ScheduleVersionRepository
	classrooms repository.ClassroomRepository
	teachers   repository.TeacherRepository
	classes    repository.ClassRepository
	courses    repository.CourseRepository
	timeSlots  repository.TimeSlotRepository
	logger     *slog.Logger
}

// NewScheduleVersionService constructs a schedule version service.
func NewScheduleVersionService(
	versions repository.ScheduleVersionRepository,
	classrooms repository.ClassroomRepository,
	teachers repository.TeacherRepository,
	classes repository.ClassRepository,
	courses repository.CourseRepository,
	timeSlots repository.TimeSlotRepository,
	logger *slog.Logger,
) ScheduleVersionService {
	return &scheduleVersionService{
		versions:   versions,
		classrooms: classrooms,
		teachers:   teachers,
		classes:    classes,
		courses:    courses,
		timeSlots:  timeSlots,
		logger:     logger,
	}
}

// Snapshot persists an immutable copy of the freshly generated timetable
// together with the parameters that produced it.
func (s *scheduleVersionService) Snapshot(ctx context.Context, req *dto.GenerateScheduleRequest, schedules []model.Schedule) (*model.ScheduleVersion, error) {
	params, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal generation params: %w", err)
	}
	version := &model.ScheduleVersion{
		Semester:   req.Semester,
		Params:     string(params),
		EntryCount: len(schedules),
		Status:     constants.VersionStatusDraft,
	}
	entries := make([]model.ScheduleVersionEntry, 0, len(schedules))
	for _, sch := range schedules {
		entries = append(entries, model.ScheduleVersionEntry{
			Week:        sch.Week,
			DayOfWeek:   sch.DayOfWeek,
			TimeSlotID:  sch.TimeSlotID,
			ClassroomID: sch.ClassroomID,
			TeacherID:   sch.TeacherID,
			ClassID:     sch.ClassID,
			CourseID:    sch.CourseID,
		})
	}
	if err := s.versions.Create(ctx, version, entries); err != nil {
		return nil, fmt.Errorf("snapshot schedule version: %w", err)
	}
	return version, nil
}

func (s *scheduleVersionService) List(ctx context.Context, page, pageSize int) ([]dto.ScheduleVersionResponse, int64, error) {
	items, total, err := s.versions.List(ctx, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list schedule versions: %w", err)
	}
	out := make([]dto.ScheduleVersionResponse, 0, len(items))
	for i := range items {
		out = append(out, versionResponse(&items[i]))
	}
	return out, total, nil
}

func (s *scheduleVersionService) Get(ctx context.Context, id uint) (*dto.ScheduleVersionDetailResponse, error) {
	version, err := s.versions.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get schedule version: %w", err)
	}
	entries, err := s.versions.GetEntries(ctx, version.ID)
	if err != nil {
		return nil, fmt.Errorf("list schedule version entries: %w", err)
	}
	enriched, err := s.enrichEntries(ctx, entries)
	if err != nil {
		return nil, err
	}
	return &dto.ScheduleVersionDetailResponse{
		ScheduleVersionResponse: versionResponse(version),
		Entries:                 enriched,
	}, nil
}

func (s *scheduleVersionService) Compare(ctx context.Context, fromID, toID uint) (*dto.CompareScheduleVersionsResponse, error) {
	from, err := s.versions.GetByID(ctx, fromID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get source schedule version: %w", err)
	}
	to, err := s.versions.GetByID(ctx, toID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get target schedule version: %w", err)
	}
	fromEntries, err := s.versions.GetEntries(ctx, from.ID)
	if err != nil {
		return nil, fmt.Errorf("list source version entries: %w", err)
	}
	toEntries, err := s.versions.GetEntries(ctx, to.ID)
	if err != nil {
		return nil, fmt.Errorf("list target version entries: %w", err)
	}

	// Enrich both snapshots in one pass so shared lookups happen once.
	enrichedFrom, err := s.enrichEntries(ctx, fromEntries)
	if err != nil {
		return nil, err
	}
	enrichedTo, err := s.enrichEntries(ctx, toEntries)
	if err != nil {
		return nil, err
	}
	fromByID := make(map[uint]dto.ScheduleVersionEntryResponse, len(enrichedFrom))
	for _, e := range enrichedFrom {
		fromByID[e.ID] = e
	}
	toByID := make(map[uint]dto.ScheduleVersionEntryResponse, len(enrichedTo))
	for _, e := range enrichedTo {
		toByID[e.ID] = e
	}

	added, removed, changed := diffVersionEntries(fromEntries, toEntries)
	resp := &dto.CompareScheduleVersionsResponse{
		FromVersionID: from.ID,
		FromVersionNo: from.VersionNo,
		ToVersionID:   to.ID,
		ToVersionNo:   to.VersionNo,
		Added:         make([]dto.ScheduleVersionEntryResponse, 0, len(added)),
		Removed:       make([]dto.ScheduleVersionEntryResponse, 0, len(removed)),
		Changed:       make([]dto.ScheduleVersionEntryChange, 0, len(changed)),
	}
	for _, e := range added {
		resp.Added = append(resp.Added, toByID[e.ID])
	}
	for _, e := range removed {
		resp.Removed = append(resp.Removed, fromByID[e.ID])
	}
	for _, pair := range changed {
		fromResp := fromByID[pair.from.ID]
		toResp := toByID[pair.to.ID]
		resp.Changed = append(resp.Changed, dto.ScheduleVersionEntryChange{
			Week:       pair.to.Week,
			ClassID:    pair.to.ClassID,
			ClassName:  toResp.ClassName,
			CourseID:   pair.to.CourseID,
			CourseName: toResp.CourseName,
			TeacherID:  pair.to.TeacherID,
			From:       fromResp,
			To:         toResp,
		})
	}
	return resp, nil
}

// Publish marks a version as the single published snapshot. Only the latest
// version may be published; publishing a new version archives the previously
// published one. The repository performs the checks inside the publish
// transaction, so concurrent publishes can never breach the invariant.
func (s *scheduleVersionService) Publish(ctx context.Context, id uint) (*dto.ScheduleVersionResponse, error) {
	if err := s.versions.Publish(ctx, id, time.Now()); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, ErrNotFound
		case errors.Is(err, repository.ErrAlreadyPublished):
			return nil, ErrVersionAlreadyPublished
		case errors.Is(err, repository.ErrNotLatest):
			return nil, ErrVersionNotLatest
		default:
			return nil, fmt.Errorf("publish schedule version: %w", err)
		}
	}
	version, err := s.versions.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get published schedule version: %w", err)
	}
	resp := versionResponse(version)
	return &resp, nil
}

// enrichEntries converts raw snapshot entries into named responses by reusing
// the timetable enrichment helpers.
func (s *scheduleVersionService) enrichEntries(ctx context.Context, entries []model.ScheduleVersionEntry) ([]dto.ScheduleVersionEntryResponse, error) {
	out := make([]dto.ScheduleVersionEntryResponse, 0, len(entries))
	if len(entries) == 0 {
		return out, nil
	}
	schedules := make([]model.Schedule, 0, len(entries))
	for _, e := range entries {
		sch := model.Schedule{
			Week:        e.Week,
			DayOfWeek:   e.DayOfWeek,
			TimeSlotID:  e.TimeSlotID,
			ClassroomID: e.ClassroomID,
			TeacherID:   e.TeacherID,
			ClassID:     e.ClassID,
			CourseID:    e.CourseID,
		}
		sch.ID = e.ID
		schedules = append(schedules, sch)
	}

	slotList, _, err := s.timeSlots.List(ctx, 1, constants.MaxPageSize)
	if err != nil {
		return nil, fmt.Errorf("load time slots: %w", err)
	}
	slotMap := entityMap(slotList, func(t model.TimeSlot) uint { return t.ID })
	classroomList, err := s.classrooms.GetByIDs(ctx, uniqueClassroomIDs(schedules))
	if err != nil {
		return nil, fmt.Errorf("load classrooms: %w", err)
	}
	teacherList, err := s.teachers.GetByIDs(ctx, uniqueTeacherIDs(schedules))
	if err != nil {
		return nil, fmt.Errorf("load teachers: %w", err)
	}
	classList, err := s.classes.GetByIDs(ctx, uniqueClassIDs(schedules))
	if err != nil {
		return nil, fmt.Errorf("load classes: %w", err)
	}
	courseList, err := s.courses.GetByIDs(ctx, uniqueUint(schedules, func(sch model.Schedule) uint { return sch.CourseID }))
	if err != nil {
		return nil, fmt.Errorf("load courses: %w", err)
	}

	enriched := enrichSchedules(
		schedules,
		slotMap,
		entityMap(classroomList, func(c model.Classroom) uint { return c.ID }),
		entityMap(teacherList, func(t model.Teacher) uint { return t.ID }),
		entityMap(classList, func(c model.Class) uint { return c.ID }),
		entityMap(courseList, func(c model.Course) uint { return c.ID }),
	)
	for _, r := range enriched {
		out = append(out, dto.ScheduleVersionEntryResponse{
			ID:            r.ID,
			Week:          r.Week,
			DayOfWeek:     r.DayOfWeek,
			TimeSlotID:    r.TimeSlotID,
			TimeSlotCode:  r.TimeSlotCode,
			TimeSlotName:  r.TimeSlotName,
			StartTime:     r.StartTime,
			EndTime:       r.EndTime,
			ClassroomID:   r.ClassroomID,
			ClassroomName: r.ClassroomName,
			TeacherID:     r.TeacherID,
			TeacherName:   r.TeacherName,
			ClassID:       r.ClassID,
			ClassName:     r.ClassName,
			CourseID:      r.CourseID,
			CourseName:    r.CourseName,
		})
	}
	return out, nil
}

// entryExactKey identifies a lesson placement exactly: same week, day, slot,
// room and participants.
type entryExactKey struct {
	week        uint
	dayOfWeek   int
	timeSlotID  uint
	classroomID uint
	teacherID   uint
	classID     uint
	courseID    uint
}

// entryLessonKey identifies a lesson logically across versions: the same
// class taking the same course with the same teacher in the same week, even
// if it was rescheduled to another day, slot or classroom.
type entryLessonKey struct {
	week      uint
	classID   uint
	courseID  uint
	teacherID uint
}

type entryChangePair struct {
	from model.ScheduleVersionEntry
	to   model.ScheduleVersionEntry
}

// diffVersionEntries computes added, removed and rescheduled lessons between
// two snapshots. Entries identical in both versions are unchanged; remaining
// entries are paired by lesson identity to detect time-slot moves, and
// whatever is left over is reported as removed (source only) or added
// (target only). Output is deterministically ordered.
func diffVersionEntries(from, to []model.ScheduleVersionEntry) (added, removed []model.ScheduleVersionEntry, changed []entryChangePair) {
	exactKeyOf := func(e model.ScheduleVersionEntry) entryExactKey {
		return entryExactKey{e.Week, e.DayOfWeek, e.TimeSlotID, e.ClassroomID, e.TeacherID, e.ClassID, e.CourseID}
	}
	lessonKeyOf := func(e model.ScheduleVersionEntry) entryLessonKey {
		return entryLessonKey{e.Week, e.ClassID, e.CourseID, e.TeacherID}
	}
	sortEntries := func(items []model.ScheduleVersionEntry) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Week != items[j].Week {
				return items[i].Week < items[j].Week
			}
			if items[i].DayOfWeek != items[j].DayOfWeek {
				return items[i].DayOfWeek < items[j].DayOfWeek
			}
			if items[i].TimeSlotID != items[j].TimeSlotID {
				return items[i].TimeSlotID < items[j].TimeSlotID
			}
			if items[i].ClassID != items[j].ClassID {
				return items[i].ClassID < items[j].ClassID
			}
			if items[i].CourseID != items[j].CourseID {
				return items[i].CourseID < items[j].CourseID
			}
			return items[i].ID < items[j].ID
		})
	}

	// Phase 1: cancel out entries that are identical in both snapshots.
	unmatched := map[entryExactKey][]int{}
	for i := range from {
		k := exactKeyOf(from[i])
		unmatched[k] = append(unmatched[k], i)
	}
	fromUsed := make([]bool, len(from))
	var toLeft []model.ScheduleVersionEntry
	for _, e := range to {
		k := exactKeyOf(e)
		if bucket := unmatched[k]; len(bucket) > 0 {
			fromUsed[bucket[0]] = true
			unmatched[k] = bucket[1:]
			continue
		}
		toLeft = append(toLeft, e)
	}
	var fromLeft []model.ScheduleVersionEntry
	for i := range from {
		if !fromUsed[i] {
			fromLeft = append(fromLeft, from[i])
		}
	}

	// Phase 2: pair leftovers sharing a lesson identity; those are moves.
	sortEntries(fromLeft)
	sortEntries(toLeft)
	fromByLesson := map[entryLessonKey][]int{}
	for i := range fromLeft {
		k := lessonKeyOf(fromLeft[i])
		fromByLesson[k] = append(fromByLesson[k], i)
	}
	fromLeftUsed := make([]bool, len(fromLeft))
	for _, e := range toLeft {
		k := lessonKeyOf(e)
		if bucket := fromByLesson[k]; len(bucket) > 0 {
			idx := bucket[0]
			fromByLesson[k] = bucket[1:]
			fromLeftUsed[idx] = true
			changed = append(changed, entryChangePair{from: fromLeft[idx], to: e})
			continue
		}
		added = append(added, e)
	}
	for i := range fromLeft {
		if !fromLeftUsed[i] {
			removed = append(removed, fromLeft[i])
		}
	}
	return added, removed, changed
}

func versionResponse(v *model.ScheduleVersion) dto.ScheduleVersionResponse {
	resp := dto.ScheduleVersionResponse{
		ID:         v.ID,
		VersionNo:  v.VersionNo,
		Semester:   v.Semester,
		Status:     v.Status,
		EntryCount: v.EntryCount,
		Params:     v.Params,
		CreatedAt:  v.CreatedAt.Format(time.RFC3339),
	}
	if v.PublishedAt != nil {
		resp.PublishedAt = v.PublishedAt.Format(time.RFC3339)
	}
	return resp
}
