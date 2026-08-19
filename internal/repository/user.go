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

// UserRepo выполняет CRUD операции над пользователями в базе данных.
type UserRepo interface {
	Create(ctx context.Context, externalID string) (models.User, error)
	GetByExternalID(ctx context.Context, externalID string) (models.User, error)
}

type userRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo создаёт UserRepo, использующий заданный пул подключений.
func NewUserRepo(pool *pgxpool.Pool) UserRepo {
	return &userRepo{pool: pool}
}

func (r *userRepo) Create(ctx context.Context, externalID string) (models.User, error) {
	query := `
		INSERT INTO users (external_id)
		VALUES ($1)
		ON CONFLICT (external_id)
		DO UPDATE SET external_id = EXCLUDED.external_id
		RETURNING id, external_id, created_at;
	`

	var user models.User
	var row pgx.Row

	if tx, ok := database.TxFromContext(ctx); ok {
		row = tx.QueryRow(ctx, query, externalID)
	} else {
		row = r.pool.QueryRow(ctx, query, externalID)
	}

	err := row.Scan(&user.ID, &user.ExternalID, &user.CreatedAt)
	if err != nil {
		return models.User{}, fmt.Errorf("ошибка создания пользователя: %w", err)
	}

	user.ExternalID = externalID

	return user, nil
}

func (r *userRepo) GetByExternalID(ctx context.Context, externalID string) (models.User, error) {
	query := `
		SELECT id, external_id, created_at
		FROM users
		WHERE external_id = $1
	`

	var user models.User

	err := r.pool.QueryRow(ctx, query, externalID).Scan(&user.ID, &user.ExternalID, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, models.ErrNotFound
		}

		return models.User{}, fmt.Errorf("ошибка получения пользователя: %w", err)
	}

	return user, nil
}
