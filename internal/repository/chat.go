package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// ChatRepo сохраняет историю вопросов и ответов пользователя по встречам.
type ChatRepo interface {
	Create(ctx context.Context, userID, meetingID int64, question, answer string) error
}

type chatRepo struct {
	pool *pgxpool.Pool
}

// NewChatRepo создаёт ChatRepo, использующий заданный пул подключений.
func NewChatRepo(pool *pgxpool.Pool) ChatRepo {
	return &chatRepo{pool: pool}
}

func (r *chatRepo) Create(ctx context.Context, userID, meetingID int64, question, answer string) error {
	query := `
		INSERT INTO chat_messages (user_id, meeting_id, question, answer)
		SELECT $1, $2, $3, $4
		WHERE EXISTS (
			SELECT 1
			FROM meetings
			WHERE id = $2 AND user_id = $1
		)
	`

	var tag pgconn.CommandTag
	var err error

	if tx, ok := database.TxFromContext(ctx); ok {
		tag, err = tx.Exec(ctx, query, userID, meetingID, question, answer)
	} else {
		tag, err = r.pool.Exec(ctx, query, userID, meetingID, question, answer)
	}

	if err != nil {
		return fmt.Errorf("ошибка сохранения chat message: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return models.ErrAccessDenied
	}

	return nil
}
