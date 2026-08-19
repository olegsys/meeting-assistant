package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/api"
	"github.com/olegsys/meeting-assistant/internal/config"
	"github.com/olegsys/meeting-assistant/internal/database"
	"github.com/olegsys/meeting-assistant/internal/llm"
	"github.com/olegsys/meeting-assistant/internal/repository"
	"github.com/olegsys/meeting-assistant/internal/service"
	"github.com/olegsys/meeting-assistant/internal/speech"
)

type diContainer struct {
	pool *pgxpool.Pool

	userRepo    repository.UserRepo
	meetingRepo repository.MeetingRepo
	fileRepo    repository.FileRepo
	taskRepo    repository.TaskRepo
	contentRepo repository.ContentRepo
	chatRepo    repository.ChatRepo
	txManager   database.TxManager

	speechClient speech.Client
	llmClient    llm.Client

	userService       service.UserService
	meetingService    service.MeetingService
	chatService       service.ChatService
	processingService service.ProcessingService

	handler api.Handler
}

func newDIContainer() *diContainer {
	return &diContainer{}
}

func (d *diContainer) Pool() *pgxpool.Pool {
	if d.pool == nil {
		pool, err := database.NewPool(context.Background(), config.GetConfig().DatabaseURI)
		if err != nil {
			panic(fmt.Errorf("не удалось подключиться к БД: %w", err))
		}

		d.pool = pool
	}

	return d.pool
}

func (d *diContainer) TxManager() database.TxManager {
	if d.txManager == nil {
		d.txManager = database.NewTxManager(d.Pool())
	}

	return d.txManager
}

func (d *diContainer) UserRepo() repository.UserRepo {
	if d.userRepo == nil {
		d.userRepo = repository.NewUserRepo(d.Pool())
	}

	return d.userRepo
}

func (d *diContainer) MeetingRepo() repository.MeetingRepo {
	if d.meetingRepo == nil {
		d.meetingRepo = repository.NewMeetingRepo(d.Pool())
	}

	return d.meetingRepo
}

func (d *diContainer) FileRepo() repository.FileRepo {
	if d.fileRepo == nil {
		d.fileRepo = repository.NewFileRepo(d.Pool())
	}

	return d.fileRepo
}

func (d *diContainer) TaskRepo() repository.TaskRepo {
	if d.taskRepo == nil {
		d.taskRepo = repository.NewTaskRepo(d.Pool())
	}

	return d.taskRepo
}

func (d *diContainer) ContentRepo() repository.ContentRepo {
	if d.contentRepo == nil {
		d.contentRepo = repository.NewContentRepo(d.Pool())
	}

	return d.contentRepo
}

func (d *diContainer) ChatRepo() repository.ChatRepo {
	if d.chatRepo == nil {
		d.chatRepo = repository.NewChatRepo(d.Pool())
	}

	return d.chatRepo
}

func (d *diContainer) SpeechClient() speech.Client {
	if d.speechClient == nil {
		client, err := speech.NewClient(config.GetConfig().SpeechProvider)
		if err != nil {
			panic(fmt.Errorf("не удалось создать speech client: %w", err))
		}

		d.speechClient = client
	}

	return d.speechClient
}

func (d *diContainer) LLMClient() llm.Client {
	if d.llmClient == nil {
		client, err := llm.NewClient(config.GetConfig().LLMProvider)
		if err != nil {
			panic(fmt.Errorf("не удалось создать llm client: %w", err))
		}

		d.llmClient = client
	}

	return d.llmClient
}

func (d *diContainer) UserService() service.UserService {
	if d.userService == nil {
		d.userService = service.NewUserService(d.UserRepo())
	}

	return d.userService
}

func (d *diContainer) MeetingService() service.MeetingService {
	if d.meetingService == nil {
		d.meetingService = service.NewMeetingService(
			d.UserService(),
			d.MeetingRepo(),
			d.FileRepo(),
			d.TaskRepo(),
			d.ContentRepo(),
			d.TxManager(),
		)
	}

	return d.meetingService
}

func (d *diContainer) ChatService() service.ChatService {
	if d.chatService == nil {
		d.chatService = service.NewChatService(
			d.UserService(),
			d.ContentRepo(),
			d.ChatRepo(),
			d.LLMClient(),
		)
	}

	return d.chatService
}

func (d *diContainer) ProcessingService() service.ProcessingService {
	if d.processingService == nil {
		cfg := config.GetConfig()

		d.processingService = service.NewProcessingService(
			d.TaskRepo(),
			d.FileRepo(),
			d.ContentRepo(),
			d.SpeechClient(),
			d.LLMClient(),
			d.TxManager(),
			cfg.Workers,
			cfg.TaskTimeout,
			cfg.PollInterval,
			cfg.MaxAttempts,
			cfg.MaxUploadBytes,
		)
	}

	return d.processingService
}

func (d *diContainer) Handler() api.Handler {
	if d.handler == nil {
		d.handler = api.NewHandler(
			d.UserService(),
			d.MeetingService(),
			d.ChatService(),
			config.GetConfig().MaxUploadBytes,
		)
	}

	return d.handler
}

func (d *diContainer) Close() {
	if d.pool != nil {
		slog.Info("закрытие пула подключений к БД")
		d.pool.Close()
	}
}
