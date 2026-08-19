package speech

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/olegsys/meeting-assistant/internal/models"
)

// MockClient тестовая реализация Client, возвращающая предопределённую транскрипцию.
type MockClient struct{}

// NewMockClient создаёт новый экземпляр MockClient.
func NewMockClient() *MockClient {
	return &MockClient{}
}

func (c *MockClient) Transcribe(ctx context.Context, fileName string, content []byte) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if len(content) == 0 {
		return "", models.ErrEmptyFile
	}

	base := filepath.Base(fileName)

	if strings.Contains(strings.ToLower(base), "fail") {
		return "", errors.New("имитация ошибки speech client")
	}

	if strings.HasSuffix(strings.ToLower(base), ".txt") {
		if len(content) == 0 {
			return "", models.ErrEmptyTranscript
		}

		return string(content), nil
	}

	return fmt.Sprintf(
		"Тестовая транскрипция встречи %s. Обсудили сроки, ответственность и следующие шаги.",
		base,
	), nil
}
