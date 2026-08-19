package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/olegsys/meeting-assistant/internal/llm"
	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
)

// ChatService отвечает на вопросы пользователя по материалам встречи через LLM.
type ChatService interface {
	Ask(ctx context.Context, externalID string, meetingID int64, question string) (string, error)
}

type chatService struct {
	userSvc     UserService
	contentRepo repository.ContentRepo
	chatRepo    repository.ChatRepo
	llmClient   llm.Client
}

// NewChatService собирает ChatService из переданных зависимостей.
func NewChatService(
	userSvc UserService,
	contentRepo repository.ContentRepo,
	chatRepo repository.ChatRepo,
	llmClient llm.Client,
) ChatService {
	return &chatService{
		userSvc:     userSvc,
		contentRepo: contentRepo,
		chatRepo:    chatRepo,
		llmClient:   llmClient,
	}
}

func (s *chatService) Ask(ctx context.Context, externalID string, meetingID int64, question string) (string, error) {
	if question == "" {
		return "", models.ErrInvalidInput
	}

	user, err := s.userSvc.Resolve(ctx, externalID)
	if err != nil {
		return "", fmt.Errorf("resolve user: %w", err)
	}

	transcript, summary, err := s.contentRepo.GetContextOwned(ctx, user.ID, meetingID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return "", models.ErrNotFound
		}

		return "", fmt.Errorf("получение материалов встречи: %w", err)
	}

	if transcript == "" && summary == "" {
		return "", models.ErrNoMaterials
	}

	materials := buildMaterials(transcript, summary)

	answer, err := s.llmClient.Ask(ctx, materials, question)
	if err != nil {
		return "", fmt.Errorf("llm ask: %w", err)
	}

	if err := s.chatRepo.Create(ctx, user.ID, meetingID, question, answer); err != nil {
		return "", fmt.Errorf("сохранение chat message: %w", err)
	}

	return answer, nil
}

// buildMaterials формирует строку контекста для LLM из транскрипции и выжимки.
func buildMaterials(transcript, summary string) string {
	var b strings.Builder

	if transcript != "" {
		b.WriteString("Транскрипция:\n")
		b.WriteString(transcript)
	}

	if summary != "" {
		if transcript != "" {
			b.WriteString("\n\n")
		}

		b.WriteString("Выжимка:\n")
		b.WriteString(summary)
	}

	return b.String()
}
