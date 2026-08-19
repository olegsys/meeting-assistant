package speech

import (
	"context"
	"fmt"

	"github.com/olegsys/meeting-assistant/internal/models"
)

// Client распознаёт аудио и возвращает текстовую транскрипцию.
type Client interface {
	Transcribe(ctx context.Context, fileName string, content []byte) (string, error)
}

// NewClient создаёт реализацию Client по имени провайдера.
func NewClient(provider string) (Client, error) {
	switch provider {
	case "", "mock":
		return NewMockClient(), nil
	default:
		return nil, fmt.Errorf("speech provider %q: %w", provider, models.ErrUnsupported)
	}
}
