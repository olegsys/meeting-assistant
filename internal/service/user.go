package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
)

// UserService регистрирует и разрешает пользователей по внешнему идентификатору.
type UserService interface {
	Start(ctx context.Context, externalID string) (models.User, error)
	Resolve(ctx context.Context, externalID string) (models.User, error)
}

type userService struct {
	userRepo repository.UserRepo
}

// NewUserService создаёт UserService поверх заданного UserRepo.
func NewUserService(userRepo repository.UserRepo) UserService {
	return &userService{userRepo: userRepo}
}

func (s *userService) Start(ctx context.Context, externalID string) (models.User, error) {
	return s.Resolve(ctx, externalID)
}

func (s *userService) Resolve(ctx context.Context, externalID string) (models.User, error) {
	if externalID == "" {
		return models.User{}, models.ErrInvalidInput
	}

	user, err := s.userRepo.GetByExternalID(ctx, externalID)
	if err == nil {
		return user, nil
	}

	if !errors.Is(err, models.ErrNotFound) {
		return models.User{}, fmt.Errorf("поиск пользователя: %w", err)
	}

	created, err := s.userRepo.Create(ctx, externalID)
	if err != nil {
		return models.User{}, fmt.Errorf("создание пользователя: %w", err)
	}

	return created, nil
}
