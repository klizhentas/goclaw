package model

import (
	"context"

	"github.com/gravitational/trace"
	"github.com/klizhentas/goclaw/internal/config"
	"github.com/klizhentas/goclaw/internal/prompt"
)

type StreamFunc func(token string) error

type Client interface {
	StreamResponse(ctx context.Context, messages []prompt.PromptMessage, onToken StreamFunc) (string, error)
}

type DiagnosticClient interface {
	StartupCheck(ctx context.Context) (map[string]any, error)
}

func NewClient(cfg config.Config) (Client, error) {
	switch cfg.ModelBackend {
	case "echo":
		return &EchoClient{}, nil
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, trace.BadParameter("OPENAI_API_KEY is required when MODEL_BACKEND=openai")
		}
		return NewOpenAIClient(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL, cfg.AllowedTools), nil
	default:
		return nil, trace.BadParameter("unsupported MODEL_BACKEND: %s", cfg.ModelBackend)
	}
}

// EchoClient is a local bootstrap model implementation.
type EchoClient struct{}

func (c *EchoClient) StreamResponse(_ context.Context, messages []prompt.PromptMessage, onToken StreamFunc) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	last := messages[len(messages)-1].Content
	if err := onToken(last); err != nil {
		return "", err
	}
	return last, nil
}
