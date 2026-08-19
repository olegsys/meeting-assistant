package config

import (
	"flag"
	"os"
	"strconv"
	"sync"
	"time"
)

// Config содержит параметры конфигурации приложения, заполняемые из переменных окружения и флагов.
type Config struct {
	RunAddress     string
	DatabaseURI    string
	SpeechProvider string
	LLMProvider    string
	LLMBaseURL     string
	LLMModel       string
	Workers        int
	TaskTimeout    time.Duration
	PollInterval   time.Duration
	MaxAttempts    int
	MaxUploadBytes int64
}

var (
	instance *Config
	once     sync.Once
)

// GetConfig возвращает синглтон Config, инициализированный из флагов и переменных окружения.
func GetConfig() *Config {
	once.Do(func() {
		cfg := &Config{}

		flag.StringVar(&cfg.RunAddress, "a", "localhost:8080", "service address")
		flag.StringVar(&cfg.DatabaseURI, "d", "", "database URI")
		flag.StringVar(&cfg.SpeechProvider, "speech", "mock", "speech provider")
		flag.StringVar(&cfg.LLMProvider, "llm", "mock", "llm provider")
		flag.StringVar(&cfg.LLMBaseURL, "llm-base-url", "http://localhost:1234/v1", "llm base url (lmstudio/openai-compatible)")
		flag.StringVar(&cfg.LLMModel, "llm-model", "", "llm model name")
		flag.IntVar(&cfg.Workers, "workers", 2, "worker pool size")
		flag.DurationVar(&cfg.TaskTimeout, "task-timeout", 30*time.Second, "task timeout")
		flag.DurationVar(&cfg.PollInterval, "poll-interval", 5*time.Second, "worker poll interval")
		flag.IntVar(&cfg.MaxAttempts, "max-attempts", 3, "max task attempts")
		flag.Int64Var(&cfg.MaxUploadBytes, "max-upload-bytes", 32<<20, "max upload bytes")
		flag.Parse()

		cfg.applyEnv()

		instance = cfg
	})

	return instance
}

func (c *Config) applyEnv() {
	if val := os.Getenv("RUN_ADDRESS"); val != "" {
		c.RunAddress = val
	}

	if val := os.Getenv("DATABASE_URI"); val != "" {
		c.DatabaseURI = val
	}

	if val := os.Getenv("SPEECH_PROVIDER"); val != "" {
		c.SpeechProvider = val
	}

	if val := os.Getenv("LLM_PROVIDER"); val != "" {
		c.LLMProvider = val
	}

	if val := os.Getenv("LLM_BASE_URL"); val != "" {
		c.LLMBaseURL = val
	}

	if val := os.Getenv("LLM_MODEL"); val != "" {
		c.LLMModel = val
	}

	if val := os.Getenv("WORKERS"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			c.Workers = parsed
		}
	}

	if val := os.Getenv("TASK_TIMEOUT"); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			c.TaskTimeout = parsed
		}
	}

	if val := os.Getenv("POLL_INTERVAL"); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			c.PollInterval = parsed
		}
	}

	if val := os.Getenv("MAX_ATTEMPTS"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			c.MaxAttempts = parsed
		}
	}

	if val := os.Getenv("MAX_UPLOAD_BYTES"); val != "" {
		if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
			c.MaxUploadBytes = parsed
		}
	}
}
