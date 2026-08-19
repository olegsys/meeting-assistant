package speech_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/speech"
)

func TestMockClient_Transcribe_RegularFile(t *testing.T) {
	c := speech.NewMockClient()

	text, err := c.Transcribe(context.Background(), "meeting.wav", []byte("audio"))

	require.NoError(t, err)
	assert.Contains(t, text, "meeting.wav")
	assert.Contains(t, text, "Тестовая транскрипция")
}

func TestMockClient_Transcribe_TxtFileReturnsContent(t *testing.T) {
	c := speech.NewMockClient()

	text, err := c.Transcribe(context.Background(), "notes.txt", []byte("просто текст из файла"))

	require.NoError(t, err)
	assert.Equal(t, "просто текст из файла", text)
}

func TestMockClient_Transcribe_FailingFile(t *testing.T) {
	c := speech.NewMockClient()

	_, err := c.Transcribe(context.Background(), "fail_me.wav", []byte("audio"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "имитация ошибки speech client")
}

func TestMockClient_Transcribe_EmptyContent(t *testing.T) {
	c := speech.NewMockClient()

	_, err := c.Transcribe(context.Background(), "meeting.wav", nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrEmptyFile)
}

func TestMockClient_Transcribe_ContextCanceled(t *testing.T) {
	c := speech.NewMockClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Transcribe(ctx, "meeting.wav", []byte("audio"))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewClient_Mock(t *testing.T) {
	c, err := speech.NewClient("mock")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_DefaultIsMock(t *testing.T) {
	c, err := speech.NewClient("")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_Unsupported(t *testing.T) {
	_, err := speech.NewClient("nope")
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrUnsupported)
}
