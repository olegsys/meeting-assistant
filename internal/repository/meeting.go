package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// MeetingRepo выполняет операции над встречами и поиск по ним.
type MeetingRepo interface {
	Create(ctx context.Context, userID int64, title string) (models.Meeting, error)
	List(ctx context.Context, userID int64) ([]models.MeetingListItem, error)
	Find(ctx context.Context, userID int64, keyword string) ([]models.FindResult, error)
}

type meetingRepo struct {
	pool *pgxpool.Pool
}

// NewMeetingRepo создаёт MeetingRepo, использующий заданный пул подключений.
func NewMeetingRepo(pool *pgxpool.Pool) MeetingRepo {
	return &meetingRepo{pool: pool}
}

func (r *meetingRepo) Create(ctx context.Context, userID int64, title string) (models.Meeting, error) {
	query := `
		INSERT INTO meetings (user_id, title)
		VALUES ($1, $2)
		RETURNING id, created_at
	`

	var meeting models.Meeting
	var row pgx.Row

	if tx, ok := database.TxFromContext(ctx); ok {
		row = tx.QueryRow(ctx, query, userID, title)
	} else {
		row = r.pool.QueryRow(ctx, query, userID, title)
	}

	err := row.Scan(&meeting.ID, &meeting.CreatedAt)
	if err != nil {
		return models.Meeting{}, fmt.Errorf("ошибка создания встречи: %w", err)
	}

	meeting.UserID = userID
	meeting.Title = title

	return meeting, nil
}

func (r *meetingRepo) List(ctx context.Context, userID int64) ([]models.MeetingListItem, error) {
	query := `
		SELECT
			m.id,
			m.title,
			m.created_at,
			pt.status,
			COALESCE(s.content, '') AS summary
		FROM meetings m
		JOIN processing_tasks pt ON pt.meeting_id = m.id
		LEFT JOIN summaries s ON s.meeting_id = m.id
		WHERE m.user_id = $1
		ORDER BY m.created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("ошибка запроса списка встреч: %w", err)
	}
	defer rows.Close()

	items := make([]models.MeetingListItem, 0)

	for rows.Next() {
		var item models.MeetingListItem
		var status string

		if err := rows.Scan(&item.MeetingID, &item.Title, &item.CreatedAt, &status, &item.Summary); err != nil {
			return nil, fmt.Errorf("ошибка чтения встречи: %w", err)
		}

		item.Status = models.ProcessingStatus(status)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по встречам: %w", err)
	}

	return items, nil
}

func (r *meetingRepo) Find(ctx context.Context, userID int64, keyword string) ([]models.FindResult, error) {
	query := `
		SELECT
			m.id,
			m.title,
			m.created_at,
			pt.status,
			COALESCE(s.content, t.content, '') AS snippet
		FROM meetings m
		JOIN processing_tasks pt ON pt.meeting_id = m.id
		LEFT JOIN transcriptions t ON t.meeting_id = m.id
		LEFT JOIN summaries s ON s.meeting_id = m.id
		WHERE m.user_id = $1
		  AND (
				to_tsvector('simple', COALESCE(t.content, '')) @@ websearch_to_tsquery('simple', $2)
				OR to_tsvector('simple', COALESCE(s.content, '')) @@ websearch_to_tsquery('simple', $2)
				OR m.title ILIKE '%' || $2 || '%'
		  )
		ORDER BY m.created_at DESC
		LIMIT 50
	`

	rows, err := r.pool.Query(ctx, query, userID, keyword)
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска встреч: %w", err)
	}
	defer rows.Close()

	results := make([]models.FindResult, 0)

	for rows.Next() {
		var item models.FindResult
		var status string

		if err := rows.Scan(&item.MeetingID, &item.Title, &item.CreatedAt, &status, &item.Snippet); err != nil {
			return nil, fmt.Errorf("ошибка чтения результата поиска: %w", err)
		}

		item.Status = models.ProcessingStatus(status)
		results = append(results, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по результатам поиска: %w", err)
	}

	return results, nil
}
