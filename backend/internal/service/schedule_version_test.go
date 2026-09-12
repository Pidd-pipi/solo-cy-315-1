package service_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/service"
)

type versionFixture struct {
	slot1   *model.TimeSlot
	slot2   *model.TimeSlot
	room    *model.Classroom
	teacher *model.Teacher
	class   *model.Class
	course1 *model.Course
	course2 *model.Course
	course3 *model.Course
}

func seedVersionFixture(t *testing.T, db *gorm.DB) versionFixture {
	t.Helper()
	f := versionFixture{
		slot1:   &model.TimeSlot{Code: "1", Name: "第一节", StartTime: "08:00", EndTime: "09:40"},
		slot2:   &model.TimeSlot{Code: "2", Name: "第二节", StartTime: "10:00", EndTime: "11:40"},
		room:    &model.Classroom{Code: "R301", Name: "301教室", Capacity: 50},
		teacher: &model.Teacher{Name: "张老师", EmployeeNo: "T001", Subjects: []string{"数学"}},
		class:   &model.Class{Name: "一班", StudentCount: 40, Grade: "高一"},
		course1: &model.Course{Name: "数学", Code: "MATH", Duration: 1},
		course2: &model.Course{Name: "语文", Code: "CHN", Duration: 1},
		course3: &model.Course{Name: "英语", Code: "ENG", Duration: 1},
	}
	for _, item := range []any{f.slot1, f.slot2, f.room, f.teacher, f.class, f.course1, f.course2, f.course3} {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}
	return f
}

func generateRequest(weeks int, f versionFixture) *dto.GenerateScheduleRequest {
	return &dto.GenerateScheduleRequest{
		Semester:      "2025-2026-1",
		Weeks:         weeks,
		DaysPerWeek:   1,
		PeriodsPerDay: 1,
		Courses: []dto.CourseRequirement{
			{CourseID: f.course1.ID, WeeklyPeriods: 1, ClassID: f.class.ID, TeacherID: f.teacher.ID},
		},
		TeacherIDs:   []uint{f.teacher.ID},
		ClassIDs:     []uint{f.class.ID},
		ClassroomIDs: []uint{f.room.ID},
	}
}

func TestScheduleVersionSnapshotOnGenerate(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)

	svc := newScheduleService(t, db)
	resp, err := svc.Generate(ctx, generateRequest(1, f))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if resp.VersionID == 0 || resp.VersionNo != 1 {
		t.Fatalf("expected first snapshot version_no=1, got id=%d no=%d", resp.VersionID, resp.VersionNo)
	}

	if _, err := svc.Generate(ctx, generateRequest(1, f)); err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	items, total, err := versions.List(ctx, 1, 1)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 versions, got %d", total)
	}
	if len(items) != 1 {
		t.Fatalf("expected page size 1, got %d items", len(items))
	}
	if items[0].VersionNo != 2 {
		t.Fatalf("expected versions ordered by version_no desc, got %d", items[0].VersionNo)
	}
	if items[0].Status != constants.VersionStatusDraft {
		t.Fatalf("expected draft status, got %s", items[0].Status)
	}
	if items[0].Params == "" {
		t.Fatal("expected generation params to be recorded")
	}

	detail, err := versions.Get(ctx, resp.VersionID)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if detail.EntryCount != resp.Generated || len(detail.Entries) != resp.Generated {
		t.Fatalf("expected %d snapshot entries, got count=%d entries=%d", resp.Generated, detail.EntryCount, len(detail.Entries))
	}
	entry := detail.Entries[0]
	if entry.CourseName != "数学" || entry.ClassName != "一班" || entry.TeacherName != "张老师" || entry.ClassroomName != "301教室" {
		t.Fatalf("expected enriched entry names, got %+v", entry)
	}

	if _, err := versions.Get(ctx, 9999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing version, got %v", err)
	}
}

func TestScheduleVersionImmutableAfterRegenerate(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)

	svc := newScheduleService(t, db)
	first, err := svc.Generate(ctx, generateRequest(2, f))
	if err != nil {
		t.Fatalf("generate 2 weeks: %v", err)
	}
	if _, err := svc.Generate(ctx, generateRequest(1, f)); err != nil {
		t.Fatalf("generate 1 week: %v", err)
	}

	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	detail, err := versions.Get(ctx, first.VersionID)
	if err != nil {
		t.Fatalf("get first version: %v", err)
	}
	if len(detail.Entries) != first.Generated {
		t.Fatalf("snapshot must stay immutable after regeneration: want %d entries, got %d", first.Generated, len(detail.Entries))
	}
}

func TestScheduleVersionCompare(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	v1, err := versions.Snapshot(ctx, generateRequest(1, f), []model.Schedule{
		{Week: 1, DayOfWeek: 1, TimeSlotID: f.slot1.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course1.ID},
		{Week: 1, DayOfWeek: 2, TimeSlotID: f.slot1.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course2.ID},
	})
	if err != nil {
		t.Fatalf("snapshot v1: %v", err)
	}
	v2, err := versions.Snapshot(ctx, generateRequest(1, f), []model.Schedule{
		// course1 rescheduled to another day and slot.
		{Week: 1, DayOfWeek: 3, TimeSlotID: f.slot2.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course1.ID},
		// course3 is new; course2 disappeared.
		{Week: 1, DayOfWeek: 1, TimeSlotID: f.slot1.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course3.ID},
	})
	if err != nil {
		t.Fatalf("snapshot v2: %v", err)
	}

	diff, err := versions.Compare(ctx, v1.ID, v2.ID)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if diff.FromVersionNo != 1 || diff.ToVersionNo != 2 {
		t.Fatalf("unexpected diff version numbers: %+v", diff)
	}
	if len(diff.Added) != 1 || diff.Added[0].CourseID != f.course3.ID {
		t.Fatalf("expected course3 added, got %+v", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].CourseID != f.course2.ID {
		t.Fatalf("expected course2 removed, got %+v", diff.Removed)
	}
	if len(diff.Changed) != 1 {
		t.Fatalf("expected 1 rescheduled lesson, got %+v", diff.Changed)
	}
	change := diff.Changed[0]
	if change.CourseID != f.course1.ID || change.ClassID != f.class.ID {
		t.Fatalf("unexpected changed lesson: %+v", change)
	}
	if change.From.DayOfWeek != 1 || change.From.TimeSlotID != f.slot1.ID {
		t.Fatalf("unexpected change source: %+v", change.From)
	}
	if change.To.DayOfWeek != 3 || change.To.TimeSlotID != f.slot2.ID {
		t.Fatalf("unexpected change target: %+v", change.To)
	}

	same, err := versions.Compare(ctx, v1.ID, v1.ID)
	if err != nil {
		t.Fatalf("compare same version: %v", err)
	}
	if len(same.Added) != 0 || len(same.Removed) != 0 || len(same.Changed) != 0 {
		t.Fatalf("comparing a version with itself must yield an empty diff, got %+v", same)
	}

	if _, err := versions.Compare(ctx, v1.ID, 9999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound comparing with missing version, got %v", err)
	}
}

func TestScheduleVersionConcurrentPublish(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	const count = 5
	ids := make([]uint, 0, count)
	for i := 0; i < count; i++ {
		v, err := versions.Snapshot(ctx, generateRequest(1, f), nil)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		ids = append(ids, v.ID)
	}
	latestID := ids[count-1]

	// Hammer publish from many goroutines: old versions must always fail,
	// the latest must succeed exactly once, and no internal error may occur.
	var wg sync.WaitGroup
	var mu sync.Mutex
	successByID := map[uint]int{}
	var errs []error
	for _, id := range ids {
		for r := 0; r < 3; r++ {
			wg.Add(1)
			go func(id uint) {
				defer wg.Done()
				_, err := versions.Publish(ctx, id)
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					successByID[id]++
				} else {
					errs = append(errs, err)
				}
			}(id)
		}
	}
	wg.Wait()

	for _, id := range ids {
		want := 0
		if id == latestID {
			want = 1
		}
		if successByID[id] != want {
			t.Fatalf("version %d: expected %d successful publishes, got %d", id, want, successByID[id])
		}
	}
	for _, err := range errs {
		if !errors.Is(err, service.ErrVersionAlreadyPublished) && !errors.Is(err, service.ErrVersionNotLatest) {
			t.Fatalf("publish must fail with a clear domain error, got %v", err)
		}
	}

	// Under concurrent pressure, exactly one published version exists.
	items, _, err := versions.List(ctx, 1, 50)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	published := 0
	for _, item := range items {
		if item.Status == constants.VersionStatusPublished {
			published++
			if item.ID != latestID {
				t.Fatalf("only the latest version may stay published, got id=%d", item.ID)
			}
		}
	}
	if published != 1 {
		t.Fatalf("expected exactly one published version, got %d", published)
	}
}

func TestScheduleVersionPublishLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	if _, err := versions.Publish(ctx, 9999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound publishing missing version, got %v", err)
	}

	v1, err := versions.Snapshot(ctx, generateRequest(1, f), nil)
	if err != nil {
		t.Fatalf("snapshot v1: %v", err)
	}
	v2, err := versions.Snapshot(ctx, generateRequest(1, f), nil)
	if err != nil {
		t.Fatalf("snapshot v2: %v", err)
	}

	if _, err := versions.Publish(ctx, v1.ID); !errors.Is(err, service.ErrVersionNotLatest) {
		t.Fatalf("expected ErrVersionNotLatest publishing old version, got %v", err)
	}

	published, err := versions.Publish(ctx, v2.ID)
	if err != nil {
		t.Fatalf("publish latest: %v", err)
	}
	if published.Status != constants.VersionStatusPublished || published.PublishedAt == "" {
		t.Fatalf("expected published version with timestamp, got %+v", published)
	}

	if _, err := versions.Publish(ctx, v2.ID); !errors.Is(err, service.ErrVersionAlreadyPublished) {
		t.Fatalf("expected ErrVersionAlreadyPublished on repeated publish, got %v", err)
	}

	v3, err := versions.Snapshot(ctx, generateRequest(1, f), nil)
	if err != nil {
		t.Fatalf("snapshot v3: %v", err)
	}
	if _, err := versions.Publish(ctx, v3.ID); err != nil {
		t.Fatalf("publish v3: %v", err)
	}

	old, err := versions.Get(ctx, v2.ID)
	if err != nil {
		t.Fatalf("get v2: %v", err)
	}
	if old.Status != constants.VersionStatusArchived {
		t.Fatalf("expected superseded version archived, got %s", old.Status)
	}

	items, total, err := versions.List(ctx, 1, 20)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 versions, got %d", total)
	}
	publishedCount := 0
	for _, item := range items {
		if item.Status == constants.VersionStatusPublished {
			publishedCount++
			if item.ID != v3.ID {
				t.Fatalf("only the newest version may stay published, got id=%d", item.ID)
			}
		}
	}
	if publishedCount != 1 {
		t.Fatalf("expected exactly one published version, got %d", publishedCount)
	}
}

// entryKeySet flattens enriched entries into comparable lesson placement keys.
func entryKeySet(entries []dto.ScheduleVersionEntryResponse) map[string]int {
	out := map[string]int{}
	for _, e := range entries {
		key := fmt.Sprintf("%d-%d-%d-%d-%d-%d-%d", e.Week, e.DayOfWeek, e.TimeSlotID, e.ClassroomID, e.TeacherID, e.ClassID, e.CourseID)
		out[key]++
	}
	return out
}

func TestScheduleVersionRollback(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	v1, err := versions.Snapshot(ctx, generateRequest(1, f), []model.Schedule{
		{Week: 1, DayOfWeek: 1, TimeSlotID: f.slot1.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course1.ID},
		{Week: 1, DayOfWeek: 2, TimeSlotID: f.slot2.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course2.ID},
	})
	if err != nil {
		t.Fatalf("snapshot v1: %v", err)
	}
	v2, err := versions.Snapshot(ctx, generateRequest(1, f), []model.Schedule{
		{Week: 1, DayOfWeek: 1, TimeSlotID: f.slot1.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course3.ID},
	})
	if err != nil {
		t.Fatalf("snapshot v2: %v", err)
	}

	// Roll back to v1: a new draft with identical content appears.
	rb, err := versions.Rollback(ctx, v1.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rb.VersionNo != 3 || rb.Status != constants.VersionStatusDraft || rb.EntryCount != 2 {
		t.Fatalf("unexpected rollback version: %+v", rb)
	}
	if rb.SourceVersionID == nil || *rb.SourceVersionID != v1.ID {
		t.Fatalf("expected source version id %d, got %+v", v1.ID, rb.SourceVersionID)
	}
	if rb.SourceVersionNo == nil || *rb.SourceVersionNo != v1.VersionNo {
		t.Fatalf("expected source version no %d, got %+v", v1.VersionNo, rb.SourceVersionNo)
	}

	// The draft's entries and params are identical to the source version.
	detail, err := versions.Get(ctx, rb.ID)
	if err != nil {
		t.Fatalf("get rollback version: %v", err)
	}
	source, err := versions.Get(ctx, v1.ID)
	if err != nil {
		t.Fatalf("get source version: %v", err)
	}
	if !reflect.DeepEqual(entryKeySet(source.Entries), entryKeySet(detail.Entries)) {
		t.Fatalf("rollback content mismatch: source %+v vs rollback %+v", entryKeySet(source.Entries), entryKeySet(detail.Entries))
	}
	if detail.Params != source.Params {
		t.Fatalf("rollback params mismatch: %s vs %s", source.Params, detail.Params)
	}

	// Rollback only appends: the source and other versions stay untouched.
	if source.Status != constants.VersionStatusDraft || len(source.Entries) != 2 {
		t.Fatalf("source version must stay untouched, got status=%s entries=%d", source.Status, len(source.Entries))
	}
	other, err := versions.Get(ctx, v2.ID)
	if err != nil {
		t.Fatalf("get v2: %v", err)
	}
	if other.Status != constants.VersionStatusDraft || len(other.Entries) != 1 {
		t.Fatalf("other version must stay untouched, got status=%s entries=%d", other.Status, len(other.Entries))
	}

	// Repeated rollback creates another distinct draft with a unique number.
	rb2, err := versions.Rollback(ctx, v1.ID)
	if err != nil {
		t.Fatalf("repeated rollback: %v", err)
	}
	if rb2.ID == rb.ID || rb2.VersionNo != 4 {
		t.Fatalf("repeated rollback must yield a new version, got id=%d no=%d", rb2.ID, rb2.VersionNo)
	}

	_, total, err := versions.List(ctx, 1, 20)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected 4 versions after two rollbacks, got %d", total)
	}
}

func TestScheduleVersionRollbackNotFound(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	if _, err := versions.Rollback(ctx, 9999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound rolling back a missing version, got %v", err)
	}
}

func TestScheduleVersionConcurrentRollback(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	v1, err := versions.Snapshot(ctx, generateRequest(1, f), []model.Schedule{
		{Week: 1, DayOfWeek: 1, TimeSlotID: f.slot1.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course1.ID},
		{Week: 1, DayOfWeek: 2, TimeSlotID: f.slot2.ID, ClassroomID: f.room.ID, TeacherID: f.teacher.ID, ClassID: f.class.ID, CourseID: f.course2.ID},
	})
	if err != nil {
		t.Fatalf("snapshot v1: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	versionNos := make([]uint, workers)
	versionIDs := make([]uint, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rb, err := versions.Rollback(ctx, v1.ID)
			if err != nil {
				errs[i] = err
				return
			}
			versionNos[i] = rb.VersionNo
			versionIDs[i] = rb.ID
		}(i)
	}
	wg.Wait()

	// Every request gets a definite result; version numbers stay unique.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("rollback %d failed: %v", i, err)
		}
	}
	seen := map[uint]bool{}
	for _, no := range versionNos {
		if no < 2 || no > workers+1 || seen[no] {
			t.Fatalf("duplicate or out-of-range version numbers: %v", versionNos)
		}
		seen[no] = true
	}

	// Each rollback produced a complete draft copy of the source.
	for _, id := range versionIDs {
		detail, err := versions.Get(ctx, id)
		if err != nil {
			t.Fatalf("get rollback version %d: %v", id, err)
		}
		if detail.Status != constants.VersionStatusDraft || len(detail.Entries) != 2 {
			t.Fatalf("incomplete rollback version %d: status=%s entries=%d", id, detail.Status, len(detail.Entries))
		}
		if detail.SourceVersionID == nil || *detail.SourceVersionID != v1.ID {
			t.Fatalf("rollback version %d lost its source reference", id)
		}
	}

	_, total, err := versions.List(ctx, 1, 50)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != workers+1 {
		t.Fatalf("expected %d versions, got %d", workers+1, total)
	}
}

func TestScheduleVersionRollbackPublishFlow(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	v1, err := versions.Snapshot(ctx, generateRequest(1, f), nil)
	if err != nil {
		t.Fatalf("snapshot v1: %v", err)
	}
	v2, err := versions.Snapshot(ctx, generateRequest(1, f), nil)
	if err != nil {
		t.Fatalf("snapshot v2: %v", err)
	}
	if _, err := versions.Publish(ctx, v2.ID); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	// The rollback draft is the newest version, so it may be published.
	rb, err := versions.Rollback(ctx, v1.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	published, err := versions.Publish(ctx, rb.ID)
	if err != nil {
		t.Fatalf("publish rollback draft: %v", err)
	}
	if published.Status != constants.VersionStatusPublished {
		t.Fatalf("expected published rollback draft, got %s", published.Status)
	}

	// The previously published version was archived; exactly one remains.
	superseded, err := versions.Get(ctx, v2.ID)
	if err != nil {
		t.Fatalf("get v2: %v", err)
	}
	if superseded.Status != constants.VersionStatusArchived {
		t.Fatalf("expected v2 archived, got %s", superseded.Status)
	}
	items, _, err := versions.List(ctx, 1, 20)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	publishedCount := 0
	for _, item := range items {
		if item.Status == constants.VersionStatusPublished {
			publishedCount++
			if item.ID != rb.ID {
				t.Fatalf("only the rollback draft may stay published, got id=%d", item.ID)
			}
		}
	}
	if publishedCount != 1 {
		t.Fatalf("expected exactly one published version, got %d", publishedCount)
	}

	// Older versions still cannot be published.
	if _, err := versions.Publish(ctx, v1.ID); !errors.Is(err, service.ErrVersionNotLatest) {
		t.Fatalf("expected ErrVersionNotLatest publishing old version, got %v", err)
	}
}
