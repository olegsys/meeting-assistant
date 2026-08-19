package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/service/mocks"
)

// recordingTxManager вызывает callback в том же контексте, позволяя тестам проверять,
// какие операции выполняются внутри транзакции. В отличие от passthroughTxManager,
// применяется в тестах processing, чтобы не дублировать определение.
type recordingTxManager struct{}

func (recordingTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func newProcessingService(
	t *testing.T,
	taskRepo *mocks.MockTaskRepo,
	fileRepo *mocks.MockFileRepo,
	contentRepo *mocks.MockContentRepo,
	speechClient *mocks.MockSpeechClient,
	llmClient *mocks.MockLLMClient,
) *processingService {
	t.Helper()
	return &processingService{
		taskRepo:     taskRepo,
		fileRepo:     fileRepo,
		contentRepo:  contentRepo,
		speechClient: speechClient,
		llmClient:    llmClient,
		txManager:    recordingTxManager{},
		workers:      2,
		taskTimeout:  5 * time.Second,
		pollInterval: time.Second,
		maxAttempts:  3,
		maxFileBytes: 1 << 20, // 1 MiB
	}
}

func TestProcessingService_ProcessJob_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), "meeting.wav", gomock.Any()).
		Return("транскрипция", nil)
	contentRepo.EXPECT().UpsertTranscription(gomock.Any(), int64(100), "транскрипция").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusTranscribed).Return(nil)
	llmClient.EXPECT().Summarize(gomock.Any(), "транскрипция").Return("выжимка", nil)
	contentRepo.EXPECT().UpsertSummary(gomock.Any(), int64(100), "выжимка").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusSummarized).Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusCompleted).Return(nil)

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_FileTooLarge(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)
	svc.maxFileBytes = 4 // предельно мало, чтобы тестовый файл гарантированно превысил лимит

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("big.wav", []byte("0123456789"), nil)
	taskRepo.EXPECT().SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, taskID int64, msg string) error {
			assert.Contains(t, msg, "file too large")
			assert.Contains(t, msg, "10") // размер
			return nil
		})

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_FileRepoError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).
		Return("", nil, errors.New("db read error"))
	taskRepo.EXPECT().SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, msg string) error {
			assert.Contains(t, msg, "получение файла")
			return nil
		})

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_SpeechError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", errors.New("speech api down"))
	taskRepo.EXPECT().SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, msg string) error {
			assert.Contains(t, msg, "speech client")
			return nil
		})

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_TranscriptionAlreadyExistsContinues(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("новая транскрипция", nil)
	// транскрипция уже была сохранена раньше → задача НЕ переходит в transcribed, но продолжаем
	contentRepo.EXPECT().UpsertTranscription(gomock.Any(), int64(100), "новая транскрипция").
		Return(models.ErrTranscriptionAlreadyExists)
	// UpdateStatus(transcribed) НЕ должен вызываться
	llmClient.EXPECT().Summarize(gomock.Any(), "новая транскрипция").Return("выжимка", nil)
	contentRepo.EXPECT().UpsertSummary(gomock.Any(), int64(100), "выжимка").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusSummarized).Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusCompleted).Return(nil)

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_UpsertTranscriptionError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("транскрипция", nil)
	contentRepo.EXPECT().UpsertTranscription(gomock.Any(), int64(100), "транскрипция").
		Return(errors.New("insert failed"))
	taskRepo.EXPECT().SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, msg string) error {
			assert.Contains(t, msg, "сохранение транскрипции")
			return nil
		})

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_LLMError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("транскрипция", nil)
	contentRepo.EXPECT().UpsertTranscription(gomock.Any(), int64(100), "транскрипция").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusTranscribed).Return(nil)
	llmClient.EXPECT().Summarize(gomock.Any(), "транскрипция").
		Return("", errors.New("gigachat 429"))
	taskRepo.EXPECT().SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, msg string) error {
			assert.Contains(t, msg, "llm client summarize")
			return nil
		})

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_SummaryAlreadyExistsContinues(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("транскрипция", nil)
	contentRepo.EXPECT().UpsertTranscription(gomock.Any(), int64(100), "транскрипция").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusTranscribed).Return(nil)
	llmClient.EXPECT().Summarize(gomock.Any(), "транскрипция").Return("выжимка", nil)
	contentRepo.EXPECT().UpsertSummary(gomock.Any(), int64(100), "выжимка").
		Return(models.ErrSummaryAlreadyExists)
	// UpdateStatus(summarized) НЕ должен вызываться
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusCompleted).Return(nil)

	svc.processJob(context.Background(), job)
}

func TestProcessingService_ProcessJob_FinalizeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	speechClient := mocks.NewMockSpeechClient(ctrl)
	llmClient := mocks.NewMockLLMClient(ctrl)

	svc := newProcessingService(t, taskRepo, fileRepo, contentRepo, speechClient, llmClient)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	fileRepo.EXPECT().Get(gomock.Any(), int64(100)).Return("meeting.wav", []byte("audio"), nil)
	speechClient.EXPECT().Transcribe(gomock.Any(), gomock.Any(), gomock.Any()).
		Return("транскрипция", nil)
	contentRepo.EXPECT().UpsertTranscription(gomock.Any(), int64(100), "транскрипция").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusTranscribed).Return(nil)
	llmClient.EXPECT().Summarize(gomock.Any(), "транскрипция").Return("выжимка", nil)
	contentRepo.EXPECT().UpsertSummary(gomock.Any(), int64(100), "выжимка").Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusSummarized).Return(nil)
	taskRepo.EXPECT().UpdateStatus(gomock.Any(), int64(1), models.StatusCompleted).
		Return(errors.New("connection lost"))
	taskRepo.EXPECT().SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, msg string) error {
			assert.Contains(t, msg, "финализация задачи")
			return nil
		})

	svc.processJob(context.Background(), job)
}

func TestProcessingService_FailTask_ContextCanceledReturnsToCreated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newProcessingService(t,
		taskRepo,
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
		mocks.NewMockSpeechClient(ctrl),
		mocks.NewMockLLMClient(ctrl),
	)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	// при отмене контекста задача должна быть возвращена в created, а не в failed
	taskRepo.EXPECT().
		UpdateStatus(gomock.Any(), int64(1), models.StatusCreated).
		Return(nil)

	svc.failTask(job, context.Canceled)

	// достаточно, что gomock не нашёл неоправданных вызовов и канал не словил SetFailed
}

func TestProcessingService_FailTask_NormalErrorMarksFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newProcessingService(t,
		taskRepo,
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
		mocks.NewMockSpeechClient(ctrl),
		mocks.NewMockLLMClient(ctrl),
	)

	job := models.ProcessingJob{TaskID: 1, MeetingID: 100, UserID: 7}

	taskRepo.EXPECT().
		SetFailed(gomock.Any(), int64(1), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, msg string) error {
			assert.Contains(t, msg, "boom")
			return nil
		})

	svc.failTask(job, errors.New("boom"))
}

// Дополнительный тест: проверка дефолтов конструктора NewProcessingService.
func TestNewProcessingService_Defaults(t *testing.T) {
	svc := NewProcessingService(
		mocks.NewMockTaskRepo(nil),
		mocks.NewMockFileRepo(nil),
		mocks.NewMockContentRepo(nil),
		mocks.NewMockSpeechClient(nil),
		mocks.NewMockLLMClient(nil),
		nil,
		0, 0, 0, 0, 0,
	).(*processingService)

	require.NotNil(t, svc)
	assert.Equal(t, 1, svc.workers)
	assert.Equal(t, 30*time.Second, svc.taskTimeout)
	assert.Equal(t, 5*time.Second, svc.pollInterval)
	assert.Equal(t, 3, svc.maxAttempts)
}
