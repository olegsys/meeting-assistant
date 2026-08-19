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

// passthroughTxManager выполняет callback в том же контексте, без реальной БД.
// Подходит для unit-тестов
type passthroughTxManager struct{}

func (passthroughTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func newMeetingService(
	t *testing.T,
	userSvc service.UserService,
	meetingRepo *mocks.MockMeetingRepo,
	fileRepo *mocks.MockFileRepo,
	taskRepo *mocks.MockTaskRepo,
	contentRepo *mocks.MockContentRepo,
) service.MeetingService {
	t.Helper()
	return service.NewMeetingService(
		userSvc,
		meetingRepo,
		fileRepo,
		taskRepo,
		contentRepo,
		passthroughTxManager{},
	)
}

func TestMeetingService_Load_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	user := models.User{ID: 1, ExternalID: "alice"}
	content := []byte("audio-bytes")

	userSvc.EXPECT().
		Resolve(gomock.Any(), "alice").
		Return(user, nil)

	meetingRepo.EXPECT().
		Create(gomock.Any(), user.ID, "meeting.wav").
		Return(models.Meeting{ID: 100, UserID: user.ID, Title: "meeting.wav"}, nil)

	fileRepo.EXPECT().
		Save(gomock.Any(), int64(100), "meeting.wav", content).
		Return(nil)

	taskRepo.EXPECT().
		Create(gomock.Any(), int64(100)).
		Return(int64(7), nil)

	id, err := svc.Load(context.Background(), "alice", "meeting.wav", content)

	require.NoError(t, err)
	assert.Equal(t, int64(100), id)
}

func TestMeetingService_Load_EmptyFileName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").
		Return(models.User{ID: 1, ExternalID: "alice"}, nil)

	id, err := svc.Load(context.Background(), "alice", "", []byte("data"))

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrInvalidInput)
	assert.Equal(t, int64(0), id)
}

func TestMeetingService_Load_EmptyContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").
		Return(models.User{ID: 1, ExternalID: "alice"}, nil)

	id, err := svc.Load(context.Background(), "alice", "meeting.wav", nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrEmptyFile)
	assert.Equal(t, int64(0), id)
}

func TestMeetingService_Load_ResolveFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	userSvc.EXPECT().
		Resolve(gomock.Any(), "alice").
		Return(models.User{}, errors.New("db down"))

	id, err := svc.Load(context.Background(), "alice", "meeting.wav", []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve user")
	assert.Equal(t, int64(0), id)
}

func TestMeetingService_Load_MeetingCreateFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	userSvc.EXPECT().
		Resolve(gomock.Any(), "alice").
		Return(models.User{ID: 1, ExternalID: "alice"}, nil)

	meetingRepo.EXPECT().
		Create(gomock.Any(), int64(1), "meeting.wav").
		Return(models.Meeting{}, errors.New("insert failed"))

	id, err := svc.Load(context.Background(), "alice", "meeting.wav", []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert failed")
	assert.Equal(t, int64(0), id)
}

func TestMeetingService_Load_FileSaveFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	meetingRepo.EXPECT().Create(gomock.Any(), int64(1), "meeting.wav").
		Return(models.Meeting{ID: 100, UserID: 1, Title: "meeting.wav"}, nil)
	fileRepo.EXPECT().Save(gomock.Any(), int64(100), "meeting.wav", gomock.Any()).
		Return(errors.New("write failed"))

	id, err := svc.Load(context.Background(), "alice", "meeting.wav", []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
	assert.Equal(t, int64(0), id)
}

func TestMeetingService_Load_TaskCreateFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	meetingRepo.EXPECT().Create(gomock.Any(), int64(1), "meeting.wav").
		Return(models.Meeting{ID: 100, UserID: 1, Title: "meeting.wav"}, nil)
	fileRepo.EXPECT().Save(gomock.Any(), int64(100), "meeting.wav", gomock.Any()).Return(nil)
	taskRepo.EXPECT().Create(gomock.Any(), int64(100)).Return(int64(0), errors.New("task insert"))

	id, err := svc.Load(context.Background(), "alice", "meeting.wav", []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "task insert")
	assert.Equal(t, int64(0), id)
}

func TestMeetingService_Load_StripsDirectoryFromTitle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	meetingRepo.EXPECT().Create(gomock.Any(), int64(1), "meeting.wav").
		Return(models.Meeting{ID: 5, UserID: 1, Title: "meeting.wav"}, nil)
	fileRepo.EXPECT().Save(gomock.Any(), int64(5), "/tmp/meeting.wav", gomock.Any()).Return(nil)
	taskRepo.EXPECT().Create(gomock.Any(), int64(5)).Return(int64(1), nil)

	_, err := svc.Load(context.Background(), "alice", "/tmp/meeting.wav", []byte("x"))

	require.NoError(t, err)
}

func TestMeetingService_List_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	fileRepo := mocks.NewMockFileRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)

	svc := newMeetingService(t, userSvc, meetingRepo, fileRepo, taskRepo, contentRepo)

	expected := []models.MeetingListItem{
		{MeetingID: 1, Title: "first", Status: models.StatusCompleted, Summary: "ok"},
		{MeetingID: 2, Title: "second", Status: models.StatusProcessing},
	}

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	meetingRepo.EXPECT().List(gomock.Any(), int64(1)).Return(expected, nil)

	items, err := svc.List(context.Background(), "alice")

	require.NoError(t, err)
	assert.Equal(t, expected, items)
}

func TestMeetingService_List_ResolveFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	svc := newMeetingService(t, userSvc, meetingRepo,
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{}, errors.New("boom"))

	items, err := svc.List(context.Background(), "alice")

	require.Error(t, err)
	assert.Nil(t, items)
	assert.Contains(t, err.Error(), "resolve user")
}

func TestMeetingService_Status_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newMeetingService(t, userSvc, meetingRepo,
		mocks.NewMockFileRepo(ctrl),
		taskRepo,
		mocks.NewMockContentRepo(ctrl),
	)

	info := models.StatusInfo{
		MeetingID: 10,
		Status:    models.StatusCompleted,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	taskRepo.EXPECT().GetStatusOwned(gomock.Any(), int64(1), int64(10)).Return(info, nil)

	got, err := svc.Status(context.Background(), "alice", 10)

	require.NoError(t, err)
	assert.Equal(t, info, got)
}

func TestMeetingService_Status_NotFoundBubblesUp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		taskRepo,
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	taskRepo.EXPECT().GetStatusOwned(gomock.Any(), int64(1), int64(999)).
		Return(models.StatusInfo{}, models.ErrNotFound)

	info, err := svc.Status(context.Background(), "alice", 999)

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrNotFound)
	assert.Equal(t, models.StatusInfo{}, info)
}

func TestMeetingService_Status_OtherUserMeetingReturnsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		taskRepo,
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	// репозиторий сам обеспечивает фильтрацию по user_id: чужой meeting даст ErrNotFound.
	taskRepo.EXPECT().GetStatusOwned(gomock.Any(), int64(1), int64(50)).
		Return(models.StatusInfo{}, models.ErrNotFound)

	_, err := svc.Status(context.Background(), "alice", 50)

	assert.ErrorIs(t, err, models.ErrNotFound)
}

func TestMeetingService_GetTranscription_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		contentRepo,
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetTranscriptionOwned(gomock.Any(), int64(1), int64(10)).
		Return("текст транскрипции", nil)

	text, err := svc.GetTranscription(context.Background(), "alice", 10)

	require.NoError(t, err)
	assert.Equal(t, "текст транскрипции", text)
}

func TestMeetingService_GetTranscription_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	contentRepo := mocks.NewMockContentRepo(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		contentRepo,
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	contentRepo.EXPECT().GetTranscriptionOwned(gomock.Any(), int64(1), int64(10)).
		Return("", models.ErrNotFound)

	text, err := svc.GetTranscription(context.Background(), "alice", 10)

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrNotFound)
	assert.Equal(t, "", text)
}

func TestMeetingService_Find_EmptyKeyword(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := newMeetingService(t,
		mocks.NewMockUserService(ctrl),
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
	)

	results, err := svc.Find(context.Background(), "alice", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrInvalidInput)
	assert.Nil(t, results)
}

func TestMeetingService_Find_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	svc := newMeetingService(t, userSvc, meetingRepo,
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
	)

	expected := []models.FindResult{
		{MeetingID: 1, Title: "budget meeting", Status: models.StatusCompleted, Snippet: "budget approved"},
	}

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	meetingRepo.EXPECT().Find(gomock.Any(), int64(1), "budget").Return(expected, nil)

	got, err := svc.Find(context.Background(), "alice", "budget")

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestMeetingService_Find_OnlyForCurrentUser(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	meetingRepo := mocks.NewMockMeetingRepo(ctrl)
	svc := newMeetingService(t, userSvc, meetingRepo,
		mocks.NewMockFileRepo(ctrl),
		mocks.NewMockTaskRepo(ctrl),
		mocks.NewMockContentRepo(ctrl),
	)

	// репозиторий обязан получать именно user_id=1 — поиск изолирован по пользователю.
	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	meetingRepo.EXPECT().Find(gomock.Any(), int64(1), "secret").
		Return([]models.FindResult{}, nil)

	_, err := svc.Find(context.Background(), "alice", "secret")

	require.NoError(t, err)
}

func TestMeetingService_Retry_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		taskRepo,
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	taskRepo.EXPECT().RetryFailedOwned(gomock.Any(), int64(1), int64(10)).Return(nil)

	err := svc.Retry(context.Background(), "alice", 10)

	require.NoError(t, err)
}

func TestMeetingService_Retry_TaskNotFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	userSvc := mocks.NewMockUserService(ctrl)
	taskRepo := mocks.NewMockTaskRepo(ctrl)
	svc := newMeetingService(t, userSvc,
		mocks.NewMockMeetingRepo(ctrl),
		mocks.NewMockFileRepo(ctrl),
		taskRepo,
		mocks.NewMockContentRepo(ctrl),
	)

	userSvc.EXPECT().Resolve(gomock.Any(), "alice").Return(models.User{ID: 1}, nil)
	taskRepo.EXPECT().RetryFailedOwned(gomock.Any(), int64(1), int64(10)).
		Return(models.ErrTaskNotFailed)

	err := svc.Retry(context.Background(), "alice", 10)

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrTaskNotFailed)
}
