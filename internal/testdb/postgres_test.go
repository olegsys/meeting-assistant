// Smoke-тест инфраструктуры testdb: поднимает контейнер, проверяет пул,
// миграции и наличие всех таблиц из schema 001_initial_schema.up.sql.
//
//go:build integration

package testdb_test

import (
	"context"
	"testing"
	"time"

	"github.com/olegsys/meeting-assistant/internal/testdb"
)

func TestPostgresContainer_Smoke(t *testing.T) {
	pool := testdb.Pool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var one int
	if err := pool.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}

	if one != 1 {
		t.Fatalf("ожидалось 1, получено %d", one)
	}

	want := []string{
		"users",
		"meetings",
		"meeting_files",
		"processing_tasks",
		"transcriptions",
		"summaries",
		"chat_messages",
	}

	for _, table := range want {
		var exists bool

		err := pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`,
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("проверка таблицы %s: %v", table, err)
		}

		if !exists {
			t.Fatalf("таблица %q отсутствует после миграций", table)
		}
	}

	testdb.Truncate(t, pool)
}
