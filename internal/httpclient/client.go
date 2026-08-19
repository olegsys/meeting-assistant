package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olegsys/meeting-assistant/internal/models"
)

// Client HTTP клиент для обращения к API meeting assistant из CLI утилиты.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New создаёт HTTP клиент с заданным базовым URL сервиса.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// apiError представляет формат ошибки, возвращаемой API.
type apiError struct {
	Error string `json:"error"`
}

// listResponse содержит список встреч пользователя от API.
type listResponse struct {
	Items []models.MeetingListItem `json:"items"`
}

// findResponse содержит результаты поиска от API.
type findResponse struct {
	Items []models.FindResult `json:"items"`
}

// loadResponse содержит идентификатор встречи и начальный статус от API.
type loadResponse struct {
	MeetingID int64                   `json:"meeting_id"`
	Status    models.ProcessingStatus `json:"status"`
}

// transcriptionResponse содержит текст транскрипции от API.
type transcriptionResponse struct {
	Content string `json:"content"`
}

// chatResponse содержит ответ LLM на вопрос от API.
type chatResponse struct {
	Answer string `json:"answer"`
}

// startRequest содержит идентификатор пользователя при регистрации.
type startRequest struct {
	UserID string `json:"user_id"`
}

// chatRequest содержит текст вопроса пользователя по встрече.
type chatRequest struct {
	Text string `json:"text"`
}

// Start регистрирует или получает пользователя по внешнему идентификатору.
func (c *Client) Start(ctx context.Context, userID string) (models.User, error) {
	body, err := json.Marshal(startRequest{UserID: userID})
	if err != nil {
		return models.User{}, fmt.Errorf("не удалось сериализовать запрос: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/start", bytes.NewReader(body))
	if err != nil {
		return models.User{}, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := c.do(req)
	if err != nil {
		return models.User{}, err
	}
	defer res.Body.Close()

	var user models.User

	if err := json.NewDecoder(res.Body).Decode(&user); err != nil {
		return models.User{}, fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return user, nil
}

// Load загружает файл с диска и отправляет его в API как новую встречу.
func (c *Client) Load(ctx context.Context, userID, path string) (loadResponse, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return loadResponse{}, fmt.Errorf("не удалось прочитать файл: %w", err)
	}

	fileName := filepath.Base(path)

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)

	fileField, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return loadResponse{}, fmt.Errorf("не удалось создать поле file: %w", err)
	}

	if _, err := fileField.Write(content); err != nil {
		return loadResponse{}, fmt.Errorf("не удалось записать файл: %w", err)
	}

	if err := writer.Close(); err != nil {
		return loadResponse{}, fmt.Errorf("не удалось завершить multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/meetings/load", &buf)
	if err != nil {
		return loadResponse{}, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return loadResponse{}, err
	}
	defer res.Body.Close()

	var result loadResponse

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return loadResponse{}, fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return result, nil
}

// List возвращает список встреч пользователя.
func (c *Client) List(ctx context.Context, userID string) ([]models.MeetingListItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/meetings", nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var result listResponse

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return result.Items, nil
}

// Status возвращает текущий статус обработки встречи.
func (c *Client) Status(ctx context.Context, userID string, meetingID int64) (models.StatusInfo, error) {
	path := fmt.Sprintf("%s/api/meetings/%d/status", c.baseURL, meetingID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return models.StatusInfo{}, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return models.StatusInfo{}, err
	}
	defer res.Body.Close()

	var info models.StatusInfo

	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return models.StatusInfo{}, fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return info, nil
}

// GetTranscription возвращает текст транскрипции встречи.
func (c *Client) GetTranscription(ctx context.Context, userID string, meetingID int64) (string, error) {
	path := fmt.Sprintf("%s/api/meetings/%d/transcription", c.baseURL, meetingID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var result transcriptionResponse

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return result.Content, nil
}

// Find выполняет поиск встреч пользователя по ключевому слову.
func (c *Client) Find(ctx context.Context, userID, keyword string) ([]models.FindResult, error) {
	values := url.Values{}
	values.Set("keyword", keyword)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/meetings/search?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var result findResponse

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return result.Items, nil
}

// Chat отправляет вопрос по встрече и возвращает ответ LLM.
func (c *Client) Chat(ctx context.Context, userID string, meetingID int64, text string) (string, error) {
	body, err := json.Marshal(chatRequest{Text: text})
	if err != nil {
		return "", fmt.Errorf("не удалось сериализовать запрос: %w", err)
	}

	path := fmt.Sprintf("%s/api/meetings/%d/chat", c.baseURL, meetingID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var result chatResponse

	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("не удалось декодировать ответ: %w", err)
	}

	return result.Answer, nil
}

// Retry ставит задачу обработки встречи в повторную обработку.
func (c *Client) Retry(ctx context.Context, userID string, meetingID int64) error {
	path := fmt.Sprintf("%s/api/meetings/%d/retry", c.baseURL, meetingID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("не удалось создать запрос: %w", err)
	}

	req.Header.Set("X-User-Id", userID)

	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка HTTP запроса: %w", err)
	}

	if res.StatusCode >= http.StatusBadRequest {
		defer res.Body.Close()

		var apiErr apiError

		if err := json.NewDecoder(res.Body).Decode(&apiErr); err == nil && apiErr.Error != "" {
			return nil, errors.New(apiErr.Error)
		}

		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}

	return res, nil
}
