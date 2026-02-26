package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gravitational/trace"
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
	ToolDescriptions       map[string]string
	UIUserLabel            string
	UIAssistantLabel       string
	TermTheme              string
}

type policyTool struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

type policyFile struct {
	Allow struct {
		Tool []policyTool `toml:"tool"`
	} `toml:"allow"`
	UI struct {
		UserLabel      string `toml:"user_label"`
		AssistantLabel string `toml:"assistant_label"`
	} `toml:"ui"`
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
		LogLevel:               parseLogLevel(envOrDefault("LOG_LEVEL", "WARN")),
		LogPath:                envOrDefault("LOG_PATH", "./data/goclaw.log"),
		Mode:                   envOrDefault("MODE", "single"),
		WorkerID:               envOrDefault("WORKER_ID", ""),
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
		UIUserLabel:            envOrDefault("UI_USER_LABEL", "you"),
		UIAssistantLabel:       envOrDefault("UI_ASSISTANT_LABEL", "goclaw"),
		TermTheme:              strings.ToLower(envOrDefault("TERM_THEME", "light")),
	}

	policyCfg, err := loadPolicyFile(cfg.PolicyPath)
	if err != nil {
		return Config{}, err
	}
	cfg.AllowedTools = policyCfg.tools
	cfg.ToolDescriptions = policyCfg.descriptions
	if policyCfg.uiUserLabel != "" {
		cfg.UIUserLabel = policyCfg.uiUserLabel
	}
	if policyCfg.uiAssistantLabel != "" {
		cfg.UIAssistantLabel = policyCfg.uiAssistantLabel
	}

	if cfg.MaxActiveConversations < 1 {
		return Config{}, trace.BadParameter("MAX_ACTIVE_CONVERSATIONS must be >= 1")
	}
	if cfg.HistoryWindow < 1 {
		return Config{}, trace.BadParameter("HISTORY_WINDOW must be >= 1")
	}
	if cfg.AssistantName == "" {
		return Config{}, trace.BadParameter("ASSISTANT_NAME must be non-empty")
	}
	if cfg.QueuePollInterval <= 0 {
		return Config{}, trace.BadParameter("QUEUE_POLL_INTERVAL_MS must be > 0")
	}
	if cfg.TaskLeaseDuration <= 0 {
		return Config{}, trace.BadParameter("TASK_LEASE_SECONDS must be > 0")
	}
	if cfg.SchedulerConcurrency < 1 {
		return Config{}, trace.BadParameter("SCHEDULER_CONCURRENCY must be >= 1")
	}
	if strings.TrimSpace(cfg.UIUserLabel) == "" {
		return Config{}, trace.BadParameter("UI user label must be non-empty")
	}
	if strings.TrimSpace(cfg.UIAssistantLabel) == "" {
		return Config{}, trace.BadParameter("UI assistant label must be non-empty")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.TermTheme)) {
	case "", "light", "dark":
		if strings.TrimSpace(cfg.TermTheme) == "" {
			cfg.TermTheme = "light"
		}
	default:
		return Config{}, trace.BadParameter("TERM_THEME must be one of: light,dark")
	}

	return cfg, nil
}

type policyConfig struct {
	tools            []string
	descriptions     map[string]string
	uiUserLabel      string
	uiAssistantLabel string
}

func loadPolicyFile(path string) (policyConfig, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return policyConfig{}, nil
		}
		return policyConfig{}, trace.Wrap(err)
	}

	var policy policyFile
	if _, err := toml.DecodeFile(path, &policy); err != nil {
		return policyConfig{}, trace.Wrap(err)
	}

	seen := make(map[string]struct{})
	tools := make([]string, 0, len(policy.Allow.Tool))
	descriptions := make(map[string]string, len(policy.Allow.Tool))

	for _, tool := range policy.Allow.Tool {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			tools = append(tools, name)
		}
		if desc := strings.TrimSpace(tool.Description); desc != "" {
			descriptions[name] = desc
		}
	}

	return policyConfig{
		tools:            tools,
		descriptions:     descriptions,
		uiUserLabel:      strings.TrimSpace(policy.UI.UserLabel),
		uiAssistantLabel: strings.TrimSpace(policy.UI.AssistantLabel),
	}, nil
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
