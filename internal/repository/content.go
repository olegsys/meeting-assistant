package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// ContentRepo сохраняет и читает транскрипции и выжимки встреч.
type ContentRepo interface {
	UpsertTranscription(ctx context.Context, meetingID int64, content string) error
	UpsertSummary(ctx context.Context, meetingID int64, content string) error
	GetTranscriptionOwned(ctx context.Context, userID, meetingID int64) (string, error)
	GetContextOwned(ctx context.Context, userID, meetingID int64) (string, string, error)
}

type contentRepo struct {
	pool *pgxpool.Pool
}

// NewContentRepo создаёт ContentRepo, использующий заданный пул подключений.
func NewContentRepo(pool *pgxpool.Pool) ContentRepo {
	return &contentRepo{pool: pool}
}

func (r *contentRepo) UpsertTranscription(ctx context.Context, meetingID int64, content string) error {
	query := `
		INSERT INTO transcriptions (meeting_id, content)
		VALUES ($1, $2)
		ON CONFLICT (meeting_id) DO NOTHING
	`

	var tag pgconn.CommandTag
	var err error

	if tx, ok := database.TxFromContext(ctx); ok {
		tag, err = tx.Exec(ctx, query, meetingID, content)
	} else {
		tag, err = r.pool.Exec(ctx, query, meetingID, content)
	}

	if err != nil {
		return fmt.Errorf("ошибка сохранения транскрипции: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return models.ErrTranscriptionAlreadyExists
	}

	return nil
}

func (r *contentRepo) UpsertSummary(ctx context.Context, meetingID int64, content string) error {
	query := `
		INSERT INTO summaries (meeting_id, content)
		VALUES ($1, $2)
		ON CONFLICT (meeting_id) DO NOTHING
	`

	var tag pgconn.CommandTag
	var err error

	if tx, ok := database.TxFromContext(ctx); ok {
		tag, err = tx.Exec(ctx, query, meetingID, content)
	} else {
		tag, err = r.pool.Exec(ctx, query, meetingID, content)
	}

	if err != nil {
		return fmt.Errorf("ошибка сохранения выжимки: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return models.ErrSummaryAlreadyExists
	}

	return nil
}

func (r *contentRepo) GetTranscriptionOwned(ctx context.Context, userID, meetingID int64) (string, error) {
	query := `
		SELECT t.content
		FROM transcriptions t
		JOIN meetings m ON m.id = t.meeting_id
		WHERE m.id = $1 AND m.user_id = $2
	`

	var content string

	err := r.pool.QueryRow(ctx, query, meetingID, userID).Scan(&content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", models.ErrNotFound
		}

		return "", fmt.Errorf("ошибка получения транскрипции: %w", err)
	}

	return content, nil
}

func (r *contentRepo) GetContextOwned(ctx context.Context, userID, meetingID int64) (string, string, error) {
	query := `
		SELECT
			COALESCE(t.content, ''),
			COALESCE(s.content, '')
		FROM meetings m
		LEFT JOIN transcriptions t ON t.meeting_id = m.id
		LEFT JOIN summaries s ON s.meeting_id = m.id
		WHERE m.id = $1 AND m.user_id = $2
	`

	var transcript string
	var summary string

	err := r.pool.QueryRow(ctx, query, meetingID, userID).Scan(&transcript, &summary)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", models.ErrNotFound
		}

		return "", "", fmt.Errorf("ошибка получения материалов встречи: %w", err)
	}

	return transcript, summary, nil
}
