package llm

import (
	"context"
	"fmt"

	"github.com/olegsys/meeting-assistant/internal/models"
)

// MockClient тестовая реализация Client, возвращающая предопределённые суммаризацию и ответы.
type MockClient struct{}

// NewMockClient создаёт новый экземпляр MockClient.
func NewMockClient() *MockClient {
	return &MockClient{}
}

func (c *MockClient) Summarize(ctx context.Context, transcript string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if transcript == "" {
		return "", models.ErrEmptyTranscript
	}

	return fmt.Sprintf("Выжимка: %s", truncate(transcript, 200)), nil
}

func (c *MockClient) Ask(ctx context.Context, materials string, question string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if question == "" {
		return "", models.ErrInvalidInput
	}

	if materials == "" {
		return "", models.ErrNoMaterials
	}

	return fmt.Sprintf("Ответ по материалам встречи на вопрос %q: используйте транскрипцию и выжимку.", question), nil
}

// truncate обрезает строку до n рун и добавляет многоточие, если строка длиннее.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}

	return string(runes[:n]) + "..."
}
