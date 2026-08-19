package models

import "time"

// User представляет зарегистрированного пользователя системы.
type User struct {
	ID         int64     `json:"id"`
	ExternalID string    `json:"external_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// Meeting представляет встречу, загруженную пользователем.
type Meeting struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"-"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// ProcessingStatus описывает состояние задачи обработки встречи.
type ProcessingStatus string

const (
	// StatusCreated означает, что задача создана и ожидает воркер.
	StatusCreated ProcessingStatus = "created"
	// StatusProcessing означает, что задача взята воркером и обрабатывается.
	StatusProcessing ProcessingStatus = "processing"
	// StatusTranscribed означает, что транскрипция получена и сохранена.
	StatusTranscribed ProcessingStatus = "transcribed"
	// StatusSummarized означает, что выжимка получена и сохранена.
	StatusSummarized ProcessingStatus = "summarized"
	// StatusCompleted означает, что задача полностью завершена успешно.
	StatusCompleted ProcessingStatus = "completed"
	// StatusFailed означает, что обработка завершилась ошибкой.
	StatusFailed ProcessingStatus = "failed"
)

// ProcessingJob содержит идентификаторы задачи, встречи и пользователя для передачи в воркер.
type ProcessingJob struct {
	TaskID    int64
	MeetingID int64
	UserID    int64
}

// StatusInfo описывает текущее состояние задачи обработки для ответа API.
type StatusInfo struct {
	MeetingID    int64            `json:"meeting_id"`
	Status       ProcessingStatus `json:"status"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	ErrorMessage string           `json:"error_message"`
}

// MeetingListItem представляет встречу в списке встреч пользователя.
type MeetingListItem struct {
	MeetingID int64            `json:"meeting_id"`
	Title     string           `json:"title"`
	CreatedAt time.Time        `json:"created_at"`
	Status    ProcessingStatus `json:"status"`
	Summary   string           `json:"summary"`
}

// FindResult представляет один результат поиска по встречам пользователя.
type FindResult struct {
	MeetingID int64            `json:"meeting_id"`
	Title     string           `json:"title"`
	CreatedAt time.Time        `json:"created_at"`
	Status    ProcessingStatus `json:"status"`
	Snippet   string           `json:"snippet"`
}
