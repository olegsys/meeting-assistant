package llm

import (
	"context"
	"fmt"

	"github.com/olegsys/meeting-assistant/internal/config"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// Client выполняет суммаризацию и отвечает на вопросы по материалам встречи.
type Client interface {
	Summarize(ctx context.Context, transcript string) (string, error)
	Ask(ctx context.Context, materials string, question string) (string, error)
}

// NewClient создаёт реализацию Client по имени провайдера.
func NewClient(provider string) (Client, error) {
	switch provider {
	case "", "mock":
		return NewMockClient(), nil
	case "lmstudio":
		cfg := config.GetConfig()
		return NewLMStudioClient(cfg.LLMBaseURL, cfg.LLMModel), nil
	default:
		return nil, fmt.Errorf("llm provider %q: %w", provider, models.ErrUnsupported)
	}
}
