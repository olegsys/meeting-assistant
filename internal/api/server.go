package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/olegsys/meeting-assistant/internal/models"
)

// UserService описывает зависимость транспортного слоя от сервиса пользователей.
type UserService interface {
	Start(ctx context.Context, externalID string) (models.User, error)
}

// MeetingService описывает зависимость транспортного слоя от сервиса встреч.
type MeetingService interface {
	Load(ctx context.Context, externalID, fileName string, content []byte) (int64, error)
	List(ctx context.Context, externalID string) ([]models.MeetingListItem, error)
	Status(ctx context.Context, externalID string, meetingID int64) (models.StatusInfo, error)
	GetTranscription(ctx context.Context, externalID string, meetingID int64) (string, error)
	Find(ctx context.Context, externalID, keyword string) ([]models.FindResult, error)
	Retry(ctx context.Context, externalID string, meetingID int64) error
}

// ChatService описывает зависимость транспортного слоя от сервиса вопросов по встречам.
type ChatService interface {
	Ask(ctx context.Context, externalID string, meetingID int64, question string) (string, error)
}

// Handler собирает HTTP маршруты приложения.
type Handler interface {
	Routes() http.Handler
}

type handler struct {
	userSvc        UserService
	meetingSvc     MeetingService
	chatSvc        ChatService
	maxUploadBytes int64
}

// NewHandler собирает Handler из переданных сервисов и лимита размера загрузки.
func NewHandler(userSvc UserService, meetingSvc MeetingService, chatSvc ChatService, maxUploadBytes int64) Handler {
	return &handler{
		userSvc:        userSvc,
		meetingSvc:     meetingSvc,
		chatSvc:        chatSvc,
		maxUploadBytes: maxUploadBytes,
	}
}

func (h *handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/start", h.start)
	mux.HandleFunc("POST /api/meetings/load", h.load)
	mux.HandleFunc("GET /api/meetings", h.list)
	mux.HandleFunc("GET /api/meetings/search", h.find)
	mux.HandleFunc("GET /api/meetings/{id}/status", h.status)
	mux.HandleFunc("GET /api/meetings/{id}/transcription", h.getTranscription)
	mux.HandleFunc("POST /api/meetings/{id}/chat", h.chat)
	mux.HandleFunc("POST /api/meetings/{id}/retry", h.retry)

	return mux
}

// startRequest содержит идентификатор пользователя при регистрации.
type startRequest struct {
	UserID string `json:"user_id"`
}

// chatRequest содержит текст вопроса пользователя по встрече.
type chatRequest struct {
	Text string `json:"text"`
}

// errorResponse содержит сообщение об ошибке для клиента.
type errorResponse struct {
	Error string `json:"error"`
}

// listResponse содержит список встреч пользователя.
type listResponse struct {
	Items []models.MeetingListItem `json:"items"`
}

// findResponse содержит результаты поиска по встречам.
type findResponse struct {
	Items []models.FindResult `json:"items"`
}

// loadResponse содержит идентификатор созданной встречи и начальный статус.
type loadResponse struct {
	MeetingID int64                   `json:"meeting_id"`
	Status    models.ProcessingStatus `json:"status"`
}

// transcriptionResponse содержит текст транскрипции встречи.
type transcriptionResponse struct {
	Content string `json:"content"`
}

// chatResponse содержит ответ LLM на вопрос пользователя.
type chatResponse struct {
	Answer string `json:"answer"`
}

// retryResponse содержит статус задачи после постановки в повторную обработку.
type retryResponse struct {
	Status models.ProcessingStatus `json:"status"`
}

func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[startRequest](r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	user, err := h.userSvc.Start(r.Context(), req.UserID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *handler) load(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)

	if err := r.ParseMultipartForm(h.maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	userID := extractUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	meetingID, err := h.meetingSvc.Load(r.Context(), userID, header.Filename, content)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, loadResponse{
		MeetingID: meetingID,
		Status:    models.StatusCreated,
	})
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	items, err := h.meetingSvc.List(r.Context(), userID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, listResponse{
		Items: items,
	})
}

func (h *handler) find(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	keyword := r.URL.Query().Get("keyword")

	if userID == "" || keyword == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	items, err := h.meetingSvc.Find(r.Context(), userID, keyword)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, findResponse{
		Items: items,
	})
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	meetingID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	userID := extractUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	info, err := h.meetingSvc.Status(r.Context(), userID, meetingID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, info)
}

func (h *handler) getTranscription(w http.ResponseWriter, r *http.Request) {
	meetingID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	userID := extractUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	content, err := h.meetingSvc.GetTranscription(r.Context(), userID, meetingID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, transcriptionResponse{
		Content: content,
	})
}

func (h *handler) chat(w http.ResponseWriter, r *http.Request) {
	meetingID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	userID := extractUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	req, err := decodeJSON[chatRequest](r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	answer, err := h.chatSvc.Ask(r.Context(), userID, meetingID, req.Text)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{
		Answer: answer,
	})
}

func (h *handler) retry(w http.ResponseWriter, r *http.Request) {
	meetingID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	userID := extractUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, models.ErrInvalidInput)
		return
	}

	if err := h.meetingSvc.Retry(r.Context(), userID, meetingID); err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, retryResponse{
		Status: models.StatusCreated,
	})
}

// handleError маппит доменную ошибку в HTTP статус и пишет JSON ответ.
func (h *handler) handleError(w http.ResponseWriter, err error) {
	status := httpStatus(err)

	if status == http.StatusInternalServerError {
		slog.Error("внутренняя ошибка API", slog.String("error", err.Error()))
	}

	writeError(w, status, err)
}

// httpStatus возвращает HTTP статус код для заданной доменной ошибки.
func httpStatus(err error) int {
	switch {
	case errors.Is(err, models.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, models.ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, models.ErrEmptyFile):
		return http.StatusBadRequest
	case errors.Is(err, models.ErrAccessDenied):
		return http.StatusForbidden
	case errors.Is(err, models.ErrNoMaterials):
		return http.StatusConflict
	case errors.Is(err, models.ErrTaskNotFailed):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// parseID парсит строковый идентификатор из URL в положительный int64.
func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, models.ErrInvalidInput
	}

	if id <= 0 {
		return 0, models.ErrInvalidInput
	}

	return id, nil
}

// extractUserID извлекает идентификатор пользователя из заголовка X-User-Id или query параметра.
func extractUserID(r *http.Request) string {
	if id := r.Header.Get("X-User-Id"); id != "" {
		return id
	}

	if id := r.URL.Query().Get("user_id"); id != "" {
		return id
	}

	return ""
}

// writeJSON сериализует значение в JSON и пишет ответ с указанным статус кодом.
func writeJSON[T any](w http.ResponseWriter, status int, body T) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error(
			"ошибка кодирования JSON ответа",
			slog.String("error", err.Error()),
		)
	}
}

// decodeJSON декодирует JSON тело запроса в значение типа T.
func decodeJSON[T any](body io.Reader) (T, error) {
	var result T

	if err := json.NewDecoder(body).Decode(&result); err != nil {
		slog.Debug(
			"ошибка декодирования JSON запроса",
			slog.String("error", err.Error()),
		)

		return result, models.ErrInvalidInput
	}

	return result, nil
}

// writeError пишет JSON ответ с ошибкой и указанным HTTP статус кодом.
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{
		Error: err.Error(),
	})
}
