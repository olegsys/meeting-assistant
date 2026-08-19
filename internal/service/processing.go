package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/llm"
	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
	"github.com/olegsys/meeting-assistant/internal/speech"
	"golang.org/x/sync/errgroup"
)

// ProcessingService запускает пул воркеров, обрабатывающих задачи встреч.
type ProcessingService interface {
	Run(ctx context.Context) error
}

type processingService struct {
	taskRepo     repository.TaskRepo
	fileRepo     repository.FileRepo
	contentRepo  repository.ContentRepo
	speechClient speech.Client
	llmClient    llm.Client
	txManager    database.TxManager

	workers      int
	taskTimeout  time.Duration
	pollInterval time.Duration
	maxAttempts  int
	maxFileBytes int64
}

// NewProcessingService собирает ProcessingService с заданными параметрами пула и таймаутов.
func NewProcessingService(
	taskRepo repository.TaskRepo,
	fileRepo repository.FileRepo,
	contentRepo repository.ContentRepo,
	speechClient speech.Client,
	llmClient llm.Client,
	txManager database.TxManager,
	workers int,
	taskTimeout time.Duration,
	pollInterval time.Duration,
	maxAttempts int,
	maxFileBytes int64,
) ProcessingService {
	if workers <= 0 {
		workers = 1
	}

	if taskTimeout <= 0 {
		taskTimeout = 30 * time.Second
	}

	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}

	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	return &processingService{
		taskRepo:     taskRepo,
		fileRepo:     fileRepo,
		contentRepo:  contentRepo,
		speechClient: speechClient,
		llmClient:    llmClient,
		txManager:    txManager,
		workers:      workers,
		taskTimeout:  taskTimeout,
		pollInterval: pollInterval,
		maxAttempts:  maxAttempts,
		maxFileBytes: maxFileBytes,
	}
}

func (s *processingService) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	jobs := make(chan models.ProcessingJob, s.workers)

	for i := 0; i < s.workers; i++ {
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case job, ok := <-jobs:
					if !ok {
						return nil
					}

					s.processJob(ctx, job)
				}
			}
		})
	}

	g.Go(func() error {
		defer close(jobs)

		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				s.dispatch(ctx, jobs)
			}
		}
	})

	return g.Wait()
}

// dispatch забирает созданные задачи из БД и отправляет их в канал воркеров, попутно переставляя зависшие.
func (s *processingService) dispatch(ctx context.Context, jobs chan models.ProcessingJob) {
	requeued, err := s.taskRepo.RequeueStale(ctx, s.taskTimeout*2, s.maxAttempts)
	if err != nil {
		slog.Error("не удалось выполнить requeue stale задач", slog.String("error", err.Error()))
	} else if requeued > 0 {
		slog.Info("requeued stale tasks", slog.Int64("count", requeued))
	}

	claimed, err := s.taskRepo.ClaimCreated(ctx, s.workers)
	if err != nil {
		slog.Error("не удалось получить задачи для обработки", slog.String("error", err.Error()))
		return
	}

	for _, job := range claimed {
		select {
		case jobs <- job:
		case <-ctx.Done():
			return
		}
	}
}

// processJob выполняет полный цикл обработки одной задачи: файл, транскрипция, выжимка, финальный статус.
func (s *processingService) processJob(ctx context.Context, job models.ProcessingJob) {
	logger := slog.With(
		slog.Int64("task_id", job.TaskID),
		slog.Int64("meeting_id", job.MeetingID),
		slog.Int64("user_id", job.UserID),
	)

	defer func() {
		if r := recover(); r != nil {
			logger.Error(
				"паника при обработке задачи",
				slog.Any("panic", r),
			)
			s.failTask(job, fmt.Errorf("внутренняя ошибка: panic: %v", r))
		}
	}()

	logger.Info("начата обработка задачи")

	ctx, cancel := context.WithTimeout(ctx, s.taskTimeout)
	defer cancel()

	// Получаем файл из БД
	fileName, content, err := s.fileRepo.Get(ctx, job.MeetingID)
	if err != nil {
		s.failTask(job, fmt.Errorf("получение файла: %w", err))
		return
	}

	// Защита от OOM: если файл превышает лимит, отказываем в обработке.
	if s.maxFileBytes > 0 && int64(len(content)) > s.maxFileBytes {
		s.failTask(
			job,
			fmt.Errorf(
				"%w: размер %d байт превышает лимит %d",
				models.ErrFileTooLarge,
				len(content),
				s.maxFileBytes,
			),
		)
		return
	}

	// Распознаём речь
	transcript, err := s.speechClient.Transcribe(ctx, fileName, content)
	if err != nil {
		s.failTask(job, fmt.Errorf("speech client: %w", err))
		return
	}

	// Сохраняем транскрипцию и ставим статус transcribed
	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.contentRepo.UpsertTranscription(txCtx, job.MeetingID, transcript); err != nil {
			if errors.Is(err, models.ErrTranscriptionAlreadyExists) {
				logger.Warn("транскрипция уже существует, пропускаем (повторная обработка)")
				return nil
			}
			return err
		}
		return s.taskRepo.UpdateStatus(txCtx, job.TaskID, models.StatusTranscribed)
	})
	if err != nil {
		s.failTask(job, fmt.Errorf("сохранение транскрипции: %w", err))
		return
	}
	logger.Info("транскрипция сохранена", slog.String("status", string(models.StatusTranscribed)))

	// Получаем выжимку (LLM Client)
	summary, err := s.llmClient.Summarize(ctx, transcript)
	if err != nil {
		s.failTask(job, fmt.Errorf("llm client summarize: %w", err))
		return
	}

	// Сохраняем выжимку и ставим статус summarized
	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.contentRepo.UpsertSummary(txCtx, job.MeetingID, summary); err != nil {
			if errors.Is(err, models.ErrSummaryAlreadyExists) {
				logger.Warn("выжимка уже существует, пропускаем (повторная обработка)")
				return nil
			}
			return err
		}
		return s.taskRepo.UpdateStatus(txCtx, job.TaskID, models.StatusSummarized)
	})
	if err != nil {
		s.failTask(job, fmt.Errorf("сохранение выжимки: %w", err))
		return
	}
	logger.Info("выжимка сохранена", slog.String("status", string(models.StatusSummarized)))

	// Завершаем задачу
	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		return s.taskRepo.UpdateStatus(txCtx, job.TaskID, models.StatusCompleted)
	})
	if err != nil {
		s.failTask(job, fmt.Errorf("финализация задачи: %w", err))
		return
	}

	logger.Info("задача успешно завершена", slog.String("status", string(models.StatusCompleted)))
}

// failTask переводит задачу в failed или возвращает в created при отмене контекста и логирует причину.
func (s *processingService) failTask(job models.ProcessingJob, originalErr error) {
	if errors.Is(originalErr, context.Canceled) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.taskRepo.UpdateStatus(bgCtx, job.TaskID, models.StatusCreated); err != nil {
			slog.Error(
				"не удалось вернуть задачу в created после отмены",
				slog.Int64("task_id", job.TaskID),
				slog.String("error", err.Error()),
			)
		}

		return
	}

	bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.taskRepo.SetFailed(bgCtx, job.TaskID, originalErr.Error()); err != nil {
		slog.Error(
			"не удалось сохранить ошибку задачи",
			slog.Int64("task_id", job.TaskID),
			slog.String("error", err.Error()),
		)
	}

	slog.Error(
		"задача завершилась ошибкой",
		slog.Int64("task_id", job.TaskID),
		slog.Int64("meeting_id", job.MeetingID),
		slog.String("error", originalErr.Error()),
	)
}
