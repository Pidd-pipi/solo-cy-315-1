package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
)

func TestScheduleVersionRepositoryCreateAssignsSequentialNumbers(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewScheduleVersionRepository(newTestDB(t))

	for want := uint(1); want <= 3; want++ {
		version := &model.ScheduleVersion{Semester: "2025-2026-1", Status: constants.VersionStatusDraft}
		entries := []model.ScheduleVersionEntry{
			{Week: 1, DayOfWeek: 1, TimeSlotID: 1, ClassroomID: 1, TeacherID: 1, ClassID: 1, CourseID: 1},
		}
		if err := repo.Create(ctx, version, entries); err != nil {
			t.Fatalf("create version: %v", err)
		}
		if version.VersionNo != want {
			t.Fatalf("expected version_no %d, got %d", want, version.VersionNo)
		}
		if version.ID == 0 {
			t.Fatal("expected auto generated id")
		}
		got, err := repo.GetEntries(ctx, version.ID)
		if err != nil {
			t.Fatalf("get entries: %v", err)
		}
		if len(got) != 1 || got[0].VersionID != version.ID {
			t.Fatalf("expected one entry bound to version %d, got %+v", version.ID, got)
		}
	}

	latest, err := repo.Latest(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.VersionNo != 3 {
		t.Fatalf("expected latest version_no 3, got %d", latest.VersionNo)
	}

	items, total, err := repo.List(ctx, 1, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(items) != 2 {
		t.Fatalf("expected total=3 page len=2, got total=%d len=%d", total, len(items))
	}
	if items[0].VersionNo != 3 {
		t.Fatalf("expected versions ordered by version_no desc, got %d", items[0].VersionNo)
	}
}

func TestScheduleVersionRepositoryPublishKeepsSinglePublished(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewScheduleVersionRepository(newTestDB(t))

	v1 := &model.ScheduleVersion{Status: constants.VersionStatusDraft}
	if err := repo.Create(ctx, v1, nil); err != nil {
		t.Fatalf("create v1: %v", err)
	}
	v2 := &model.ScheduleVersion{Status: constants.VersionStatusDraft}
	if err := repo.Create(ctx, v2, nil); err != nil {
		t.Fatalf("create v2: %v", err)
	}

	if err := repo.Publish(ctx, v1.ID, time.Now()); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	if err := repo.Publish(ctx, v2.ID, time.Now()); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	first, err := repo.GetByID(ctx, v1.ID)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	if first.Status != constants.VersionStatusArchived {
		t.Fatalf("expected v1 archived after v2 published, got %s", first.Status)
	}
	second, err := repo.GetByID(ctx, v2.ID)
	if err != nil {
		t.Fatalf("get v2: %v", err)
	}
	if second.Status != constants.VersionStatusPublished || second.PublishedAt == nil {
		t.Fatalf("expected v2 published with timestamp, got %+v", second)
	}

	if err := repo.Publish(ctx, 9999, time.Now()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound publishing missing version, got %v", err)
	}
}
