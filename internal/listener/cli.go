package listener

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

type InboundMessage struct {
	ConversationID string
	Content        string
}

type CLIListener struct {
	in  io.Reader
	out io.Writer
}

func NewCLIListener(in io.Reader, out io.Writer) *CLIListener {
	return &CLIListener{in: in, out: out}
}

func (l *CLIListener) Run(ctx context.Context, onMessage func(context.Context, InboundMessage) error) error {
	scanner := bufio.NewScanner(l.in)
	fmt.Fprintln(l.out, "enter messages as <conversation_id>: <text>; type 'exit' to quit")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "exit") {
			return nil
		}
		conversationID, content, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(content) == "" {
			fmt.Fprintln(l.out, "invalid input; expected <conversation_id>: <text>")
			continue
		}
		if err := onMessage(ctx, InboundMessage{ConversationID: strings.TrimSpace(conversationID), Content: strings.TrimSpace(content)}); err != nil {
			return err
		}
	}
}
