package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/service"
	"github.com/olegsys/meeting-assistant/internal/service/mocks"
)

func newChatService(
	t *testing.T,
	userSvc service.UserService,
	contentRepo *mocks.MockContentRepo,
	chatRepo *mocks.MockChatRepo,
	llm *mocks.MockLLMClient,
) service.ChatService {
	t.Helper()
	return service.NewChatService(userSvc, contentRepo, chatRepo, llm)
}

func TestChatService_Ask_EmptyQuestion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := newChatService(t,
		mocks.NewMockUserService(ctrl),
		mocks.NewMockContentRepo(ctrl),
		mocks.NewMockChatRepo(ctrl),
		mocks.NewMockLLMClient(ctrl),
	)

	answer, err := svc.Ask(context.Background(), "alice", 10, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrInvalidInput)
	assert.Equal(t, "", answer)
}

func TestChatService_Ask_SuccessWithTranscriptAndSummary(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(10)).
		Return("транскрипция встречи", "краткая выжимка", nil)

	// ожидаем, что в LLM уйдут обе части материалов
	llm.EXPECT().
		Ask(gomock.Any(), gomock.Any(), "что решили?").
		DoAndReturn(func(_ context.Context, materials, _ string) (string, error) {
			assert.Contains(t, materials, "транскрипция встречи")
			assert.Contains(t, materials, "краткая выжимка")
			assert.Contains(t, materials, "Транскрипция:")
			assert.Contains(t, materials, "Выжимка:")
			return "ответ LLM", nil
		})

	chatRepo.EXPECT().Create(gomock.Any(), int64(1), int64(10), "что решили?", "ответ LLM").Return(nil)

	answer, err := svc.Ask(context.Background(), "alice", 10, "что решили?")

	require.NoError(t, err)
	assert.Equal(t, "ответ LLM", answer)
}

func TestChatService_Ask_SuccessWithOnlyTranscript(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(10)).
		Return("только транскрипция", "", nil)

	llm.EXPECT().
		Ask(gomock.Any(), gomock.Any(), "вопрос").
		DoAndReturn(func(_ context.Context, materials, _ string) (string, error) {
			assert.Contains(t, materials, "только транскрипция")
			assert.NotContains(t, materials, "Выжимка:")
			return "ответ", nil
		})

	chatRepo.EXPECT().Create(gomock.Any(), int64(1), int64(10), "вопрос", "ответ").Return(nil)

	answer, err := svc.Ask(context.Background(), "alice", 10, "вопрос")

	require.NoError(t, err)
	assert.Equal(t, "ответ", answer)
}

func TestChatService_Ask_SuccessWithOnlySummary(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(10)).
		Return("", "только выжимка", nil)

	llm.EXPECT().
		Ask(gomock.Any(), gomock.Any(), "вопрос").
		DoAndReturn(func(_ context.Context, materials, _ string) (string, error) {
			assert.Contains(t, materials, "только выжимка")
			assert.NotContains(t, materials, "Транскрипция:")
			return "ответ", nil
		})

	chatRepo.EXPECT().Create(gomock.Any(), int64(1), int64(10), "вопрос", "ответ").Return(nil)

	_, err := svc.Ask(context.Background(), "alice", 10, "вопрос")

	require.NoError(t, err)
}

func TestChatService_Ask_NoMaterials(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(10)).
		Return("", "", nil)

	answer, err := svc.Ask(context.Background(), "alice", 10, "вопрос")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrNoMaterials)
	assert.Equal(t, "", answer)
}

func TestChatService_Ask_MeetingNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(999)).
		Return("", "", models.ErrNotFound)

	answer, err := svc.Ask(context.Background(), "alice", 999, "вопрос")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrNotFound)
	assert.Equal(t, "", answer)
}

func TestChatService_Ask_ResolveFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").
		Return(models.User{}, errors.New("db down"))

	answer, err := svc.Ask(context.Background(), "alice", 10, "вопрос")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve user")
	assert.Equal(t, "", answer)
}

func TestChatService_Ask_LLMError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(10)).
		Return("транскрипция", "выжимка", nil)
	llm.EXPECT().Ask(gomock.Any(), gomock.Any(), "в?").
		Return("", errors.New("api down"))

	answer, err := svc.Ask(context.Background(), "alice", 10, "в?")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm ask")
	assert.Equal(t, "", answer)
}

func TestChatService_Ask_ChatSaveFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	chatRepo := mocks.NewMockChatRepo(ctrl)
	llm := mocks.NewMockLLMClient(ctrl)

	svc := newChatService(t, userSvc, contentRepo, chatRepo, llm)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetContextOwned(gomock.Any(), int64(1), int64(10)).
		Return("транскрипция", "выжимка", nil)
	llm.EXPECT().Ask(gomock.Any(), gomock.Any(), "в?").Return("ответ", nil)
	chatRepo.EXPECT().Create(gomock.Any(), int64(1), int64(10), "в?", "ответ").
		Return(errors.New("write failed"))

	_, err := svc.Ask(context.Background(), "alice", 10, "в?")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "сохранение chat message")
}
