package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gravitational/trace"
	"github.com/klizhentas/goclaw/internal/prompt"
	"github.com/klizhentas/goclaw/internal/tools"
)

type OpenAIClient struct {
	apiKey   string
	model    string
	baseURL  string
	http     *http.Client
	executor *tools.Executor
}

func NewOpenAIClient(apiKey, model, baseURL string, allowedTools []string) *OpenAIClient {
	return &OpenAIClient{
		apiKey:   apiKey,
		model:    model,
		baseURL:  strings.TrimRight(baseURL, "/"),
		http:     &http.Client{},
		executor: tools.NewExecutor(allowedTools),
	}
}

type openAIRequest struct {
	Model    string                 `json:"model"`
	Messages []openAIRequestMessage `json:"messages"`
	Tools    []openAITool           `json:"tools,omitempty"`
}

type openAIRequestMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string               `json:"type"`
	Function openAIToolDefinition `json:"function"`
}

type openAIToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openAIModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

type execLocalToolArgs struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
}

func (c *OpenAIClient) StreamResponse(ctx context.Context, messages []prompt.PromptMessage, onToken StreamFunc) (string, error) {
	conversation := make([]openAIRequestMessage, 0, len(messages)+8)
	for _, msg := range messages {
		conversation = append(conversation, openAIRequestMessage{Role: msg.Role, Content: msg.Content})
	}
	if c.executor.HasAllowedTools() {
		conversation = append([]openAIRequestMessage{{
			Role:    "system",
			Content: toolUsageSystemPrompt(c.executor.AllowedTools()),
		}}, conversation...)
	}

	for step := 0; step < 5; step++ {
		req := openAIRequest{Model: c.model, Messages: conversation}
		if c.executor.HasAllowedTools() {
			req.Tools = []openAITool{execToolDefinition()}
		}
		slog.Debug("openai model request", "stage", "model_stream", "tooling_enabled", c.executor.HasAllowedTools(), "allowed_tools_count", c.executor.AllowedCount(), "step", step)

		parsed, _, err := c.chatCompletion(ctx, req)
		if err != nil {
			return "", err
		}
		if len(parsed.Choices) == 0 {
			return "", trace.BadParameter("openai returned no choices")
		}

		message := parsed.Choices[0].Message
		if len(message.ToolCalls) == 0 {
			if c.executor.HasAllowedTools() {
				slog.Warn("model returned no tool call", "stage", "model_stream", "step", step, "assistant_content_preview", preview(message.Content))
			}
			if err := onToken(message.Content); err != nil {
				return "", err
			}
			return message.Content, nil
		}
		slog.Info("model returned tool calls", "stage", "model_stream", "step", step, "tool_calls_count", len(message.ToolCalls))

		conversation = append(conversation, openAIRequestMessage{
			Role:      "assistant",
			Content:   message.Content,
			ToolCalls: message.ToolCalls,
		})

		for _, toolCall := range message.ToolCalls {
			toolResult := c.handleToolCall(ctx, toolCall)
			conversation = append(conversation, openAIRequestMessage{
				Role:       "tool",
				ToolCallID: toolCall.ID,
				Content:    toolResult,
			})
		}
	}

	return "", trace.LimitExceeded("max tool-call steps reached")
}

func (c *OpenAIClient) handleToolCall(ctx context.Context, toolCall openAIToolCall) string {
	resultPayload := map[string]any{}

	if toolCall.Type != "function" || toolCall.Function.Name != "exec_local_tool" {
		slog.Warn("model requested unsupported tool", "stage", "model_stream", "tool_type", toolCall.Type, "tool_name", toolCall.Function.Name)
		resultPayload["error"] = "unsupported tool call"
		encoded, _ := json.Marshal(resultPayload)
		return string(encoded)
	}

	var args execLocalToolArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		slog.Warn("invalid tool arguments", "stage", "model_stream", "tool_name", toolCall.Function.Name, "error", err)
		resultPayload["error"] = fmt.Sprintf("invalid tool args: %v", err)
		encoded, _ := json.Marshal(resultPayload)
		return string(encoded)
	}
	normalized, normErr := normalizeExecArgs(toolCall.Function.Arguments, args)
	if normErr != nil {
		slog.Warn("tool arguments normalization failed", "stage", "model_stream", "raw_arguments", toolCall.Function.Arguments, "error", normErr)
		resultPayload["error"] = fmt.Sprintf("invalid tool args: %v", normErr)
		resultPayload["return_code"] = -1
		encoded, _ := json.Marshal(resultPayload)
		return string(encoded)
	}
	args = normalized
	slog.Info("executing tool call", "stage", "model_stream", "requested_tool", args.Tool, "args_count", len(args.Args))

	execResult, err := c.executor.Execute(ctx, args.Tool, args.Args)
	if err != nil {
		slog.Warn("tool execution denied/failed", "stage", "model_stream", "requested_tool", args.Tool, "allowed_tools", c.executor.AllowedTools(), "error", err)
		resultPayload["error"] = err.Error()
		resultPayload["return_code"] = -1
		encoded, _ := json.Marshal(resultPayload)
		return string(encoded)
	}
	slog.Info("tool execution completed", "stage", "model_stream", "requested_tool", args.Tool, "return_code", execResult.ReturnCode)

	resultPayload["stdout"] = execResult.Stdout
	resultPayload["return_code"] = execResult.ReturnCode
	encoded, _ := json.Marshal(resultPayload)
	return string(encoded)
}

func (c *OpenAIClient) chatCompletion(ctx context.Context, reqPayload openAIRequest) (openAIResponse, string, error) {
	payload, err := json.Marshal(reqPayload)
	if err != nil {
		return openAIResponse{}, "", trace.Wrap(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return openAIResponse{}, "", trace.Wrap(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return openAIResponse{}, "", trace.Wrap(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return openAIResponse{}, "", trace.Wrap(err)
	}

	var parsed openAIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return openAIResponse{}, resp.Status, trace.Wrap(err)
	}
	if resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return openAIResponse{}, resp.Status, trace.AccessDenied(parsed.Error.Message)
		}
		return openAIResponse{}, resp.Status, trace.AccessDenied("openai http status: %s", resp.Status)
	}
	return parsed, resp.Status, nil
}

func execToolDefinition() openAITool {
	return openAITool{
		Type: "function",
		Function: openAIToolDefinition{
			Name:        "exec_local_tool",
			Description: "Execute one local allowlisted command and return stdout and return code. Use this for command-like user requests.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool": map[string]any{"type": "string", "description": "Allowlisted tool executable name."},
					"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"tool", "args"},
			},
		},
	}
}

func (c *OpenAIClient) StartupCheck(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, trace.Wrap(err)
	}

	info := map[string]any{
		"backend":               "openai",
		"base_url":              c.baseURL,
		"configured_model":      c.model,
		"api_key_hint":          keyHint(c.apiKey),
		"allowed_tools_count":   c.executor.AllowedCount(),
		"allowed_tools_enabled": c.executor.HasAllowedTools(),
	}

	var parsed openAIModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		info["http_status"] = resp.Status
		return info, trace.Wrap(err)
	}

	if resp.StatusCode >= 300 {
		info["http_status"] = resp.Status
		if parsed.Error != nil {
			info["error_type"] = parsed.Error.Type
			info["error_code"] = parsed.Error.Code
			return info, trace.AccessDenied(parsed.Error.Message)
		}
		return info, trace.AccessDenied("openai diagnostics http status: %s", resp.Status)
	}

	info["http_status"] = resp.Status
	info["models_count"] = len(parsed.Data)
	modelFound := false
	for _, model := range parsed.Data {
		if model.ID == c.model {
			modelFound = true
			break
		}
	}
	info["configured_model_found"] = modelFound
	return info, nil
}

func keyHint(raw string) string {
	if raw == "" {
		return "missing"
	}
	if len(raw) <= 8 {
		return "present(len<=8)"
	}
	return raw[:6] + "..." + raw[len(raw)-4:]
}

func preview(text string) string {
	const max = 160
	if len(text) <= max {
		return text
	}
	return text[:max]
}

func normalizeExecArgs(raw string, parsed execLocalToolArgs) (execLocalToolArgs, error) {
	if strings.TrimSpace(parsed.Tool) != "" {
		return parsed, nil
	}

	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return execLocalToolArgs{}, trace.Wrap(err)
	}

	tool := firstNonEmptyString(
		valueAsString(generic["tool"]),
		valueAsString(generic["command"]),
		valueAsString(generic["cmd"]),
		valueAsString(generic["name"]),
	)
	args := parsed.Args
	if len(args) == 0 {
		switch v := generic["args"].(type) {
		case []any:
			for _, item := range v {
				s := valueAsString(item)
				if s != "" {
					args = append(args, s)
				}
			}
		case string:
			args = splitArgsFallback(v)
		}
	}

	// Support single command strings like {"command":"curl https://..."}.
	if tool != "" && len(args) == 0 && strings.Contains(tool, " ") {
		parts := splitArgsFallback(tool)
		if len(parts) > 0 {
			tool = parts[0]
			if len(parts) > 1 {
				args = parts[1:]
			}
		}
	}

	if tool == "" && len(args) > 0 {
		tool = args[0]
		args = args[1:]
	}

	if strings.TrimSpace(tool) == "" {
		return execLocalToolArgs{}, trace.BadParameter("missing tool name in arguments: %s", raw)
	}

	return execLocalToolArgs{Tool: strings.TrimSpace(tool), Args: args}, nil
}

func valueAsString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func splitArgsFallback(raw string) []string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func toolUsageSystemPrompt(allowedTools []string) string {
	return fmt.Sprintf(
		"You can execute local commands with the tool `exec_local_tool`. Allowed tools: %s. "+
			"When the user asks for a local command or explicitly asks to use a tool, call `exec_local_tool` instead of refusing. "+
			"If execution fails, explain the failure and include key stdout details.",
		strings.Join(allowedTools, ", "),
	)
}
