package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/olegsys/meeting-assistant/internal/models"
)

// LMStudioClient реализация Client для LMStudio и любых OpenAI совместимых серверов.
type LMStudioClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewLMStudioClient создаёт клиент LMStudio с указанным базовым URL и именем модели.
func NewLMStudioClient(baseURL, model string) *LMStudioClient {
	return &LMStudioClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *LMStudioClient) chat(ctx context.Context, system, user string) (string, error) {
	req := chatRequest{
		Model:       c.model,
		Temperature: 0.3,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("lmstudio: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("lmstudio: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("lmstudio: http call: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("lmstudio: read body: %w", err)
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("lmstudio: decode response: %w (body=%s)", err, string(raw))
	}

	if out.Error != nil {
		return "", fmt.Errorf("lmstudio: %s", out.Error.Message)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("lmstudio: status %d: %s", resp.StatusCode, string(raw))
	}

	if len(out.Choices) == 0 {
		return "", models.ErrEmptyTranscript
	}

	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (c *LMStudioClient) Summarize(ctx context.Context, transcript string) (string, error) {
	if transcript == "" {
		return "", models.ErrEmptyTranscript
	}

	system := "Ты — ассистент, делающий краткие структурированные выжимки встреч на русском языке. " +
		"Выделяй ключевые решения, ответственных и сроки. Ответ — 5-8 пунктов."

	user := "Транскрипт встречи:\n\n" + transcript

	return c.chat(ctx, system, user)
}

func (c *LMStudioClient) Ask(ctx context.Context, materials, question string) (string, error) {
	if question == "" {
		return "", models.ErrInvalidInput
	}

	if materials == "" {
		return "", models.ErrNoMaterials
	}

	system := "Ты отвечаешь на вопросы по материалам встречи. " +
		"Используй только предоставленные материалы. Если ответа нет — так и скажи. " +
		"Отвечай кратко, по существу, на русском языке."

	user := fmt.Sprintf("Материалы:\n%s\n\nВопрос: %s", materials, question)

	return c.chat(ctx, system, user)
}
