package service_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
	"github.com/gbschedule/gbschedule/internal/service"
)

// failingVersionService stubs ScheduleVersionService and fails every snapshot
// to exercise the generation rollback path.
type failingVersionService struct {
	service.ScheduleVersionService
	err error
}

func (s failingVersionService) Snapshot(ctx context.Context, req *dto.GenerateScheduleRequest, schedules []model.Schedule) (*model.ScheduleVersion, error) {
	return nil, s.err
}

func TestScheduleServiceConcurrentGenerate(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	svc := newScheduleService(t, db)
	versions := newVersionService(db, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	versionNos := make([]uint, workers)
	versionIDs := make([]uint, workers)
	generated := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := svc.Generate(ctx, generateRequest(1, f))
			if err != nil {
				errs[i] = err
				return
			}
			versionNos[i] = resp.VersionNo
			versionIDs[i] = resp.VersionID
			generated[i] = resp.Generated
		}(i)
	}
	wg.Wait()

	// Every request gets a clear result: no internal errors, no version
	// number conflicts.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("generate %d failed: %v", i, err)
		}
	}

	// Version numbers are unique and exactly 1..workers.
	seen := map[uint]bool{}
	for _, no := range versionNos {
		if no < 1 || no > workers || seen[no] {
			t.Fatalf("duplicate or out-of-range version numbers: %v", versionNos)
		}
		seen[no] = true
	}

	// Every successful generation left a complete snapshot.
	_, total, err := versions.List(ctx, 1, 50)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != workers {
		t.Fatalf("expected %d snapshots, got %d", workers, total)
	}
	for i, id := range versionIDs {
		detail, err := versions.Get(ctx, id)
		if err != nil {
			t.Fatalf("get version %d: %v", id, err)
		}
		if detail.EntryCount != generated[i] || len(detail.Entries) != generated[i] {
			t.Fatalf("incomplete snapshot for version %d: count=%d entries=%d generated=%d",
				id, detail.EntryCount, len(detail.Entries), generated[i])
		}
	}

	// The live timetable holds exactly one generation's result, never a mix.
	items, err := svc.List(ctx, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(items) != generated[0] {
		t.Fatalf("expected live timetable of exactly one generation (%d entries), got %d", generated[0], len(items))
	}
}

func TestScheduleServiceGenerateRollbackOnSnapshotFailure(t *testing.T) {
	ctx := context.Background()
	db := newScheduleTestDB(t)
	f := seedVersionFixture(t, db)
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

	// First generation succeeds with the real version service.
	svc := newScheduleService(t, db)
	first, err := svc.Generate(ctx, generateRequest(1, f))
	if err != nil {
		t.Fatalf("seed generate: %v", err)
	}

	// Second generation fails while snapshotting: the whole write phase must
	// roll back, leaving the previous timetable and version history intact.
	failing := service.NewScheduleService(
		repository.NewScheduleRepository(db),
		repository.NewClassroomRepository(db),
		repository.NewTeacherRepository(db),
		repository.NewClassRepository(db),
		repository.NewCourseRepository(db),
		repository.NewTimeSlotRepository(db),
		repository.NewAdjustmentLogRepository(db),
		failingVersionService{err: errors.New("snapshot boom")},
		repository.NewTransactor(db),
		logger,
	)
	if _, err := failing.Generate(ctx, generateRequest(2, f)); err == nil {
		t.Fatal("expected generate to fail when the snapshot write fails")
	}

	// The live timetable still holds the first generation, untouched.
	items, err := svc.List(ctx, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(items) != first.Generated {
		t.Fatalf("expected rolled-back timetable to keep %d entries, got %d", first.Generated, len(items))
	}

	// No half-written snapshot survived the rollback.
	versions := newVersionService(db, logger)
	_, total, err := versions.List(ctx, 1, 10)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected only the successful snapshot to remain, got %d versions", total)
	}
}
