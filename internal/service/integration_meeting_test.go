// Интеграционные тесты MeetingService поверх реального PostgreSQL.
//
//go:build integration

package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
	"github.com/olegsys/meeting-assistant/internal/service"
	"github.com/olegsys/meeting-assistant/internal/testdb"
)

func newMeetingEnv(t *testing.T) (service.MeetingService, repository.TaskRepo) {
	t.Helper()

	pool := testdb.Pool(t)
	testdb.Truncate(t, pool)

	txManager := database.NewTxManager(pool)
	userRepo := repository.NewUserRepo(pool)
	meetingRepo := repository.NewMeetingRepo(pool)
	fileRepo := repository.NewFileRepo(pool)
	taskRepo := repository.NewTaskRepo(pool)
	contentRepo := repository.NewContentRepo(pool)

	userSvc := service.NewUserService(userRepo)
	svc := service.NewMeetingService(userSvc, meetingRepo, fileRepo, taskRepo, contentRepo, txManager)

	return svc, taskRepo
}

func TestMeetingService_Load_CreatesAllEntities(t *testing.T) {
	svc, taskRepo := newMeetingEnv(t)
	ctx := context.Background()

	mid, err := svc.Load(ctx, "alice", "audio.wav", []byte("payload"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if mid == 0 {
		t.Fatalf("meeting_id = 0")
	}

	// Проверяем состояние через репозитории
	pool := testdb.Pool(t)
	var meetings, files, tasks int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM meetings WHERE id = $1`, mid).Scan(&meetings); err != nil {
		t.Fatalf("count meetings: %v", err)
	}

	if meetings != 1 {
		t.Fatalf("meetings count = %d, ожидалось 1", meetings)
	}

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM meeting_files WHERE meeting_id = $1`, mid).Scan(&files); err != nil {
		t.Fatalf("count files: %v", err)
	}

	if files != 1 {
		t.Fatalf("files count = %d, ожидалось 1", files)
	}

	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM processing_tasks WHERE meeting_id = $1 AND status = 'created'`, mid,
	).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}

	if tasks != 1 {
		t.Fatalf("tasks count = %d, ожидалось 1", tasks)
	}

	_ = taskRepo
}

func TestMeetingService_Load_RollsBackOnError(t *testing.T) {
	svc, _ := newMeetingEnv(t)
	ctx := context.Background()

	_, err := svc.Load(ctx, "alice", "empty.wav", []byte{})
	if !errors.Is(err, models.ErrEmptyFile) {
		t.Fatalf("ожидалось ErrEmptyFile, получено %v", err)
	}

	pool := testdb.Pool(t)

	var meetings int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM meetings`).Scan(&meetings); err != nil {
		t.Fatalf("count: %v", err)
	}

	if meetings != 0 {
		t.Fatalf("после отката осталось %d встреч", meetings)
	}

	var users int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}

	if users != 1 {
		t.Fatalf("alice должен был быть создан, но users = %d", users)
	}
}

// Изоляция списка по пользователю

func TestMeetingService_List_FiltersByUser(t *testing.T) {
	svc, _ := newMeetingEnv(t)
	ctx := context.Background()

	if _, err := svc.Load(ctx, "alice", "a.wav", []byte("x")); err != nil {
		t.Fatalf("alice load1: %v", err)
	}

	if _, err := svc.Load(ctx, "alice", "b.wav", []byte("y")); err != nil {
		t.Fatalf("alice load2: %v", err)
	}

	if _, err := svc.Load(ctx, "bob", "c.wav", []byte("z")); err != nil {
		t.Fatalf("bob load: %v", err)
	}

	aliceItems, err := svc.List(ctx, "alice")
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}

	if len(aliceItems) != 2 {
		t.Fatalf("alice: ожидалось 2, получено %d", len(aliceItems))
	}

	bobItems, err := svc.List(ctx, "bob")
	if err != nil {
		t.Fatalf("List bob: %v", err)
	}

	if len(bobItems) != 1 {
		t.Fatalf("bob: ожидалось 1, получено %d", len(bobItems))
	}
}

// Доступ к чужой встрече возвращает ErrNotFound

func TestMeetingService_Status_AccessDenied(t *testing.T) {
	svc, _ := newMeetingEnv(t)
	ctx := context.Background()

	mid, err := svc.Load(ctx, "alice", "a.wav", []byte("x"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	_, err = svc.Status(ctx, "bob", mid)
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("ожидалось ErrNotFound, получено %v", err)
	}
}

func TestMeetingService_GetTranscription_AccessDenied(t *testing.T) {
	svc, _ := newMeetingEnv(t)
	ctx := context.Background()

	mid, err := svc.Load(ctx, "alice", "a.wav", []byte("x"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	_, err = svc.GetTranscription(ctx, "bob", mid)
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("ожидалось ErrNotFound, получено %v", err)
	}
}

// Retry работает только на failed.

func TestMeetingService_Retry_OnlyFailed(t *testing.T) {
	svc, taskRepo := newMeetingEnv(t)
	ctx := context.Background()

	mid, err := svc.Load(ctx, "alice", "a.wav", []byte("x"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Завершим задачу успешно
	if err := taskRepo.UpdateStatus(ctx, mustTaskID(t, taskRepo, mid), models.StatusCompleted); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	err = svc.Retry(ctx, "alice", mid)
	if !errors.Is(err, models.ErrTaskNotFailed) {
		t.Fatalf("Retry на completed: ожидалось ErrTaskNotFailed, получено %v", err)
	}

	// Переведём в failed и попробуем retry
	if err := taskRepo.SetFailed(ctx, mustTaskID(t, taskRepo, mid), "boom"); err != nil {
		t.Fatalf("SetFailed: %v", err)
	}

	if err := svc.Retry(ctx, "alice", mid); err != nil {
		t.Fatalf("Retry на failed: %v", err)
	}

	info, err := svc.Status(ctx, "alice", mid)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if info.Status != models.StatusCreated {
		t.Fatalf("после retry status = %s, ожидалось created", info.Status)
	}

	if info.ErrorMessage != "" {
		t.Fatalf("error_message = %q, ожидалось пусто", info.ErrorMessage)
	}
}

func mustTaskID(t *testing.T, repo repository.TaskRepo, meetingID int64) int64 {
	t.Helper()

	pool := testdb.Pool(t)
	ctx := context.Background()

	var id int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM processing_tasks WHERE meeting_id = $1`, meetingID,
	).Scan(&id); err != nil {
		t.Fatalf("get task id for meeting %d: %v", meetingID, err)
	}

	return id
}
