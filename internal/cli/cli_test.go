package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegsys/meeting-assistant/internal/cli"
	"github.com/olegsys/meeting-assistant/internal/models"
)

// runCLI выполняет CLI команду в контексте сервера и возвращает ошибку.
func runCLI(t *testing.T, serverURL string, args []string) error {
	t.Helper()
	runner := cli.NewRunner(serverURL, "")
	return runner.Run(context.Background(), args)
}

func TestCLI_NoCommand(t *testing.T) {
	err := runCLI(t, "http://localhost:0", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "команда не указана")
}

func TestCLI_UnknownCommand(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"foo"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "неизвестная команда")
}

func TestCLI_Start_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/start", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "alice", body["user_id"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.User{ID: 1, ExternalID: "alice"})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"start", "-user", "alice"})
	require.NoError(t, err)
}

func TestCLI_Start_MissingUser(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"start"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан user")
}

func TestCLI_Load_Success(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "meeting.wav")
	require.NoError(t, os.WriteFile(audioPath, []byte("audio-content"), 0o644))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings/load", r.URL.Path)
		assert.Equal(t, "alice", r.Header.Get("X-User-Id"))
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"meeting_id": int64(42),
			"status":     models.StatusCreated,
		})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"load", "-user", "alice", "-path", audioPath})
	require.NoError(t, err)
}

func TestCLI_Load_MissingPath(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"load", "-user", "alice"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан path")
}

func TestCLI_Load_MissingUser(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"load", "-path", "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан user")
}

func TestCLI_Load_FileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("сервер не должен вызываться при отсутствии файла")
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"load", "-user", "alice", "-path", "/no/such/file"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось прочитать файл")
}

func TestCLI_List_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"list", "-user", "alice"})
	require.NoError(t, err)
}

func TestCLI_List_WithItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"meeting_id": int64(1), "title": "first", "status": "completed", "summary": "ok"},
				{"meeting_id": int64(2), "title": "second", "status": "processing"},
			},
		})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"list", "-user", "alice"})
	require.NoError(t, err)
}

func TestCLI_Status_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings/10/status", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.StatusInfo{
			MeetingID: 10,
			Status:    models.StatusCompleted,
		})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"status", "-user", "alice", "-id", "10"})
	require.NoError(t, err)
}

func TestCLI_Status_MissingID(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"status", "-user", "alice"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан id")
}

func TestCLI_Get_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings/7/transcription", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": "текст транскрипции"})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"get", "-user", "alice", "-id", "7"})
	require.NoError(t, err)
}

func TestCLI_Find_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings/search", r.URL.Path)
		assert.Equal(t, "budget", r.URL.Query().Get("keyword"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"meeting_id": int64(5), "title": "budget", "status": "completed", "snippet": "..."},
			},
		})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"find", "-user", "alice", "-keyword", "budget"})
	require.NoError(t, err)
}

func TestCLI_Find_MissingKeyword(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"find", "-user", "alice"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан keyword")
}

func TestCLI_Chat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings/3/chat", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "что решили?", body["text"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": "решили так"})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"chat", "-user", "alice", "-id", "3", "-text", "что решили?"})
	require.NoError(t, err)
}

func TestCLI_Chat_MissingText(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"chat", "-user", "alice", "-id", "3"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан text")
}

func TestCLI_Retry_Success(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/meetings/9/retry", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"retry", "-user", "alice", "-id", "9"})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestCLI_Retry_MissingID(t *testing.T) {
	err := runCLI(t, "http://localhost:0", []string{"retry", "-user", "alice"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не указан id")
}

func TestCLI_DefaultUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bob", r.Header.Get("X-User-Id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	runner := cli.NewRunner(srv.URL, "bob")
	err := runner.Run(context.Background(), []string{"list"})
	require.NoError(t, err)
}

func TestCLI_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	err := runCLI(t, srv.URL, []string{"list", "-user", "alice"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
