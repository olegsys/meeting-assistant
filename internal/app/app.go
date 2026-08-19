package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/olegsys/meeting-assistant/internal/config"
	"github.com/olegsys/meeting-assistant/internal/database"
	"golang.org/x/sync/errgroup"
)

// App собирает DI контейнер, применяет миграции и запускает HTTP сервер с воркер пулом.
type App struct {
	diContainer *diContainer
}

// New создаёт новый экземпляр приложения.
func New() *App {
	return &App{
		diContainer: newDIContainer(),
	}
}

func (a *App) initMigrations() error {
	dsn := config.GetConfig().DatabaseURI
	if dsn == "" {
		return errors.New("DATABASE_URI не задан")
	}

	if err := database.RunMigrations(dsn); err != nil {
		return fmt.Errorf("ошибка миграций: %w", err)
	}

	return nil
}

// Run применяет миграции, запускает HTTP сервер и воркер пул и ожидает их корректного завершения.
func (a *App) Run() error {
	if err := a.initMigrations(); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.GetConfig()

	server := &http.Server{
		Addr:         cfg.RunAddress,
		Handler:      a.diContainer.Handler().Routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return a.diContainer.ProcessingService().Run(gctx)
	})

	g.Go(func() error {
		return runHTTPServer(gctx, server)
	})

	slog.Info("сервис запущен", slog.String("addr", cfg.RunAddress))

	err := g.Wait()

	a.diContainer.Close()

	if errors.Is(err, context.Canceled) {
		slog.Info("сервис остановлен корректно")
		return nil
	}

	return err
}

// runHTTPServer запускает HTTP сервер и корректно останавливает его при отмене контекста.
func runHTTPServer(ctx context.Context, server *http.Server) error {
	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("ошибка остановки HTTP сервера", slog.String("error", err.Error()))
		}
	}()

	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("ошибка HTTP сервера: %w", err)
	}

	return nil
}
