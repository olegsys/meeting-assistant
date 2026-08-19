package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// TaskRepo управляет жизненным циклом задач обработки встреч.
type TaskRepo interface {
	Create(ctx context.Context, meetingID int64) (int64, error)
	GetStatusOwned(ctx context.Context, userID, meetingID int64) (models.StatusInfo, error)
	ClaimCreated(ctx context.Context, limit int) ([]models.ProcessingJob, error)
	UpdateStatus(ctx context.Context, taskID int64, status models.ProcessingStatus) error
	SetFailed(ctx context.Context, taskID int64, message string) error
	RequeueStale(ctx context.Context, staleAfter time.Duration, maxAttempts int) (int64, error)
	RetryFailedOwned(ctx context.Context, userID, meetingID int64) error
}

type taskRepo struct {
	pool *pgxpool.Pool
}

// NewTaskRepo создаёт TaskRepo, использующий заданный пул подключений.
func NewTaskRepo(pool *pgxpool.Pool) TaskRepo {
	return &taskRepo{pool: pool}
}

func (r *taskRepo) Create(ctx context.Context, meetingID int64) (int64, error) {
	query := `
		INSERT INTO processing_tasks (meeting_id, status)
		VALUES ($1, 'created')
		RETURNING id
	`

	var id int64
	var row pgx.Row

	if tx, ok := database.TxFromContext(ctx); ok {
		row = tx.QueryRow(ctx, query, meetingID)
	} else {
		row = r.pool.QueryRow(ctx, query, meetingID)
	}

	err := row.Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ошибка создания задачи обработки: %w", err)
	}

	return id, nil
}

func (r *taskRepo) GetStatusOwned(ctx context.Context, userID, meetingID int64) (models.StatusInfo, error) {
	query := `
		SELECT
			m.id,
			pt.status,
			pt.created_at,
			pt.updated_at,
			COALESCE(pt.error_message, '')
		FROM meetings m
		JOIN processing_tasks pt ON pt.meeting_id = m.id
		WHERE m.id = $1 AND m.user_id = $2
	`

	var info models.StatusInfo
	var status string

	err := r.pool.QueryRow(ctx, query, meetingID, userID).Scan(
		&info.MeetingID,
		&status,
		&info.CreatedAt,
		&info.UpdatedAt,
		&info.ErrorMessage,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.StatusInfo{}, models.ErrNotFound
		}

		return models.StatusInfo{}, fmt.Errorf("ошибка получения статуса задачи: %w", err)
	}

	info.Status = models.ProcessingStatus(status)

	return info, nil
}

func (r *taskRepo) ClaimCreated(ctx context.Context, limit int) ([]models.ProcessingJob, error) {
	query := `
		WITH cte AS (
			SELECT id
			FROM processing_tasks
			WHERE status = 'created'
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE processing_tasks t
		SET status = 'processing',
		    attempts = t.attempts + 1,
		    updated_at = now()
		FROM cte, meetings m
		WHERE t.id = cte.id
		  AND t.meeting_id = m.id
		RETURNING t.id, t.meeting_id, m.user_id
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("ошибка claim задач: %w", err)
	}
	defer rows.Close()

	jobs := make([]models.ProcessingJob, 0)

	for rows.Next() {
		var job models.ProcessingJob

		if err := rows.Scan(&job.TaskID, &job.MeetingID, &job.UserID); err != nil {
			return nil, fmt.Errorf("ошибка чтения задачи: %w", err)
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по задачам: %w", err)
	}

	return jobs, nil
}

func (r *taskRepo) UpdateStatus(ctx context.Context, taskID int64, status models.ProcessingStatus) error {
	query := `
		UPDATE processing_tasks
		SET status = $2,
		    error_message = '',
		    updated_at = now()
		WHERE id = $1
	`

	var tag pgconn.CommandTag
	var err error

	if tx, ok := database.TxFromContext(ctx); ok {
		tag, err = tx.Exec(ctx, query, taskID, status)
	} else {
		tag, err = r.pool.Exec(ctx, query, taskID, status)
	}

	if err != nil {
		return fmt.Errorf("ошибка обновления статуса задачи: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *taskRepo) SetFailed(ctx context.Context, taskID int64, message string) error {
	query := `
		UPDATE processing_tasks
		SET status = 'failed',
		    error_message = $2,
		    updated_at = now()
		WHERE id = $1
	`

	var tag pgconn.CommandTag
	var err error

	if tx, ok := database.TxFromContext(ctx); ok {
		tag, err = tx.Exec(ctx, query, taskID, message)
	} else {
		tag, err = r.pool.Exec(ctx, query, taskID, message)
	}

	if err != nil {
		return fmt.Errorf("ошибка перевода задачи в failed: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *taskRepo) RequeueStale(ctx context.Context, staleAfter time.Duration, maxAttempts int) (int64, error) {
	seconds := int64(staleAfter.Seconds())
	if seconds <= 0 {
		seconds = 1
	}

	query := `
		UPDATE processing_tasks
		SET status = CASE WHEN attempts < $2 THEN 'created' ELSE 'failed' END,
		    error_message = CASE WHEN attempts < $2 THEN '' ELSE 'task processing timeout' END,
		    updated_at = now()
		WHERE status = 'processing'
		  AND updated_at < now() - ($1::int * interval '1 second')
	`

	result, err := r.pool.Exec(ctx, query, seconds, maxAttempts)
	if err != nil {
		return 0, fmt.Errorf("ошибка requeue stale задач: %w", err)
	}

	return result.RowsAffected(), nil
}

func (r *taskRepo) RetryFailedOwned(ctx context.Context, userID, meetingID int64) error {
	statusQuery := `
		SELECT pt.status
		FROM processing_tasks pt
		JOIN meetings m ON m.id = pt.meeting_id
		WHERE m.id = $1 AND m.user_id = $2
	`

	var status string

	err := r.pool.QueryRow(ctx, statusQuery, meetingID, userID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrNotFound
		}

		return fmt.Errorf("ошибка получения задачи для retry: %w", err)
	}

	if models.ProcessingStatus(status) != models.StatusFailed {
		return models.ErrTaskNotFailed
	}

	updateQuery := `
		UPDATE processing_tasks
		SET status = 'created',
		    error_message = '',
		    attempts = 0,
		    updated_at = now()
		WHERE meeting_id = $1 AND status = 'failed'
	`

	result, err := r.pool.Exec(ctx, updateQuery, meetingID)
	if err != nil {
		return fmt.Errorf("ошибка retry задачи: %w", err)
	}

	if result.RowsAffected() == 0 {
		return models.ErrNotFound
	}

	return nil
}
