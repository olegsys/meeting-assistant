// Package testdb содержит хелперы для интеграционных тестов с PostgreSQL,
// поднимаемым через testcontainers.
//
// Этот пакет компилируется только при build-теге `integration`.
//go:build integration

package testdb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/olegsys/meeting-assistant/internal/database"
)

const (
	postgresImage    = "postgres:16-alpine"
	postgresUser     = "test"
	postgresPassword = "test"
	postgresDB       = "test"
	postgresPort     = "5432/tcp"

	startupTimeout = 90 * time.Second
	connectTimeout = 30 * time.Second
)

var (
	globalPool *pgxpool.Pool
	globalMu   sync.Mutex
	refCount   int
)

// Pool возвращает общий пул для всех интеграционных тестов в одном прогоне.
// Контейнер поднимается один раз; тесты должны вызывать Truncate между сценариями.
//
// Если Docker недоступен, тест будет пропущен через t.Skip.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	globalMu.Lock()
	defer globalMu.Unlock()

	if globalPool != nil {
		refCount++
		t.Cleanup(func() {
			globalMu.Lock()
			defer globalMu.Unlock()
			refCount--
		})

		return globalPool
	}

	if !dockerAvailable() {
		t.Skip("Docker недоступен: интеграционные тесты с PostgreSQL пропущены")
	}

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	pgC, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage(postgresImage),
		tcpostgres.WithUsername(postgresUser),
		tcpostgres.WithPassword(postgresPassword),
		tcpostgres.WithDatabase(postgresDB),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(startupTimeout),
		),
	)
	if err != nil {
		t.Fatalf("не удалось поднять контейнер postgres: %v", err)
	}

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = pgC.Terminate(context.Background())
		t.Fatalf("не удалось получить DSN контейнера: %v", err)
	}

	if err := database.RunMigrations(dsn); err != nil {
		_ = pgC.Terminate(context.Background())
		t.Fatalf("не удалось применить миграции: %v", err)
	}

	poolCtx, poolCancel := context.WithTimeout(context.Background(), connectTimeout)
	defer poolCancel()

	pool, err := database.NewPool(poolCtx, dsn)
	if err != nil {
		_ = pgC.Terminate(context.Background())
		t.Fatalf("не удалось создать пул: %v", err)
	}

	globalPool = pool
	refCount = 1

	t.Cleanup(func() {
		globalMu.Lock()
		defer globalMu.Unlock()
		refCount--

		if refCount > 0 {
			return
		}

		pool.Close()
		if err := pgC.Terminate(context.Background()); err != nil {
			t.Logf("не удалось остановить контейнер postgres: %v", err)
		}

		globalPool = nil
	})

	return pool
}

// Truncate очищает все таблицы в обратном порядке FK и сбрасывает BIGSERIAL.
func Truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const query = `
		TRUNCATE
			chat_messages,
			summaries,
			transcriptions,
			processing_tasks,
			meeting_files,
			meetings,
			users
		RESTART IDENTITY CASCADE
	`

	if _, err := pool.Exec(ctx, query); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// dockerAvailable быстро проверяет, доступен ли Docker daemon, чтобы тесты
// можно было пропустить (t.Skip) в окружениях без Docker вместо долгого таймаута.
func dockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}

	if err := provider.Health(ctx); err != nil {
		return false
	}

	return true
}

// errInvalidArgument хелпер для читаемой обработки ошибок pgx.
var errInvalidArgument = errors.New("invalid argument")

// MustExec выполняет запрос и падает в тесте при ошибке.
func MustExec(t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// MustInsertUser создаёт пользователя и возвращает его id; используется в фикстурах тестов.
func MustInsertUser(t *testing.T, pool *pgxpool.Pool, externalID string) int64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO users (external_id) VALUES ($1) RETURNING id`,
		externalID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert user %q: %v", externalID, err)
	}

	return id
}

// MustInsertMeeting создаёт встречу и возвращает её id.
func MustInsertMeeting(t *testing.T, pool *pgxpool.Pool, userID int64, title string) int64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO meetings (user_id, title) VALUES ($1, $2) RETURNING id`,
		userID, title,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert meeting: %v", err)
	}

	return id
}

// StringErr возвращает ошибку как строку для логирования.
func StringErr(err error) string {
	if err == nil {
		return "<nil>"
	}

	return fmt.Sprintf("%v", err)
}
