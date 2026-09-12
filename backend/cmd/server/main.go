package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	_ "github.com/gbschedule/gbschedule/docs"
	"github.com/gbschedule/gbschedule/internal/config"
	"github.com/gbschedule/gbschedule/internal/handler"
	"github.com/gbschedule/gbschedule/internal/migration"
	"github.com/gbschedule/gbschedule/internal/model"
	"github.com/gbschedule/gbschedule/internal/repository"
	"github.com/gbschedule/gbschedule/internal/router"
	"github.com/gbschedule/gbschedule/internal/service"
)

// @title           教室排课助手 API
// @version         1.0.0
// @description     教室排课、教室资源管理和冲突检测 RESTful API。
// @BasePath        /
func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	db, err := openDatabase(cfg.DBPath)
	if err != nil {
		logger.Error("open database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := migrate(db); err != nil {
		logger.Error("migrate database", slog.String("error", err.Error()))
		os.Exit(1)
	}

	app, err := newApp(db, logger)
	if err != nil {
		logger.Error("assemble app", slog.String("error", err.Error()))
		os.Exit(1)
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      app,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	logger.Info("server starting", slog.Int("port", cfg.ServerPort), slog.String("db_path", cfg.DBPath))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}

func openDatabase(path string) (*gorm.DB, error) {
	if path == "" {
		path = "./data/gbschedule.db"
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(sqliteDSN(path)), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows only one writer at a time. A single connection serializes
	// all database access in-process, so concurrent requests never hit
	// shared-cache lock conflicts; transactions still provide atomicity.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db, nil
}

// sqliteDSN appends a busy timeout so concurrent writers wait for each other
// instead of failing with "database is locked".
func sqliteDSN(path string) string {
	if strings.Contains(path, "busy_timeout") {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "_pragma=busy_timeout(5000)"
}

func migrate(db *gorm.DB) error {
	// Versioned SQL migrations run first and are recorded in
	// schema_migrations, so repeated starts never re-apply them; AutoMigrate
	// reconciles any remaining model drift afterwards.
	if err := migration.Apply(db, migration.Migrations); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if err := db.AutoMigrate(
		&model.Classroom{},
		&model.Teacher{},
		&model.Class{},
		&model.Course{},
		&model.TimeSlot{},
		&model.Schedule{},
		&model.AdjustmentLog{},
		&model.ScheduleVersion{},
		&model.ScheduleVersionEntry{},
	); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

func newApp(db *gorm.DB, logger *slog.Logger) (*gin.Engine, error) {
	classroomRepo := repository.NewClassroomRepository(db)
	teacherRepo := repository.NewTeacherRepository(db)
	classRepo := repository.NewClassRepository(db)
	courseRepo := repository.NewCourseRepository(db)
	timeSlotRepo := repository.NewTimeSlotRepository(db)
	scheduleRepo := repository.NewScheduleRepository(db)
	adjustmentRepo := repository.NewAdjustmentLogRepository(db)
	versionRepo := repository.NewScheduleVersionRepository(db)

	classroomService := service.NewClassroomService(classroomRepo, logger)
	teacherService := service.NewTeacherService(teacherRepo, logger)
	classService := service.NewClassService(classRepo, logger)
	courseService := service.NewCourseService(courseRepo, logger)
	timeSlotService := service.NewTimeSlotService(timeSlotRepo, logger)
	versionService := service.NewScheduleVersionService(versionRepo, classroomRepo, teacherRepo, classRepo, courseRepo, timeSlotRepo, logger)
	scheduleService := service.NewScheduleService(scheduleRepo, classroomRepo, teacherRepo, classRepo, courseRepo, timeSlotRepo, adjustmentRepo, versionService, repository.NewTransactor(db), logger)

	h := router.Handlers{
		Classroom:       handler.NewClassroomHandler(classroomService, logger),
		Teacher:         handler.NewTeacherHandler(teacherService, logger),
		Class:           handler.NewClassHandler(classService, logger),
		Course:          handler.NewCourseHandler(courseService, logger),
		TimeSlot:        handler.NewTimeSlotHandler(timeSlotService, logger),
		Schedule:        handler.NewScheduleHandler(scheduleService, logger),
		ScheduleVersion: handler.NewScheduleVersionHandler(versionService, logger),
		Statistics:      handler.NewStatisticsHandler(scheduleService, logger),
	}
	return router.New(h, logger), nil
}
