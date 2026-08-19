package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/service"
	"github.com/olegsys/meeting-assistant/internal/service/mocks"
)

func TestUserService_Resolve_EmptyExternalID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockUserRepo(ctrl)
	svc := service.NewUserService(repo)

	user, err := svc.Resolve(context.Background(), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrInvalidInput)
	assert.Equal(t, models.User{}, user)
}

func TestUserService_Resolve_UserExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockUserRepo(ctrl)
	svc := service.NewUserService(repo)

	existing := models.User{
		ID:         42,
		ExternalID: "alice",
		CreatedAt:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	repo.EXPECT().
		GetByExternalID(gomock.Any(), "alice").
		Return(existing, nil).
		Times(1)

	user, err := svc.Resolve(context.Background(), "alice")

	require.NoError(t, err)
	assert.Equal(t, existing, user)
}

func TestUserService_Resolve_UserNotFound_CreatesNew(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockUserRepo(ctrl)
	svc := service.NewUserService(repo)

	created := models.User{
		ID:         7,
		ExternalID: "bob",
		CreatedAt:  time.Now().UTC(),
	}

	gomock.InOrder(
		repo.EXPECT().
			GetByExternalID(gomock.Any(), "bob").
			Return(models.User{}, models.ErrNotFound),
		repo.EXPECT().
			Create(gomock.Any(), "bob").
			Return(created, nil),
	)

	user, err := svc.Resolve(context.Background(), "bob")

	require.NoError(t, err)
	assert.Equal(t, created, user)
}

func TestUserService_Resolve_GetByExternalID_OtherError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockUserRepo(ctrl)
	svc := service.NewUserService(repo)

	repo.EXPECT().
		GetByExternalID(gomock.Any(), "alice").
		Return(models.User{}, errors.New("connection refused"))

	_, err := svc.Resolve(context.Background(), "alice")

	require.Error(t, err)
	assert.NotErrorIs(t, err, models.ErrNotFound)
	assert.Contains(t, err.Error(), "connection refused")
	assert.Contains(t, err.Error(), "поиск пользователя")
}

func TestUserService_Resolve_CreateError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockUserRepo(ctrl)
	svc := service.NewUserService(repo)

	gomock.InOrder(
		repo.EXPECT().
			GetByExternalID(gomock.Any(), "new-user").
			Return(models.User{}, models.ErrNotFound),
		repo.EXPECT().
			Create(gomock.Any(), "new-user").
			Return(models.User{}, errors.New("db unavailable")),
	)

	_, err := svc.Resolve(context.Background(), "new-user")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "db unavailable")
	assert.Contains(t, err.Error(), "создание пользователя")
}

func TestUserService_Start_DelegatesToResolve(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mocks.NewMockUserRepo(ctrl)
	svc := service.NewUserService(repo)

	existing := models.User{ID: 1, ExternalID: "alice"}

	repo.EXPECT().
		GetByExternalID(gomock.Any(), "alice").
		Return(existing, nil)

	user, err := svc.Start(context.Background(), "alice")

	require.NoError(t, err)
	assert.Equal(t, existing, user)
}
