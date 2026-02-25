package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DatabasePath           string
	AssistantName          string
	MainConversationID     string
	NonMainNeedsTrigger    bool
	MainNeedsTrigger       bool
	MaxActiveConversations int
	HistoryWindow          int
	RequestTimeout         time.Duration
	LogLevel               slog.Level
	LogPath                string
	Mode                   string
	WorkerID               string
	SenderID               string
	QueuePollInterval      time.Duration
	TaskLeaseDuration      time.Duration
	SchedulerID            string
	SchedulerConcurrency   int
	ModelBackend           string
	OpenAIAPIKey           string
	OpenAIModel            string
	OpenAIBaseURL          string
	PolicyPath             string
	AllowedTools           []string
}

type policyFile struct {
	Allow struct {
		Tools []string `toml:"tools"`
	} `toml:"allow"`
}

func Load() (Config, error) {
	cfg := Config{
		DatabasePath:           envOrDefault("DATABASE_PATH", "./data/goclaw.db"),
		AssistantName:          envOrDefault("ASSISTANT_NAME", "Andy"),
		MainConversationID:     envOrDefault("MAIN_CONVERSATION_ID", "main"),
		NonMainNeedsTrigger:    envBoolOrDefault("NON_MAIN_NEEDS_TRIGGER", true),
		MainNeedsTrigger:       envBoolOrDefault("MAIN_NEEDS_TRIGGER", false),
		MaxActiveConversations: envIntOrDefault("MAX_ACTIVE_CONVERSATIONS", 3),
		HistoryWindow:          envIntOrDefault("HISTORY_WINDOW", 20),
		RequestTimeout:         time.Duration(envIntOrDefault("REQUEST_TIMEOUT_SECONDS", 30)) * time.Second,
		LogLevel:               parseLogLevel(envOrDefault("LOG_LEVEL", "INFO")),
		LogPath:                envOrDefault("LOG_PATH", "./data/goclaw.log"),
		Mode:                   envOrDefault("MODE", "single"),
		WorkerID:               envOrDefault("WORKER_ID", "worker-1"),
		SenderID:               envOrDefault("SENDER_ID", "sender-1"),
		QueuePollInterval:      time.Duration(envIntOrDefault("QUEUE_POLL_INTERVAL_MS", 500)) * time.Millisecond,
		TaskLeaseDuration:      time.Duration(envIntOrDefault("TASK_LEASE_SECONDS", 60)) * time.Second,
		SchedulerID:            envOrDefault("SCHEDULER_ID", "scheduler-1"),
		SchedulerConcurrency:   envIntOrDefault("SCHEDULER_CONCURRENCY", 2),
		ModelBackend:           strings.ToLower(envOrDefault("MODEL_BACKEND", "echo")),
		OpenAIAPIKey:           strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		OpenAIModel:            envOrDefault("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBaseURL:          envOrDefault("OPENAI_BASE_URL", "https://api.openai.com"),
		PolicyPath:             envOrDefault("POLICY_PATH", "./data/goclaw.toml"),
	}

	allowedTools, err := loadAllowedTools(cfg.PolicyPath)
	if err != nil {
		return Config{}, err
	}
	cfg.AllowedTools = allowedTools

	if cfg.MaxActiveConversations < 1 {
		return Config{}, fmt.Errorf("MAX_ACTIVE_CONVERSATIONS must be >= 1")
	}
	if cfg.HistoryWindow < 1 {
		return Config{}, fmt.Errorf("HISTORY_WINDOW must be >= 1")
	}
	if cfg.AssistantName == "" {
		return Config{}, fmt.Errorf("ASSISTANT_NAME must be non-empty")
	}
	if cfg.QueuePollInterval <= 0 {
		return Config{}, fmt.Errorf("QUEUE_POLL_INTERVAL_MS must be > 0")
	}
	if cfg.TaskLeaseDuration <= 0 {
		return Config{}, fmt.Errorf("TASK_LEASE_SECONDS must be > 0")
	}
	if cfg.SchedulerConcurrency < 1 {
		return Config{}, fmt.Errorf("SCHEDULER_CONCURRENCY must be >= 1")
	}

	return cfg, nil
}

func loadAllowedTools(path string) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat policy file %q: %w", path, err)
	}

	var policy policyFile
	if _, err := toml.DecodeFile(path, &policy); err != nil {
		return nil, fmt.Errorf("decode policy file %q: %w", path, err)
	}

	tools := make([]string, 0, len(policy.Allow.Tools))
	for _, tool := range policy.Allow.Tools {
		name := strings.TrimSpace(tool)
		if name == "" {
			continue
		}
		tools = append(tools, name)
	}
	return tools, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envIntOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBoolOrDefault(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
