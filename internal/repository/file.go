package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// FileRepo сохраняет и читает содержимое загруженных файлов встреч.
type FileRepo interface {
	Save(ctx context.Context, meetingID int64, fileName string, content []byte) error
	Get(ctx context.Context, meetingID int64) (string, []byte, error)
}

type fileRepo struct {
	pool *pgxpool.Pool
}

// NewFileRepo создаёт FileRepo, использующий заданный пул подключений.
func NewFileRepo(pool *pgxpool.Pool) FileRepo {
	return &fileRepo{pool: pool}
}

func (r *fileRepo) Save(ctx context.Context, meetingID int64, fileName string, content []byte) error {
	query := `
		INSERT INTO meeting_files (meeting_id, file_name, content)
		VALUES ($1, $2, $3)
	`

	var err error

	if tx, ok := database.TxFromContext(ctx); ok {
		_, err = tx.Exec(ctx, query, meetingID, fileName, content)
	} else {
		_, err = r.pool.Exec(ctx, query, meetingID, fileName, content)
	}

	if err != nil {
		return fmt.Errorf("ошибка сохранения файла встречи: %w", err)
	}

	return nil
}

func (r *fileRepo) Get(ctx context.Context, meetingID int64) (string, []byte, error) {
	query := `
		SELECT file_name, content
		FROM meeting_files
		WHERE meeting_id = $1
	`

	var fileName string
	var content []byte

	err := r.pool.QueryRow(ctx, query, meetingID).Scan(&fileName, &content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, models.ErrNotFound
		}

		return "", nil, fmt.Errorf("ошибка получения файла встречи: %w", err)
	}

	return fileName, content, nil
}
