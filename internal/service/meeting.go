package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
)

// MeetingService содержит бизнес-логику работы со встречами.
type MeetingService interface {
	Load(ctx context.Context, externalID, fileName string, content []byte) (int64, error)
	List(ctx context.Context, externalID string) ([]models.MeetingListItem, error)
	Status(ctx context.Context, externalID string, meetingID int64) (models.StatusInfo, error)
	GetTranscription(ctx context.Context, externalID string, meetingID int64) (string, error)
	Find(ctx context.Context, externalID, keyword string) ([]models.FindResult, error)
	Retry(ctx context.Context, externalID string, meetingID int64) error
}

type meetingService struct {
	userSvc     UserService
	meetingRepo repository.MeetingRepo
	fileRepo    repository.FileRepo
	taskRepo    repository.TaskRepo
	contentRepo repository.ContentRepo
	txManager   database.TxManager
}

// NewMeetingService собирает MeetingService из переданных зависимостей.
func NewMeetingService(
	userSvc UserService,
	meetingRepo repository.MeetingRepo,
	fileRepo repository.FileRepo,
	taskRepo repository.TaskRepo,
	contentRepo repository.ContentRepo,
	txManager database.TxManager,
) MeetingService {
	return &meetingService{
		userSvc:     userSvc,
		meetingRepo: meetingRepo,
		fileRepo:    fileRepo,
		taskRepo:    taskRepo,
		contentRepo: contentRepo,
		txManager:   txManager,
	}
}

func (s *meetingService) Load(ctx context.Context, externalID, fileName string, content []byte) (int64, error) {
	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return 0, fmt.Errorf("resolve user: %w", err)
	}

	if fileName == "" {
		return 0, models.ErrInvalidInput
	}

	if len(content) == 0 {
		return 0, models.ErrEmptyFile
	}

	title := filepath.Base(fileName)
	if title == "." || title == "/" {
		title = "meeting"
	}

	var meetingID int64

	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		meeting, err := s.meetingRepo.Create(txCtx, user.ID, title)
		if err != nil {
			return err
		}

		meetingID = meeting.ID

		if err := s.fileRepo.Save(txCtx, meetingID, fileName, content); err != nil {
			return err
		}

		_, err = s.taskRepo.Create(txCtx, meetingID)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("создание встречи, файла и задачи: %w", err)
	}

	return meetingID, nil
}

func (s *meetingService) List(ctx context.Context, externalID string) ([]models.MeetingListItem, error) {
	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return nil, fmt.Errorf("resolve user: %w", err)
	}

	items, err := s.meetingRepo.List(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("получение списка встреч: %w", err)
	}

	return items, nil
}

func (s *meetingService) Status(ctx context.Context, externalID string, meetingID int64) (models.StatusInfo, error) {
	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return models.StatusInfo{}, fmt.Errorf("resolve user: %w", err)
	}

	info, err := s.taskRepo.GetStatusOwned(ctx, user.ID, meetingID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return models.StatusInfo{}, models.ErrNotFound
		}

		return models.StatusInfo{}, fmt.Errorf("получение статуса: %w", err)
	}

	return info, nil
}

func (s *meetingService) GetTranscription(ctx context.Context, externalID string, meetingID int64) (string, error) {
	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return "", fmt.Errorf("resolve user: %w", err)
	}

	content, err := s.contentRepo.GetTranscriptionOwned(ctx, user.ID, meetingID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return "", models.ErrNotFound
		}

		return "", fmt.Errorf("получение транскрипции: %w", err)
	}

	return content, nil
}

func (s *meetingService) Find(ctx context.Context, externalID, keyword string) ([]models.FindResult, error) {
	if keyword == "" {
		return nil, models.ErrInvalidInput
	}

	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return nil, fmt.Errorf("resolve user: %w", err)
	}

	results, err := s.meetingRepo.Find(ctx, user.ID, keyword)
	if err != nil {
		return nil, fmt.Errorf("поиск встреч: %w", err)
	}

	return results, nil
}

func (s *meetingService) Retry(ctx context.Context, externalID string, meetingID int64) error {
	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	if err := s.taskRepo.RetryFailedOwned(ctx, user.ID, meetingID); err != nil {
		return fmt.Errorf("retry задачи: %w", err)
	}

	return nil
}
