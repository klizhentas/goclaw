package tools

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"

	"github.com/gravitational/trace"
)

type Result struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ReturnCode int    `json:"return_code"`
}

type Executor struct {
	allowed map[string]struct{}
}

func NewExecutor(allowedTools []string) *Executor {
	allowed := make(map[string]struct{}, len(allowedTools))
	for _, tool := range allowedTools {
		name := strings.TrimSpace(tool)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return &Executor{allowed: allowed}
}

func (e *Executor) HasAllowedTools() bool {
	return len(e.allowed) > 0
}

func (e *Executor) AllowedCount() int {
	return len(e.allowed)
}

func (e *Executor) AllowedTools() []string {
	tools := make([]string, 0, len(e.allowed))
	for name := range e.allowed {
		tools = append(tools, name)
	}
	sort.Strings(tools)
	return tools
}

func (e *Executor) Execute(ctx context.Context, tool string, args []string) (Result, error) {
	if _, ok := e.allowed[tool]; !ok {
		return Result{}, trace.AccessDenied("tool %q is not allowlisted; allowed=%v", tool, e.AllowedTools())
	}

	cmd := exec.CommandContext(ctx, tool, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return Result{
			Stdout:     limitOutput(stdout.String()),
			Stderr:     limitOutput(stderr.String()),
			ReturnCode: 0,
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return Result{
			Stdout:     limitOutput(stdout.String()),
			Stderr:     limitOutput(stderr.String()),
			ReturnCode: exitErr.ExitCode(),
		}, nil
	}

	if stderr.Len() > 0 {
		return Result{}, trace.Wrap(err, "stderr=%s", limitOutput(stderr.String()))
	}
	return Result{}, trace.Wrap(err)
}

func limitOutput(raw string) string {
	const max = 8 * 1024
	if len(raw) <= max {
		return raw
	}
	return raw[:max]
}
