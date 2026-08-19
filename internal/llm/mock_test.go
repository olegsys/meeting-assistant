package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegsys/meeting-assistant/internal/llm"
	"github.com/olegsys/meeting-assistant/internal/models"
)

func TestMockClient_Summarize_Success(t *testing.T) {
	c := llm.NewMockClient()

	out, err := c.Summarize(context.Background(), "длинная транскрипция встречи про бюджет")

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "Выжимка:"))
	assert.Contains(t, out, "транскрипция встречи")
}

func TestMockClient_Summarize_EmptyTranscript(t *testing.T) {
	c := llm.NewMockClient()

	_, err := c.Summarize(context.Background(), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrEmptyTranscript)
}

func TestMockClient_Summarize_ContextCanceled(t *testing.T) {
	c := llm.NewMockClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Summarize(ctx, "транскрипция")

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMockClient_Summarize_TruncatesLongTranscript(t *testing.T) {
	c := llm.NewMockClient()

	long := strings.Repeat("a", 500)

	out, err := c.Summarize(context.Background(), long)

	require.NoError(t, err)
	assert.Contains(t, out, "...")
	assert.LessOrEqual(t, len([]rune(out)), 220)
}

func TestMockClient_Ask_Success(t *testing.T) {
	c := llm.NewMockClient()

	out, err := c.Ask(context.Background(), "материалы встречи", "что решили?")

	require.NoError(t, err)
	assert.Contains(t, out, "что решили?")
	assert.Contains(t, out, "Ответ")
}

func TestMockClient_Ask_EmptyQuestion(t *testing.T) {
	c := llm.NewMockClient()

	_, err := c.Ask(context.Background(), "материалы", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrInvalidInput)
}

func TestMockClient_Ask_EmptyMaterials(t *testing.T) {
	c := llm.NewMockClient()

	_, err := c.Ask(context.Background(), "", "вопрос")

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrNoMaterials)
}

func TestMockClient_Ask_ContextCanceled(t *testing.T) {
	c := llm.NewMockClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Ask(ctx, "материалы", "вопрос")

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewClient_Mock(t *testing.T) {
	c, err := llm.NewClient("mock")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_DefaultIsMock(t *testing.T) {
	c, err := llm.NewClient("")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_Unsupported(t *testing.T) {
	_, err := llm.NewClient("gigachat-not-configured")
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrUnsupported)
}
