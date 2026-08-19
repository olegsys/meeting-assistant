// Интеграционные тесты ProcessingService поверх реального PostgreSQL.
//
//go:build integration

package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
	"github.com/olegsys/meeting-assistant/internal/service"
	"github.com/olegsys/meeting-assistant/internal/speech"
	"github.com/olegsys/meeting-assistant/internal/testdb"
)

// scriptableSpeechClient позволяет задать поведение Transcribe через функцию,
// а также отслеживает пиковое число одновременных вызовов
type scriptableSpeechClient struct {
	fn       func(ctx context.Context, fileName string, content []byte) (string, error)
	current  int64
	peak     int64
	wgEnter  sync.WaitGroup
	wgLeave  sync.WaitGroup
	holdFunc func()
}

func newScriptableSpeechClient(
	fn func(ctx context.Context, fileName string, content []byte) (string, error),
) *scriptableSpeechClient {
	return &scriptableSpeechClient{fn: fn}
}

func (c *scriptableSpeechClient) Transcribe(ctx context.Context, fileName string, content []byte) (string, error) {
	now := atomic.AddInt64(&c.current, 1)
	defer atomic.AddInt64(&c.current, -1)

	for {
		old := atomic.LoadInt64(&c.peak)
		if now <= old || atomic.CompareAndSwapInt64(&c.peak, old, now) {
			break
		}
	}

	c.wgEnter.Done()

	if c.holdFunc != nil {
		c.holdFunc()
	}

	out, err := c.fn(ctx, fileName, content)

	c.wgLeave.Done()

	return out, err
}

// scriptableLLMClient — аналогичный LLM-клиент.
type scriptableLLMClient struct {
	fn func(ctx context.Context, transcript string) (string, error)
}

func (c *scriptableLLMClient) Summarize(ctx context.Context, transcript string) (string, error) {
	return c.fn(ctx, transcript)
}

func (c *scriptableLLMClient) Ask(ctx context.Context, materials, question string) (string, error) {
	return "", errors.New("chat not used in integration tests")
}

type processingEnv struct {
	pool        *pgxpool.Pool
	userRepo    repository.UserRepo
	meetingRepo repository.MeetingRepo
	fileRepo    repository.FileRepo
	taskRepo    repository.TaskRepo
	contentRepo repository.ContentRepo
	txManager   database.TxManager
	userSvc     service.UserService
}

func newProcessingEnv(t *testing.T) *processingEnv {
	t.Helper()

	pool := testdb.Pool(t)
	testdb.Truncate(t, pool)

	return &processingEnv{
		pool:        pool,
		userRepo:    repository.NewUserRepo(pool),
		meetingRepo: repository.NewMeetingRepo(pool),
		fileRepo:    repository.NewFileRepo(pool),
		taskRepo:    repository.NewTaskRepo(pool),
		contentRepo: repository.NewContentRepo(pool),
		txManager:   database.NewTxManager(pool),
		userSvc:     service.NewUserService(repository.NewUserRepo(pool)),
	}
}

func (e *processingEnv) seedMeeting(t *testing.T, externalID string) (int64, int64) {
	t.Helper()
	ctx := context.Background()

	user, err := e.userSvc.Resolve(ctx, externalID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	meeting, err := e.meetingRepo.Create(ctx, user.ID, "meeting-"+externalID)
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}

	if err := e.fileRepo.Save(ctx, meeting.ID, "audio.wav", []byte("payload")); err != nil {
		t.Fatalf("save file: %v", err)
	}

	taskID, err := e.taskRepo.Create(ctx, meeting.ID)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	return meeting.ID, taskID
}

func (e *processingEnv) taskStatus(t *testing.T, taskID int64) (models.ProcessingStatus, string) {
	t.Helper()

	var (
		status string
		msg    string
	)

	if err := e.pool.QueryRow(context.Background(),
		`SELECT status, COALESCE(error_message, '') FROM processing_tasks WHERE id = $1`, taskID,
	).Scan(&status, &msg); err != nil {
		t.Fatalf("read task %d: %v", taskID, err)
	}

	return models.ProcessingStatus(status), msg
}

func (e *processingEnv) transcription(t *testing.T, meetingID int64) string {
	t.Helper()

	var s string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT content FROM transcriptions WHERE meeting_id = $1`, meetingID,
	).Scan(&s); err != nil {
		return ""
	}

	return s
}

func (e *processingEnv) summary(t *testing.T, meetingID int64) string {
	t.Helper()

	var s string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT content FROM summaries WHERE meeting_id = $1`, meetingID,
	).Scan(&s); err != nil {
		return ""
	}

	return s
}

func TestProcessingService_HappyPath_Completed(t *testing.T) {
	env := newProcessingEnv(t)

	mid, taskID := env.seedMeeting(t, "alice")

	speechClient := newScriptableSpeechClient(func(_ context.Context, fileName string, _ []byte) (string, error) {
		return "транскрипция встречи " + fileName, nil
	})

	llmClient := &scriptableLLMClient{fn: func(_ context.Context, transcript string) (string, error) {
		return "выжимка: " + transcript, nil
	}}

	ps := service.NewProcessingService(
		env.taskRepo, env.fileRepo, env.contentRepo,
		speechClient, llmClient, env.txManager,
		1, 5*time.Second, 100*time.Millisecond, 3, 1024*1024,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = ps.Run(ctx)
		close(done)
	}()

	waitForStatus(t, env.pool, taskID, string(models.StatusCompleted), 5*time.Second)

	cancel()
	<-done

	if got := env.transcription(t, mid); got != "транскрипция встречи audio.wav" {
		t.Fatalf("transcription = %q", got)
	}

	if got := env.summary(t, mid); got != "выжимка: транскрипция встречи audio.wav" {
		t.Fatalf("summary = %q", got)
	}

	status, _ := env.taskStatus(t, taskID)
	if status != models.StatusCompleted {
		t.Fatalf("status = %s, ожидалось completed", status)
	}
}

func TestProcessingService_SpeechError_Failed(t *testing.T) {
	env := newProcessingEnv(t)

	mid, taskID := env.seedMeeting(t, "alice")

	speechClient := newScriptableSpeechClient(func(_ context.Context, _ string, _ []byte) (string, error) {
		return "", errors.New("speech down")
	})

	llmClient := &scriptableLLMClient{fn: func(_ context.Context, _ string) (string, error) {
		return "не вызывался", nil
	}}

	ps := service.NewProcessingService(
		env.taskRepo, env.fileRepo, env.contentRepo,
		speechClient, llmClient, env.txManager,
		1, 5*time.Second, 100*time.Millisecond, 3, 1024*1024,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = ps.Run(ctx)
		close(done)
	}()

	waitForStatus(t, env.pool, taskID, string(models.StatusFailed), 5*time.Second)
	cancel()
	<-done

	status, msg := env.taskStatus(t, taskID)
	if status != models.StatusFailed {
		t.Fatalf("status = %s", status)
	}

	if !strings.Contains(msg, "speech down") {
		t.Fatalf("error_message не содержит причину: %q", msg)
	}

	if got := env.transcription(t, mid); got != "" {
		t.Fatalf("транскрипции быть не должно: %q", got)
	}

	if got := env.summary(t, mid); got != "" {
		t.Fatalf("выжимки быть не должно: %q", got)
	}
}

func TestProcessingService_LLMError_Failed(t *testing.T) {
	env := newProcessingEnv(t)

	mid, taskID := env.seedMeeting(t, "alice")

	speechClient := newScriptableSpeechClient(func(_ context.Context, _ string, _ []byte) (string, error) {
		return "успешная транскрипция", nil
	})

	llmClient := &scriptableLLMClient{fn: func(_ context.Context, _ string) (string, error) {
		return "", errors.New("llm quota exceeded")
	}}

	ps := service.NewProcessingService(
		env.taskRepo, env.fileRepo, env.contentRepo,
		speechClient, llmClient, env.txManager,
		1, 5*time.Second, 100*time.Millisecond, 3, 1024*1024,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = ps.Run(ctx)
		close(done)
	}()

	waitForStatus(t, env.pool, taskID, string(models.StatusFailed), 5*time.Second)
	cancel()
	<-done

	if got := env.transcription(t, mid); got != "успешная транскрипция" {
		t.Fatalf("транскрипция должна быть сохранена, получено %q", got)
	}

	if got := env.summary(t, mid); got != "" {
		t.Fatalf("выжимки быть не должно: %q", got)
	}

	status, msg := env.taskStatus(t, taskID)
	if status != models.StatusFailed {
		t.Fatalf("status = %s, ожидалось failed", status)
	}

	if !strings.Contains(msg, "llm quota exceeded") {
		t.Fatalf("error_message не содержит причину LLM: %q", msg)
	}
}

func TestProcessingService_RespectsWorkerLimit(t *testing.T) {
	env := newProcessingEnv(t)

	const total = 6
	const workers = 2

	taskIDs := make([]int64, 0, total)
	for i := 0; i < total; i++ {
		_, taskID := env.seedMeeting(t, fmt.Sprintf("user-%d", i))
		taskIDs = append(taskIDs, taskID)
	}

	client := newScriptableSpeechClient(func(_ context.Context, _ string, _ []byte) (string, error) {
		return "транскрипция", nil
	})

	// Удерживаем Transcribe до тех пор, пока все задачи не будут заявлены воркерами.
	released := make(chan struct{})

	client.holdFunc = func() {
		<-released
	}

	// Каждый вызов Transcribe должен попасть в воркер до того, как мы отпустим блокировку.
	client.wgEnter.Add(workers)
	client.wgLeave.Add(workers)

	llmClient := &scriptableLLMClient{fn: func(_ context.Context, _ string) (string, error) {
		return "выжимка", nil
	}}

	ps := service.NewProcessingService(
		env.taskRepo, env.fileRepo, env.contentRepo,
		client, llmClient, env.txManager,
		workers, 30*time.Second, 100*time.Millisecond, 3, 1024*1024,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = ps.Run(ctx)
		close(done)
	}()

	// Ждём, пока workers воркеров войдут в Transcribe.
	client.wgEnter.Wait()

	if peak := atomic.LoadInt64(&client.peak); peak > workers {
		t.Fatalf("пик одновременных Transcribe = %d, ожидалось ≤ %d", peak, workers)
	}

	close(released)

	// Ждём завершения всех задач.
	for _, id := range taskIDs {
		waitForStatus(t, env.pool, id, string(models.StatusCompleted), 10*time.Second)
	}

	cancel()
	<-done

	if peak := atomic.LoadInt64(&client.peak); peak != workers {
		t.Fatalf("итоговый пик = %d, ожидалось ровно %d", peak, workers)
	}
}

func TestProcessingService_RequeueStale_RestartsTask(t *testing.T) {
	env := newProcessingEnv(t)
	ctx := context.Background()

	// Создаём встречу и вручную переводим её задачу в processing с устаревшим updated_at,
	// как если бы воркер упал до завершения.
	_, taskID := env.seedMeeting(t, "alice")

	if _, err := env.taskRepo.ClaimCreated(ctx, 1); err != nil {
		t.Fatalf("ClaimCreated: %v", err)
	}

	testdb.MustExec(t, env.pool,
		`UPDATE processing_tasks SET attempts = 1, updated_at = now() - interval '1 hour' WHERE id = $1`,
		taskID,
	)

	// Поднимаем ProcessingService, который по первому тику RequeueStale должен
	// вернуть зависшую задачу в created, затем забрать её в processing и завершить.
	started := make(chan struct{}, 1)
	speechClient := newScriptableSpeechClient(func(_ context.Context, _ string, _ []byte) (string, error) {
		select {
		case started <- struct{}{}:
		default:
		}

		return "транскрипция", nil
	})

	llmClient := &scriptableLLMClient{fn: func(_ context.Context, _ string) (string, error) {
		return "выжимка", nil
	}}

	ps := service.NewProcessingService(
		env.taskRepo, env.fileRepo, env.contentRepo,
		speechClient, llmClient, env.txManager,
		1, 5*time.Second, 100*time.Millisecond, 3, 1024*1024,
	)

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = ps.Run(runCtx)
		close(done)
	}()

	select {
	case <-started:
	case <-runCtx.Done():
		t.Fatalf("воркер не запустился повторно после RequeueStale")
	}

	waitForStatus(t, env.pool, taskID, string(models.StatusCompleted), 5*time.Second)

	cancel()
	<-done

	status, _ := env.taskStatus(t, taskID)
	if status != models.StatusCompleted {
		t.Fatalf("status = %s, ожидалось completed после requeue", status)
	}
}

func waitForStatus(t *testing.T, pool *pgxpool.Pool, taskID int64, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var status string
		if err := pool.QueryRow(context.Background(),
			`SELECT status FROM processing_tasks WHERE id = $1`, taskID,
		).Scan(&status); err == nil {
			if status == want {
				return
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("задача %d не достигла статуса %q за %s", taskID, want, timeout)
}

// Гарантируем, что speech-импорты не помечаются как неиспользуемые при условной компиляции.
var _ speech.Client = (*scriptableSpeechClient)(nil)
